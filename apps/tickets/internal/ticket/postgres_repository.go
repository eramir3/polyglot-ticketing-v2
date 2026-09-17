package ticket

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresRepository struct {
	pool *pgxpool.Pool
}

func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{pool: pool}
}

func (repository *PostgresRepository) Create(ctx context.Context, input CreateInput) (Ticket, error) {
	if input.ID == "" {
		input.ID = uuid.NewString()
	}
	var created Ticket
	_, err := repository.pool.Exec(
		ctx,
		`INSERT INTO tickets (id, title, price, user_id)
		 VALUES ($1, $2, $3, $4) ON CONFLICT (id) DO NOTHING`,
		input.ID,
		input.Title,
		input.Price,
		input.UserID,
	)
	if err != nil {
		return Ticket{}, err
	}

	err = repository.pool.QueryRow(ctx, `SELECT user_id FROM tickets WHERE id = $1`, input.ID).Scan(&created.UserID)
	if err != nil {
		return Ticket{}, err
	}
	if created.UserID != input.UserID {
		return Ticket{}, ErrConflict
	}
	// Return the immutable workflow input even if this activity is retried after
	// a subsequent edit. Never overwrite the current row on a retry.
	return Ticket{ID: input.ID, Title: input.Title, Price: input.Price, UserID: input.UserID, AggregateVersion: 1}, nil
}

func (repository *PostgresRepository) FindByID(ctx context.Context, id string) (Ticket, error) {
	var found Ticket
	err := repository.pool.QueryRow(
		ctx,
		`SELECT id, title, price, user_id, aggregate_version, reserved_by_order_id::text
		 FROM tickets
		 WHERE id = $1`,
		id,
	).Scan(&found.ID, &found.Title, &found.Price, &found.UserID, &found.AggregateVersion, &found.ReservedByOrderID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Ticket{}, ErrNotFound
	}

	return found, err
}

func (repository *PostgresRepository) List(ctx context.Context) ([]Ticket, error) {
	rows, err := repository.pool.Query(
		ctx,
		`SELECT id, title, price, user_id, aggregate_version, reserved_by_order_id::text
		 FROM tickets
		 ORDER BY title ASC, id ASC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	tickets := make([]Ticket, 0)
	for rows.Next() {
		var listed Ticket
		if err := rows.Scan(&listed.ID, &listed.Title, &listed.Price, &listed.UserID, &listed.AggregateVersion, &listed.ReservedByOrderID); err != nil {
			return nil, err
		}

		tickets = append(tickets, listed)
	}

	return tickets, rows.Err()
}

func (repository *PostgresRepository) Update(ctx context.Context, id string, input UpdateInput) (Ticket, error) {
	var updated Ticket
	err := repository.pool.QueryRow(
		ctx,
		`UPDATE tickets
		 SET title = $1, price = $2, aggregate_version = aggregate_version + 1
		 WHERE id = $3 AND user_id = $4 AND reserved_by_order_id IS NULL
		 RETURNING id, title, price, user_id, aggregate_version`,
		input.Title,
		input.Price,
		id,
		input.UserID,
	).Scan(&updated.ID, &updated.Title, &updated.Price, &updated.UserID, &updated.AggregateVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		return Ticket{}, repository.ticketUpdateError(ctx, repository.pool, id, input.UserID)
	}

	return updated, err
}

// UpdateWithIdempotency applies one ticket edit and stores its resulting
// snapshot under the caller's key, so retried requests return the original
// result without applying the mutation or incrementing its version again.
func (repository *PostgresRepository) UpdateWithIdempotency(ctx context.Context, id string, input UpdateInput) (Ticket, error) {
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return Ticket{}, err
	}
	defer tx.Rollback(ctx)

	var owner string
	var reservationID *string
	// Lock the source row first. A reservation takes precedence over an
	// idempotency replay: every attempted edit is forbidden while an order holds
	// the ticket, even if that edit succeeded before the reservation started.
	err = tx.QueryRow(ctx, `
		SELECT user_id, reserved_by_order_id::text
		FROM tickets
		WHERE id = $1
		FOR UPDATE`, id,
	).Scan(&owner, &reservationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Ticket{}, ErrNotFound
	}
	if err != nil {
		return Ticket{}, err
	}
	if owner != input.UserID {
		return Ticket{}, ErrForbidden
	}
	if reservationID != nil {
		return Ticket{}, ErrReserved
	}

	var existing Ticket
	// The row is unreserved, so an idempotent retry can safely return its
	// original committed snapshot without incrementing the version again.
	err = tx.QueryRow(ctx, `
		SELECT ticket_id::text, title, price, user_id, aggregate_version
		FROM ticket_update_operations
		WHERE ticket_id = $1 AND user_id = $2 AND idempotency_key = $3`,
		id, input.UserID, input.IdempotencyKey,
	).Scan(&existing.ID, &existing.Title, &existing.Price, &existing.UserID, &existing.AggregateVersion)
	if err == nil {
		if err := tx.Commit(ctx); err != nil {
			return Ticket{}, err
		}
		return existing, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return Ticket{}, err
	}

	var updated Ticket
	// The locked row is owned by the caller and unreserved. Increment the
	// aggregate version atomically with the mutation.
	// The aggregate version is incremented atomically with the update so each
	// successful update produces exactly one new version.
	err = tx.QueryRow(ctx, `
		UPDATE tickets
		SET title = $1, price = $2, aggregate_version = aggregate_version + 1
		WHERE id = $3 AND user_id = $4
		RETURNING id, title, price, user_id, aggregate_version`,
		input.Title, input.Price, id, input.UserID,
	).Scan(&updated.ID, &updated.Title, &updated.Price, &updated.UserID, &updated.AggregateVersion)
	if err != nil {
		return Ticket{}, err
	}

	// Persist the committed snapshot so future retries cannot increment again.
	_, err = tx.Exec(ctx, `
		INSERT INTO ticket_update_operations
		(ticket_id, user_id, idempotency_key, aggregate_version, title, price)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		updated.ID, updated.UserID, input.IdempotencyKey, updated.AggregateVersion, updated.Title, updated.Price,
	)
	if err != nil {
		return Ticket{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Ticket{}, err
	}
	return updated, nil
}

// ReserveForOrder claims an otherwise-unreserved ticket. Repeating the same
// order claim is safe; a different active order cannot replace the claim.
func (repository *PostgresRepository) ReserveForOrder(ctx context.Context, ticketID string, orderID string) error {
	result, err := repository.pool.Exec(ctx, `
		UPDATE tickets
		SET reserved_by_order_id = $2
		WHERE id = $1
		  AND (reserved_by_order_id IS NULL OR reserved_by_order_id = $2)`, ticketID, orderID)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 1 {
		return nil
	}
	_, reservationID, err := ticketOwnerAndReservation(ctx, repository.pool, ticketID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if reservationID != nil {
		return ErrReserved
	}
	return ErrReserved
}

// ReleaseOrderReservation clears a claim only when it still belongs to the
// supplied order. It is intentionally a no-op for an already-released or
// newer claim so Temporal activity retries are safe.
func (repository *PostgresRepository) ReleaseOrderReservation(ctx context.Context, ticketID string, orderID string) error {
	result, err := repository.pool.Exec(ctx, `
		UPDATE tickets
		SET reserved_by_order_id = NULL
		WHERE id = $1 AND reserved_by_order_id = $2`, ticketID, orderID)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 1 {
		return nil
	}
	_, _, err = ticketOwnerAndReservation(ctx, repository.pool, ticketID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

type ticketRowQuerier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func (repository *PostgresRepository) ticketUpdateError(ctx context.Context, querier ticketRowQuerier, ticketID string, userID string) error {
	owner, reservationID, err := ticketOwnerAndReservation(ctx, querier, ticketID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if owner != userID {
		return ErrForbidden
	}
	if reservationID != nil {
		return ErrReserved
	}
	return ErrNotFound
}

func ticketOwnerAndReservation(ctx context.Context, querier ticketRowQuerier, ticketID string) (string, *string, error) {
	var owner string
	var reservationID *string
	err := querier.QueryRow(ctx, `
		SELECT user_id, reserved_by_order_id::text
		FROM tickets
		WHERE id = $1`, ticketID).Scan(&owner, &reservationID)
	return owner, reservationID, err
}
