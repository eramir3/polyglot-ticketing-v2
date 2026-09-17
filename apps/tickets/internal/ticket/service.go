package ticket

import (
	"context"
	"strings"

	"github.com/google/uuid"

	"polyglot-ticketing-v2/apps/tickets/internal/errorcode"
	commonv1 "polyglot-ticketing-v2/protogen/go/common/v1"
)

const MaxPrice int64 = 9_007_199_254_740_991

type ValidationError struct {
	Code    string `json:"code"`
	Field   string `json:"field,omitempty"`
	Message string `json:"message"`
}

type Service struct {
	repository  Repository
	coordinator Coordinator
}

// Coordinator starts or joins the durable ticket creation and update
// operations. The production implementation is backed by Temporal.
type Coordinator interface {
	Create(context.Context, CreateInput) (Ticket, error)
	Update(context.Context, string, UpdateInput) (Ticket, error)
}

func NewService(
	repository Repository,
	coordinator Coordinator,
) *Service {
	return &Service{
		repository:  repository,
		coordinator: coordinator,
	}
}

func (service *Service) CreateTicket(ctx context.Context, input CreateInput) (Ticket, []ValidationError, error) {
	if validationErrors := validateMutation(input.Title, input.Price, input.UserID); len(validationErrors) > 0 {
		return Ticket{}, validationErrors, nil
	}

	if input.IdempotencyKey != nil {
		if validationErrors := validateIdempotencyKey(*input.IdempotencyKey, false); len(validationErrors) > 0 {
			return Ticket{}, validationErrors, nil
		}
	}
	created, err := service.coordinator.Create(ctx, input)
	return created, nil, err
}

func (service *Service) ListTickets(ctx context.Context) ([]Ticket, error) {
	return service.repository.List(ctx)
}

func (service *Service) GetTicket(ctx context.Context, id string) (Ticket, error) {
	return service.repository.FindByID(ctx, id)
}

func (service *Service) UpdateTicket(ctx context.Context, id string, input UpdateInput) (Ticket, []ValidationError, error) {
	if validationErrors := validateMutation(input.Title, input.Price, input.UserID); len(validationErrors) > 0 {
		return Ticket{}, validationErrors, nil
	}
	if validationErrors := validateIdempotencyKey(input.IdempotencyKey, true); len(validationErrors) > 0 {
		return Ticket{}, validationErrors, nil
	}
	updated, err := service.coordinator.Update(ctx, id, input)
	return updated, nil, err
}

// ReserveTicketForOrder is an internal Orders-to-Tickets operation. The
// reservation is intentionally not routed through the ticket edit workflow:
// it does not change any projected ticket fields or aggregate version.
func (service *Service) ReserveTicketForOrder(ctx context.Context, ticketID string, orderID string) ([]ValidationError, error) {
	if validationErrors := validateReservation(ticketID, orderID); len(validationErrors) > 0 {
		return validationErrors, nil
	}
	return nil, service.repository.ReserveForOrder(ctx, ticketID, orderID)
}

// ReleaseTicketReservation clears only the matching order claim. This makes
// cancellation and expiry activity retries harmless.
func (service *Service) ReleaseTicketReservation(ctx context.Context, ticketID string, orderID string) ([]ValidationError, error) {
	if validationErrors := validateReservation(ticketID, orderID); len(validationErrors) > 0 {
		return validationErrors, nil
	}
	return nil, service.repository.ReleaseOrderReservation(ctx, ticketID, orderID)
}

func validateReservation(ticketID string, orderID string) []ValidationError {
	if _, err := uuid.Parse(ticketID); err != nil {
		return []ValidationError{{Code: errorcode.String(commonv1.ErrorCode_ERROR_CODE_INVALID_ARGUMENT), Field: "ticketId", Message: "Ticket ID must be a valid UUID."}}
	}
	if _, err := uuid.Parse(orderID); err != nil {
		return []ValidationError{{Code: errorcode.String(commonv1.ErrorCode_ERROR_CODE_INVALID_ARGUMENT), Field: "orderId", Message: "Order ID must be a valid UUID."}}
	}
	return nil
}

func validateIdempotencyKey(key string, required bool) []ValidationError {
	if key == "" && required {
		return []ValidationError{{Code: "INVALID_ARGUMENT", Field: "idempotencyKey", Message: "Idempotency-Key is required."}}
	}
	if key == "" {
		return nil
	}
	valid := len(key) <= 128
	for _, c := range key {
		valid = valid && c >= 32 && c <= 126
	}
	if valid {
		return nil
	}
	return []ValidationError{{Code: "INVALID_ARGUMENT", Field: "idempotencyKey", Message: "Idempotency-Key must contain 1 to 128 printable ASCII characters."}}
}

func validateMutation(title string, price int64, userID string) []ValidationError {
	if strings.TrimSpace(title) == "" {
		return []ValidationError{{
			Code:    errorcode.String(commonv1.ErrorCode_ERROR_CODE_INVALID_TITLE),
			Field:   "title",
			Message: "Title is required.",
		}}
	}

	if price <= 0 || price > MaxPrice {
		return []ValidationError{{
			Code:    errorcode.String(commonv1.ErrorCode_ERROR_CODE_INVALID_PRICE),
			Field:   "price",
			Message: "Price must be between 1 and 9007199254740991.",
		}}
	}

	if userID == "" {
		return []ValidationError{{
			Code:    errorcode.String(commonv1.ErrorCode_ERROR_CODE_INVALID_ARGUMENT),
			Field:   "userId",
			Message: "Ticket owner is required.",
		}}
	}

	return nil
}
