package payment

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	ErrOrderNotFound     = errors.New("order not found")
	ErrOrderNotPayable   = errors.New("order cannot be paid")
	ErrPaymentNotPending = errors.New("payment is not pending")
	ErrInvalidOutcome    = errors.New("invalid payment processor outcome")
	ErrUnavailable       = errors.New("payment operation is unavailable or still in progress")
)

type OrderStatus string

const (
	OrderStatusAwaitingPayment OrderStatus = "AwaitingPayment"
	OrderStatusCanceled        OrderStatus = "Canceled"
	OrderStatusComplete        OrderStatus = "Complete"
)

type Status string

const (
	StatusFailed    Status = "Failed"
	StatusPending   Status = "Pending"
	StatusSucceeded Status = "Succeeded"
)

type Order struct {
	ID     string
	Price  int64
	Status OrderStatus
	UserID string
}

type Payment struct {
	ID          string
	MockOutcome *Status
	OrderID     string
	Status      Status
}

type CreateInput struct {
	OrderID string
	UserID  string
}

type CreateResult struct {
	Created bool
	Payment Payment
}

type ValidationError struct {
	Code    string `json:"code"`
	Field   string `json:"field,omitempty"`
	Message string `json:"message"`
}

type Repository interface {
	Create(context.Context, CreateInput, Order) (Payment, bool, error)
	FindByOrderAndUser(context.Context, string, string) (Payment, bool, error)
}

type CreateCoordinator interface {
	CreatePayment(context.Context, CreateInput) (CreateResult, error)
}

type SettlementRepository interface {
	ClaimNextPending(context.Context, Status, time.Time) (Payment, bool, error)
	MarkResolved(context.Context, string, Status) error
}

// OrdersClient is the direct internal boundary. Orders owns the order status
// and the Tickets-service reservation claim; Payments owns only its snapshot
// and processor state.
type OrdersClient interface {
	ResolvePayment(context.Context, string, Status) (OrderStatus, error)
	StartPayment(context.Context, CreateInput) (Order, error)
}

func ParseProcessorOutcome(value string) (Status, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "success":
		return StatusSucceeded, nil
	case "failure":
		return StatusFailed, nil
	default:
		return "", fmt.Errorf("%w: %q", ErrInvalidOutcome, value)
	}
}

func ExpectedOrderStatus(outcome Status) OrderStatus {
	switch outcome {
	case StatusSucceeded:
		return OrderStatusComplete
	case StatusFailed:
		return OrderStatusCanceled
	default:
		return ""
	}
}
