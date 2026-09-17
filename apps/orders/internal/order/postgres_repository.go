package order

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresRepository struct {
	pool *pgxpool.Pool
}

func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{pool: pool}
}

// ReserveTicket serializes reservations for a ticket by locking its
// orders-owned projection row. Projection population is intentionally deferred.
func (repository *PostgresRepository) ReserveTicket(ctx context.Context, input TicketReservationInput) (ReservationResult, error) {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return ReservationResult{}, err
	}
	defer tx.Rollback(ctx)

	var projectedTicketID string
	err = tx.QueryRow(ctx, `
		SELECT id::text
		FROM tickets
		WHERE id = $1
		FOR UPDATE`, input.TicketID).Scan(&projectedTicketID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ReservationResult{}, ErrNotFound
	}
	if err != nil {
		return ReservationResult{}, err
	}

	existing, found, err := findBlockingOrder(ctx, tx, projectedTicketID)
	if err != nil {
		return ReservationResult{}, err
	}
	if found {
		if existing.UserID == input.UserID && existing.Status != StatusComplete {
			if err := tx.Commit(ctx); err != nil {
				return ReservationResult{}, err
			}
			return ReservationResult{Order: existing}, nil
		}

		return ReservationResult{}, ErrReserved
	}

	created, err := insertOrder(ctx, tx, input)
	if err != nil {
		return ReservationResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ReservationResult{}, err
	}

	return ReservationResult{Created: true, Order: created}, nil
}

// findBlockingOrder treats every Created order as blocking. expires_at schedules
// Temporal's cancellation, but cannot prove the Tickets-service claim was released.
func findBlockingOrder(ctx context.Context, tx pgx.Tx, ticketID string) (Order, bool, error) {
	var found Order
	var status string
	err := tx.QueryRow(ctx, `
		SELECT id::text, expires_at, user_id, ticket_id::text, status::text
		FROM orders
		WHERE ticket_id = $1
		  AND (
			status = 'Complete'
			OR status = 'AwaitingPayment'
			OR status = 'Created'
		  )
		ORDER BY CASE WHEN status = 'Complete' THEN 0 ELSE 1 END, expires_at DESC
		LIMIT 1`, ticketID).Scan(
		&found.ID,
		&found.ExpiresAt,
		&found.UserID,
		&found.TicketID,
		&status,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Order{}, false, nil
	}
	if err != nil {
		return Order{}, false, err
	}
	found.Status = Status(status)

	return found, true, nil
}

func insertOrder(ctx context.Context, tx pgx.Tx, input TicketReservationInput) (Order, error) {
	var created Order
	var status string
	err := tx.QueryRow(ctx, `
		INSERT INTO orders (id, expires_at, user_id, ticket_id, status)
		VALUES (COALESCE(NULLIF($1, '')::uuid, gen_random_uuid()), $2, $3, $4, $5)
		RETURNING id::text, expires_at, user_id, ticket_id::text, status::text`,
		input.OrderID,
		input.ExpiresAt,
		input.UserID,
		input.TicketID,
		StatusCreated,
	).Scan(
		&created.ID,
		&created.ExpiresAt,
		&created.UserID,
		&created.TicketID,
		&status,
	)
	if err != nil {
		return Order{}, err
	}
	created.Status = Status(status)

	return created, nil
}

// ExpireCreatedOrder records the pre-checkout Temporal deadline outcome. It
// never changes AwaitingPayment or Complete orders: payment-pending orders
// retain their ticket claim until the future Payments service resolves them.
func (repository *PostgresRepository) ExpireCreatedOrder(ctx context.Context, orderID string) (ExpirationResult, error) {
	var expired Order
	var status string
	// Atomically cancel only an overdue order that is still in its pre-checkout state.
	err := repository.pool.QueryRow(ctx, `
		UPDATE orders
		SET status = $2
		WHERE id = $1
		  AND status = 'Created'
		  AND expires_at <= NOW()
		RETURNING id::text, expires_at, user_id, ticket_id::text, status::text`,
		orderID,
		StatusCanceled,
	).Scan(&expired.ID, &expired.ExpiresAt, &expired.UserID, &expired.TicketID, &status)
	if err == nil {
		expired.Status = Status(status)
		return ExpirationResult{Order: expired, ShouldReleaseTicket: true}, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return ExpirationResult{}, err
	}

	var found Order
	// The update may not apply because payment or completion won the race; read the
	// current state to decide whether the Tickets-service claim still needs release.
	err = repository.pool.QueryRow(ctx, `
		SELECT id::text, expires_at, user_id, ticket_id::text, status::text
		FROM orders
		WHERE id = $1`, orderID,
	).Scan(&found.ID, &found.ExpiresAt, &found.UserID, &found.TicketID, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return ExpirationResult{}, ErrOrderNotFound
	}
	if err != nil {
		return ExpirationResult{}, err
	}
	found.Status = Status(status)
	return ExpirationResult{Order: found, ShouldReleaseTicket: found.Status == StatusCanceled}, nil
}

func (repository *PostgresRepository) GetByIDAndUser(
	ctx context.Context,
	orderID string,
	userID string,
) (Order, error) {
	var found Order
	var status string
	err := repository.pool.QueryRow(ctx, `
		SELECT id::text, expires_at, user_id, ticket_id::text, status::text
		FROM orders
		WHERE id = $1 AND user_id = $2`, orderID, userID).Scan(
		&found.ID,
		&found.ExpiresAt,
		&found.UserID,
		&found.TicketID,
		&status,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Order{}, ErrOrderNotFound
	}
	if err != nil {
		return Order{}, err
	}
	found.Status = Status(status)

	return found, nil
}

// StartPayment atomically starts checkout for a Created order and returns the
// price snapshot that Payments needs. Replays after checkout started return
// the same AwaitingPayment snapshot without changing state again.
func (repository *PostgresRepository) StartPayment(
	ctx context.Context,
	orderID string,
	userID string,
) (PaymentOrder, error) {
	var started PaymentOrder
	var status string
	err := repository.pool.QueryRow(ctx, `
		UPDATE orders AS order_row
		SET status = $3
		FROM tickets
		WHERE order_row.id = $1
		  AND order_row.user_id = $2
		  AND order_row.ticket_id = tickets.id
		  AND order_row.status = 'Created'
		RETURNING order_row.id::text, order_row.user_id, tickets.price, order_row.status::text`,
		orderID,
		userID,
		StatusAwaitingPayment,
	).Scan(&started.ID, &started.UserID, &started.Price, &status)
	if err == nil {
		started.Status = Status(status)
		return started, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return PaymentOrder{}, err
	}

	err = repository.pool.QueryRow(ctx, `
		SELECT order_row.id::text, order_row.user_id, tickets.price, order_row.status::text
		FROM orders AS order_row
		JOIN tickets ON tickets.id = order_row.ticket_id
		WHERE order_row.id = $1 AND order_row.user_id = $2`, orderID, userID,
	).Scan(&started.ID, &started.UserID, &started.Price, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return PaymentOrder{}, ErrOrderNotFound
	}
	if err != nil {
		return PaymentOrder{}, err
	}
	started.Status = Status(status)
	if started.Status != StatusAwaitingPayment {
		return PaymentOrder{}, ErrOrderNotPayable
	}
	return started, nil
}

// ResolvePayment records Payments' first terminal result. A failed result
// asks the workflow to release the Tickets-service claim after cancellation.
func (repository *PostgresRepository) ResolvePayment(
	ctx context.Context,
	orderID string,
	outcome PaymentOutcome,
) (PaymentResolutionResult, error) {
	var targetStatus Status
	var shouldReleaseTicket bool
	switch outcome {
	case PaymentOutcomeSucceeded:
		targetStatus = StatusComplete
	case PaymentOutcomeFailed:
		targetStatus = StatusCanceled
		shouldReleaseTicket = true
	default:
		return PaymentResolutionResult{}, ErrOrderNotPayable
	}

	var resolved Order
	var status string
	err := repository.pool.QueryRow(ctx, `
		UPDATE orders
		SET status = $2
		WHERE id = $1 AND status = 'AwaitingPayment'
		RETURNING id::text, expires_at, user_id, ticket_id::text, status::text`,
		orderID,
		targetStatus,
	).Scan(&resolved.ID, &resolved.ExpiresAt, &resolved.UserID, &resolved.TicketID, &status)
	if err == nil {
		resolved.Status = Status(status)
		return PaymentResolutionResult{Order: resolved, ShouldReleaseTicket: shouldReleaseTicket}, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return PaymentResolutionResult{}, err
	}

	found, err := repository.getByID(ctx, orderID)
	if err != nil {
		return PaymentResolutionResult{}, err
	}
	if (outcome == PaymentOutcomeSucceeded && found.Status == StatusComplete) ||
		(outcome == PaymentOutcomeFailed && found.Status == StatusCanceled) {
		return PaymentResolutionResult{Order: found, ShouldReleaseTicket: shouldReleaseTicket}, nil
	}
	return PaymentResolutionResult{}, ErrOrderNotPayable
}

func (repository *PostgresRepository) getByID(ctx context.Context, orderID string) (Order, error) {
	var found Order
	var status string
	err := repository.pool.QueryRow(ctx, `
		SELECT id::text, expires_at, user_id, ticket_id::text, status::text
		FROM orders
		WHERE id = $1`, orderID,
	).Scan(&found.ID, &found.ExpiresAt, &found.UserID, &found.TicketID, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return Order{}, ErrOrderNotFound
	}
	if err != nil {
		return Order{}, err
	}
	found.Status = Status(status)
	return found, nil
}

func (repository *PostgresRepository) CancelByIDAndUser(
	ctx context.Context,
	orderID string,
	userID string,
) (Order, error) {
	var canceled Order
	var status string
	err := repository.pool.QueryRow(ctx, `
		UPDATE orders
		SET status = $3
		WHERE id = $1
		  AND user_id = $2
		  AND status = 'Created'
		RETURNING id::text, expires_at, user_id, ticket_id::text, status::text`,
		orderID,
		userID,
		StatusCanceled,
	).Scan(
		&canceled.ID,
		&canceled.ExpiresAt,
		&canceled.UserID,
		&canceled.TicketID,
		&status,
	)
	if err == nil {
		canceled.Status = Status(status)
		return canceled, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return Order{}, err
	}

	found, err := repository.GetByIDAndUser(ctx, orderID, userID)
	if err != nil {
		return Order{}, err
	}
	if found.Status == StatusCanceled {
		return found, nil
	}

	return Order{}, ErrOrderNotCancelable
}

func (repository *PostgresRepository) ListByUser(ctx context.Context, userID string) ([]Order, error) {
	rows, err := repository.pool.Query(ctx, `
		SELECT id::text, expires_at, user_id, ticket_id::text, status::text
		FROM orders
		WHERE user_id = $1
		ORDER BY expires_at DESC, id DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	orders := make([]Order, 0)
	for rows.Next() {
		var found Order
		var status string
		if err := rows.Scan(
			&found.ID,
			&found.ExpiresAt,
			&found.UserID,
			&found.TicketID,
			&status,
		); err != nil {
			return nil, err
		}
		found.Status = Status(status)
		orders = append(orders, found)
	}

	return orders, rows.Err()
}
