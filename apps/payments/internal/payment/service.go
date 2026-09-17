package payment

import (
	"context"
	"strings"

	"github.com/google/uuid"

	"polyglot-ticketing-v2/apps/payments/internal/errorcode"
	commonv1 "polyglot-ticketing-v2/protogen/go/common/v1"
)

type Service struct {
	coordinator CreateCoordinator
	repository  Repository
}

func NewService(repository Repository, coordinator CreateCoordinator) *Service {
	return &Service{coordinator: coordinator, repository: repository}
}

func (service *Service) CreatePayment(
	ctx context.Context,
	input CreateInput,
) (Payment, bool, []ValidationError, error) {
	if validationErrors := validateCreate(input); len(validationErrors) > 0 {
		return Payment{}, false, validationErrors, nil
	}
	// A retry after settlement must return the payment already owned by this
	// user instead of asking Orders to start checkout from a terminal order.
	existing, found, err := service.repository.FindByOrderAndUser(ctx, input.OrderID, input.UserID)
	if err != nil {
		return Payment{}, false, nil, err
	}
	if found {
		return existing, false, nil, nil
	}

	created, err := service.coordinator.CreatePayment(ctx, input)
	if err != nil {
		return Payment{}, false, nil, err
	}
	return created.Payment, created.Created, nil, nil
}

func validateCreate(input CreateInput) []ValidationError {
	if _, err := uuid.Parse(input.OrderID); err != nil {
		return []ValidationError{{
			Code:    errorcode.String(commonv1.ErrorCode_ERROR_CODE_INVALID_ARGUMENT),
			Field:   "orderId",
			Message: "Order ID must be a valid UUID.",
		}}
	}
	if strings.TrimSpace(input.UserID) == "" {
		return []ValidationError{{
			Code:    errorcode.String(commonv1.ErrorCode_ERROR_CODE_INVALID_ARGUMENT),
			Field:   "userId",
			Message: "Payment user is required.",
		}}
	}

	return nil
}
