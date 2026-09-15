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
	return Ticket{ID: input.ID, Title: input.Title, Price: input.Price, UserID: input.UserID}, nil
}

func (repository *PostgresRepository) FindByID(ctx context.Context, id string) (Ticket, error) {
	var found Ticket
	err := repository.pool.QueryRow(
		ctx,
		`SELECT id, title, price, user_id
		 FROM tickets
		 WHERE id = $1`,
		id,
	).Scan(&found.ID, &found.Title, &found.Price, &found.UserID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Ticket{}, ErrNotFound
	}

	return found, err
}

func (repository *PostgresRepository) List(ctx context.Context) ([]Ticket, error) {
	rows, err := repository.pool.Query(
		ctx,
		`SELECT id, title, price, user_id
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
		if err := rows.Scan(&listed.ID, &listed.Title, &listed.Price, &listed.UserID); err != nil {
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
		 SET title = $1, price = $2
		 WHERE id = $3 AND user_id = $4
		 RETURNING id, title, price, user_id`,
		input.Title,
		input.Price,
		id,
		input.UserID,
	).Scan(&updated.ID, &updated.Title, &updated.Price, &updated.UserID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Ticket{}, ErrNotFound
	}

	return updated, err
}
