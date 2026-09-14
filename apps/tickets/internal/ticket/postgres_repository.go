package ticket

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	ticketsv1 "polyglot-ticketing-v2/protogen/go/tickets/v1"
)

type PostgresRepository struct {
	pool *pgxpool.Pool
}

func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{pool: pool}
}

func (repository *PostgresRepository) Create(ctx context.Context, input CreateInput) (Ticket, error) {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Ticket{}, err
	}
	defer tx.Rollback(ctx)

	var created Ticket
	err = tx.QueryRow(
		ctx,
		`INSERT INTO tickets (title, price, user_id)
		 VALUES ($1, $2, $3)
		RETURNING id, title, price, user_id, COALESCE(reserved_by_order_id::text, '')`,
		input.Title,
		input.Price,
		input.UserID,
	).Scan(&created.ID, &created.Title, &created.Price, &created.UserID, &created.ReservedByOrderID)
	if err != nil {
		return Ticket{}, err
	}

	return created, nil
}

func marshalTicketCreated(occurredAt time.Time, created Ticket) ([]byte, error) {
	return proto.Marshal(&ticketsv1.TicketCreated{
		OccurredAt: timestamppb.New(occurredAt),
		Ticket: &ticketsv1.Ticket{
			Id:     created.ID,
			Title:  created.Title,
			Price:  created.Price,
			UserId: created.UserID,
		},
	})
}

func marshalTicketUpdated(occurredAt time.Time, updated Ticket) ([]byte, error) {
	return proto.Marshal(&ticketsv1.TicketUpdated{
		OccurredAt: timestamppb.New(occurredAt),
		Ticket: &ticketsv1.Ticket{
			Id:     updated.ID,
			Title:  updated.Title,
			Price:  updated.Price,
			UserId: updated.UserID,
		},
	})
}

func (repository *PostgresRepository) FindByID(ctx context.Context, id string) (Ticket, error) {
	var found Ticket
	err := repository.pool.QueryRow(
		ctx,
		`SELECT id, title, price, user_id, COALESCE(reserved_by_order_id::text, '')
		 FROM tickets
		 WHERE id = $1`,
		id,
	).Scan(&found.ID, &found.Title, &found.Price, &found.UserID, &found.ReservedByOrderID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Ticket{}, ErrNotFound
	}

	return found, err
}

func (repository *PostgresRepository) List(ctx context.Context) ([]Ticket, error) {
	rows, err := repository.pool.Query(
		ctx,
		`SELECT id, title, price, user_id, COALESCE(reserved_by_order_id::text, '')
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
		if err := rows.Scan(&listed.ID, &listed.Title, &listed.Price, &listed.UserID, &listed.ReservedByOrderID); err != nil {
			return nil, err
		}

		tickets = append(tickets, listed)
	}

	return tickets, rows.Err()
}

func (repository *PostgresRepository) Update(ctx context.Context, id string, input UpdateInput) (Ticket, error) {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Ticket{}, err
	}
	defer tx.Rollback(ctx)

	var reserved bool
	err = tx.QueryRow(ctx, `
		SELECT reserved_by_order_id IS NOT NULL
		FROM tickets
		WHERE id = $1 AND user_id = $2
		FOR UPDATE`, id, input.UserID).Scan(&reserved)
	if errors.Is(err, pgx.ErrNoRows) {
		return Ticket{}, ErrNotFound
	}
	if err != nil {
		return Ticket{}, err
	}
	if reserved {
		return Ticket{}, ErrReserved
	}

	var updated Ticket
	err = tx.QueryRow(
		ctx,
		`UPDATE tickets
		 SET title = $1, price = $2
		 WHERE id = $3 AND user_id = $4
		 RETURNING id, title, price, user_id, COALESCE(reserved_by_order_id::text, '')`,
		input.Title,
		input.Price,
		id,
		input.UserID,
	).Scan(&updated.ID, &updated.Title, &updated.Price, &updated.UserID, &updated.ReservedByOrderID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Ticket{}, ErrNotFound
	}
	if err != nil {
		return Ticket{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return Ticket{}, err
	}

	return updated, nil
}

// publishes its updated snapshot in one transaction.
func (repository *PostgresRepository) ReserveTicketFromOrder(
	ctx context.Context,
	orderID string,
	ticketID string,
) error {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var reservedByOrderID *string
	err = tx.QueryRow(ctx, `
		SELECT reserved_by_order_id::text
		FROM tickets
		WHERE id = $1
		FOR UPDATE`, ticketID).Scan(&reservedByOrderID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if reservedByOrderID != nil {
		if *reservedByOrderID == orderID {
			return tx.Commit(ctx)
		}
		return ErrReserved
	}

	var updated Ticket
	err = tx.QueryRow(ctx, `
		UPDATE tickets
		SET reserved_by_order_id = $2
		WHERE id = $1
		RETURNING id, title, price, user_id, COALESCE(reserved_by_order_id::text, '')`,
		ticketID,
		orderID,
	).Scan(
		&updated.ID,
		&updated.Title,
		&updated.Price,
		&updated.UserID,
		&updated.ReservedByOrderID,
	)
	if err != nil {
		return err
	}

	return tx.Commit(ctx)
}

// UnreserveTicketFromOrder clears the ticket marker only when the canceled
// order still owns that reservation.
func (repository *PostgresRepository) UnreserveTicketFromOrder(
	ctx context.Context,
	orderID string,
	ticketID string,
) error {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var reservedByOrderID *string
	err = tx.QueryRow(ctx, `
		SELECT reserved_by_order_id::text
		FROM tickets
		WHERE id = $1
		FOR UPDATE`, ticketID).Scan(&reservedByOrderID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if reservedByOrderID == nil || *reservedByOrderID != orderID {
		return ErrOrderReservationPending
	}

	var updated Ticket
	err = tx.QueryRow(ctx, `
		UPDATE tickets
		SET reserved_by_order_id = NULL,
		WHERE id = $1 AND reserved_by_order_id = $2
		RETURNING id, title, price, user_id`,
		ticketID,
		orderID,
	).Scan(
		&updated.ID,
		&updated.Title,
		&updated.Price,
		&updated.UserID,
		&updated.ReservedByOrderID,
	)
	if err != nil {
		return err
	}

	return tx.Commit(ctx)
}
