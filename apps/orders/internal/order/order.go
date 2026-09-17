package order

import (
	"context"
	"errors"
	"time"
)

var (
	ErrNotFound           = errors.New("ticket not found")
	ErrOrderNotCancelable = errors.New("order cannot be canceled")
	ErrOrderNotFound      = errors.New("order not found")
	ErrOrderNotPayable    = errors.New("order cannot be paid")
	ErrReserved           = errors.New("ticket is reserved")
	ErrUnavailable        = errors.New("order operation is unavailable or still in progress")
)

const ExpirationWindow = 15 * time.Minute

type Status string

const (
	StatusCreated         Status = "Created"
	StatusCanceled        Status = "Canceled"
	StatusAwaitingPayment Status = "AwaitingPayment"
	StatusComplete        Status = "Complete"
)

type Ticket struct {
	ID               string
	Title            string
	Price            int64
	AggregateVersion int64
}

type Order struct {
	ExpiresAt time.Time
	ID        string
	Status    Status
	TicketID  string
	UserID    string
}

type PaymentOrder struct {
	ID     string
	Price  int64
	Status Status
	UserID string
}

type PaymentOutcome string

const (
	PaymentOutcomeFailed    PaymentOutcome = "Failed"
	PaymentOutcomeSucceeded PaymentOutcome = "Succeeded"
)

type PaymentResolutionResult struct {
	Order               Order
	ShouldReleaseTicket bool
}

type TicketReservationInput struct {
	ExpiresAt time.Time
	OrderID   string
	TicketID  string
	UserID    string
}

type ReservationResult struct {
	Created bool
	Order   Order
}

// ExpirationResult tells the expiry workflow whether it must release the
// Tickets-service reservation claim stored in the Tickets database.
type ExpirationResult struct {
	Order               Order
	ShouldReleaseTicket bool
}

type ValidationError struct {
	Code    string `json:"code"`
	Field   string `json:"field,omitempty"`
	Message string `json:"message"`
}

type OrderRepository interface {
	CancelByIDAndUser(context.Context, string, string) (Order, error)
	ExpireCreatedOrder(context.Context, string) (ExpirationResult, error)
	GetByIDAndUser(context.Context, string, string) (Order, error)
	ReserveTicket(context.Context, TicketReservationInput) (ReservationResult, error)
	ResolvePayment(context.Context, string, PaymentOutcome) (PaymentResolutionResult, error)
	StartPayment(context.Context, string, string) (PaymentOrder, error)
	ListByUser(context.Context, string) ([]Order, error)
}
