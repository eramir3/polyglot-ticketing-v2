package order

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
)

var ErrProjectionConflict = errors.New("ticket projection already exists with different data")

// EnsureTicketProjection is insert-only: activity retries cannot overwrite
// projection changes made after ticket creation.
func (repository *PostgresRepository) EnsureTicketProjection(ctx context.Context, input Ticket) error {
	_, err := repository.pool.Exec(ctx,
		`INSERT INTO tickets (id, title, price) VALUES ($1, $2, $3) ON CONFLICT (id) DO NOTHING`,
		input.ID, input.Title, input.Price)
	if err != nil {
		return err
	}
	var existing Ticket
	err = repository.pool.QueryRow(ctx, `SELECT id, title, price FROM tickets WHERE id = $1`, input.ID).
		Scan(&existing.ID, &existing.Title, &existing.Price)
	if err != nil {
		return err
	}
	if existing.Title != input.Title || existing.Price != input.Price {
		return ErrProjectionConflict
	}
	return nil
}

func ValidateProjection(input Ticket) bool {
	_, err := uuid.Parse(input.ID)
	return err == nil && strings.TrimSpace(input.Title) != "" && input.Price > 0 && input.Price <= 9_007_199_254_740_991
}
