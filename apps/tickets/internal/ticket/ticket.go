package ticket

import (
	"context"
	"errors"
)

var (
	ErrForbidden   = errors.New("ticket access forbidden")
	ErrNotFound    = errors.New("ticket not found")
	ErrConflict    = errors.New("ticket creation conflicts with an existing request")
	ErrUnavailable = errors.New("ticket creation is unavailable or still in progress; retry with the same Idempotency-Key")
)

type Ticket struct {
	ID     string
	Title  string
	Price  int64
	UserID string
}

type CreateInput struct {
	ID             string
	IdempotencyKey *string
	Title          string
	Price          int64
	UserID         string
}

type UpdateInput struct {
	Title  string
	Price  int64
	UserID string
}

type Repository interface {
	Create(context.Context, CreateInput) (Ticket, error)
	FindByID(context.Context, string) (Ticket, error)
	List(context.Context) ([]Ticket, error)
	Update(context.Context, string, UpdateInput) (Ticket, error)
}

// TicketPersister owns the Tickets database write used by the creation
// workflow's persistence activity.
type TicketPersister interface {
	Create(context.Context, CreateInput) (Ticket, error)
}
