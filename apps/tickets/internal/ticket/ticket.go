package ticket

import (
	"context"
	"errors"
)

var (
	ErrForbidden   = errors.New("ticket access forbidden")
	ErrNotFound    = errors.New("ticket not found")
	ErrReserved    = errors.New("ticket is reserved by an order")
	ErrConflict    = errors.New("ticket creation conflicts with an existing request")
	ErrUnavailable = errors.New("ticket operation is unavailable or still in progress; retry with the same Idempotency-Key")
)

type Ticket struct {
	ID                string
	Title             string
	Price             int64
	UserID            string
	AggregateVersion  int64
	ReservedByOrderID *string
}

type CreateInput struct {
	ID             string
	IdempotencyKey *string
	Title          string
	Price          int64
	UserID         string
}

type UpdateInput struct {
	IdempotencyKey string
	Title          string
	Price          int64
	UserID         string
}

type Repository interface {
	Create(context.Context, CreateInput) (Ticket, error)
	FindByID(context.Context, string) (Ticket, error)
	List(context.Context) ([]Ticket, error)
	Update(context.Context, string, UpdateInput) (Ticket, error)
	UpdateWithIdempotency(context.Context, string, UpdateInput) (Ticket, error)
	ReserveForOrder(context.Context, string, string) error
	ReleaseOrderReservation(context.Context, string, string) error
}
