package ticket

import (
	"context"
	"strings"

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
	coordinator TicketCreationCoordinator
}

// TicketCreationCoordinator starts or joins the durable ticket creation
// operation. The production implementation is backed by Temporal.
type TicketCreationCoordinator interface {
	Create(context.Context, CreateInput) (Ticket, error)
}

func NewService(repository Repository) *Service {
	return NewServiceWithTicketCreationCoordinator(repository, repository)
}

func NewServiceWithTicketCreationCoordinator(repository Repository, coordinator TicketCreationCoordinator) *Service {
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
		key := *input.IdempotencyKey
		valid := len(key) >= 1 && len(key) <= 128
		for _, c := range key {
			valid = valid && c >= 32 && c <= 126
		}
		if !valid {
			return Ticket{}, []ValidationError{{Code: "INVALID_ARGUMENT", Field: "idempotencyKey", Message: "Idempotency-Key must contain 1 to 128 printable ASCII characters."}}, nil
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

	found, err := service.repository.FindByID(ctx, id)
	if err != nil {
		return Ticket{}, nil, err
	}
	if found.UserID != input.UserID {
		return Ticket{}, nil, ErrForbidden
	}
	updated, err := service.repository.Update(ctx, id, input)
	return updated, nil, err
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
