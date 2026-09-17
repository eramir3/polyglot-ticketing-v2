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
			OR (status = 'Created' AND expires_at > NOW())
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
