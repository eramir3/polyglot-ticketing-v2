package order

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
)

var (
	ErrProjectionConflict   = errors.New("ticket projection has conflicting data for its aggregate version")
	ErrProjectionOutOfOrder = errors.New("ticket projection update is ahead of the current aggregate version")
)

// EnsureTicketProjection applies the initial projection or exactly the next
// aggregate version. Retrying an already-applied version is safe.
func (repository *PostgresRepository) EnsureTicketProjection(ctx context.Context, input Ticket) error {
	result, err := repository.pool.Exec(ctx, `
		INSERT INTO tickets (id, title, price, aggregate_version)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (id) DO UPDATE
		SET title = EXCLUDED.title,
			price = EXCLUDED.price,
			aggregate_version = EXCLUDED.aggregate_version
		WHERE tickets.aggregate_version = EXCLUDED.aggregate_version - 1`,
		input.ID, input.Title, input.Price, input.AggregateVersion)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 1 {
		return nil
	}

	var existing Ticket
	err = repository.pool.QueryRow(ctx, `SELECT id, title, price, aggregate_version FROM tickets WHERE id = $1`, input.ID).
		Scan(&existing.ID, &existing.Title, &existing.Price, &existing.AggregateVersion)
	if err != nil {
		return err
	}
	if existing.AggregateVersion == input.AggregateVersion && existing.Title == input.Title && existing.Price == input.Price {
		return nil
	}
	if existing.AggregateVersion < input.AggregateVersion {
		return ErrProjectionOutOfOrder
	}
	return ErrProjectionConflict
}

func ValidateProjection(input Ticket) bool {
	_, err := uuid.Parse(input.ID)
	return err == nil && strings.TrimSpace(input.Title) != "" && input.Price > 0 && input.Price <= 9_007_199_254_740_991 && input.AggregateVersion >= 1
}
