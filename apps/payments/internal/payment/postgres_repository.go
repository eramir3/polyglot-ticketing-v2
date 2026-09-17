package payment

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresRepository struct {
	pool *pgxpool.Pool
}

func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{pool: pool}
}

// Create persists the direct Orders snapshot and creates at most one pending
// payment for it. Replaying CreatePayment returns the original payment.
func (repository *PostgresRepository) Create(ctx context.Context, input CreateInput, order Order) (Payment, bool, error) {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Payment{}, false, err
	}
	defer tx.Rollback(ctx)

	_, err = tx.Exec(ctx, `
		INSERT INTO orders (id, aggregate_version, user_id, price, status)
		VALUES ($1, 0, $2, $3, $4)
		ON CONFLICT (id) DO NOTHING`,
		order.ID,
		order.UserID,
		order.Price,
		order.Status,
	)
	if err != nil {
		return Payment{}, false, err
	}

	var created Payment
	err = tx.QueryRow(ctx, `
		INSERT INTO payments (order_id)
		VALUES ($1)
		ON CONFLICT (order_id) DO NOTHING
		RETURNING id::text, order_id::text, status::text, mock_outcome::text`, input.OrderID,
	).Scan(&created.ID, &created.OrderID, &created.Status, &created.MockOutcome)
	if err == nil {
		if err := tx.Commit(ctx); err != nil {
			return Payment{}, false, err
		}
		return created, true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return Payment{}, false, err
	}

	var existing Payment
	err = tx.QueryRow(ctx, `
		SELECT id::text, order_id::text, status::text, mock_outcome::text
		FROM payments
		WHERE order_id = $1`, input.OrderID,
	).Scan(&existing.ID, &existing.OrderID, &existing.Status, &existing.MockOutcome)
	if err != nil {
		return Payment{}, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Payment{}, false, err
	}
	return existing, false, nil
}

// FindByOrderAndUser lets a caller safely replay creation after the payment
// has reached a terminal state, when Orders rightly refuses to restart it.
func (repository *PostgresRepository) FindByOrderAndUser(
	ctx context.Context,
	orderID string,
	userID string,
) (Payment, bool, error) {
	var found Payment
	err := repository.pool.QueryRow(ctx, `
		SELECT payments.id::text, payments.order_id::text, payments.status::text, payments.mock_outcome::text
		FROM payments
		JOIN orders ON orders.id = payments.order_id
		WHERE payments.order_id = $1 AND orders.user_id = $2`, orderID, userID,
	).Scan(&found.ID, &found.OrderID, &found.Status, &found.MockOutcome)
	if errors.Is(err, pgx.ErrNoRows) {
		return Payment{}, false, nil
	}
	if err != nil {
		return Payment{}, false, err
	}
	return found, true, nil
}

// ClaimNextPending leases one payment without changing its durable result. The
// chosen mock outcome is saved with the lease, so recovery retries the same
// outcome after a process restart or ambiguous gRPC call.
func (repository *PostgresRepository) ClaimNextPending(ctx context.Context, configuredOutcome Status, leaseUntil time.Time) (Payment, bool, error) {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Payment{}, false, err
	}
	defer tx.Rollback(ctx)

	var claimed Payment
	err = tx.QueryRow(ctx, `
		SELECT id::text, order_id::text, status::text, mock_outcome::text
		FROM payments
		WHERE status = 'Pending'
		  AND (settlement_lease_expires_at IS NULL OR settlement_lease_expires_at <= NOW())
		ORDER BY id
		FOR UPDATE SKIP LOCKED
		LIMIT 1`,
	).Scan(&claimed.ID, &claimed.OrderID, &claimed.Status, &claimed.MockOutcome)
	if errors.Is(err, pgx.ErrNoRows) {
		return Payment{}, false, tx.Commit(ctx)
	}
	if err != nil {
		return Payment{}, false, err
	}

	err = tx.QueryRow(ctx, `
		UPDATE payments
		SET mock_outcome = COALESCE(mock_outcome, $2), settlement_lease_expires_at = $3
		WHERE id = $1
		RETURNING mock_outcome::text`, claimed.ID, configuredOutcome, leaseUntil,
	).Scan(&claimed.MockOutcome)
	if err != nil {
		return Payment{}, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Payment{}, false, err
	}
	return claimed, true, nil
}

func (repository *PostgresRepository) MarkResolved(ctx context.Context, paymentID string, outcome Status) error {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var orderID string
	err = tx.QueryRow(ctx, `
		UPDATE payments
		SET status = $2, settlement_lease_expires_at = NULL
		WHERE id = $1 AND status = 'Pending' AND mock_outcome = $2
		RETURNING order_id::text`, paymentID, outcome,
	).Scan(&orderID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrPaymentNotPending
	}
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE orders
		SET status = $2
		WHERE id = $1`, orderID, ExpectedOrderStatus(outcome)); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

var _ Repository = (*PostgresRepository)(nil)
var _ SettlementRepository = (*PostgresRepository)(nil)
