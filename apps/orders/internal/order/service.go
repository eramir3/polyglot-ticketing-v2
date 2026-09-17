package order

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"

	"polyglot-ticketing-v2/apps/orders/internal/errorcode"
	commonv1 "polyglot-ticketing-v2/protogen/go/common/v1"
)

type Service struct {
	now         func() time.Time
	repository  OrderRepository
	coordinator Coordinator
}

// Coordinator owns the durable cross-service reservation and cancellation
// operations. The production implementation is backed by Temporal.
type Coordinator interface {
	ReserveTicket(context.Context, TicketReservationInput) (ReservationResult, error)
	CancelOrder(context.Context, string, string) (Order, error)
}

func NewService(repository OrderRepository, coordinator Coordinator) *Service {
	return &Service{now: time.Now, repository: repository, coordinator: coordinator}
}

func (service *Service) ReserveTicket(
	ctx context.Context,
	ticketID string,
	userID string,
) (ReservationResult, []ValidationError, error) {
	if validationErrors := validateTicketReservation(ticketID, userID); len(validationErrors) > 0 {
		return ReservationResult{}, validationErrors, nil
	}

	reservation, err := service.coordinator.ReserveTicket(ctx, TicketReservationInput{
		ExpiresAt: service.now().UTC().Add(ExpirationWindow),
		TicketID:  ticketID,
		UserID:    userID,
	})
	return reservation, nil, err
}

func (service *Service) GetOrder(
	ctx context.Context,
	orderID string,
	userID string,
) (Order, []ValidationError, error) {
	if validationErrors := validateOrderID(orderID); len(validationErrors) > 0 {
		return Order{}, validationErrors, nil
	}
	if validationErrors := validateOrderUser(userID); len(validationErrors) > 0 {
		return Order{}, validationErrors, nil
	}

	found, err := service.repository.GetByIDAndUser(ctx, orderID, userID)
	return found, nil, err
}

func (service *Service) CancelOrder(
	ctx context.Context,
	orderID string,
	userID string,
) (Order, []ValidationError, error) {
	if validationErrors := validateOrderID(orderID); len(validationErrors) > 0 {
		return Order{}, validationErrors, nil
	}
	if validationErrors := validateOrderUser(userID); len(validationErrors) > 0 {
		return Order{}, validationErrors, nil
	}

	canceled, err := service.coordinator.CancelOrder(ctx, orderID, userID)
	return canceled, nil, err
}

func (service *Service) ListOrders(
	ctx context.Context,
	userID string,
) ([]Order, []ValidationError, error) {
	if validationErrors := validateOrderUser(userID); len(validationErrors) > 0 {
		return nil, validationErrors, nil
	}

	orders, err := service.repository.ListByUser(ctx, userID)
	return orders, nil, err
}

func validateTicketReservation(ticketID string, userID string) []ValidationError {
	if _, err := uuid.Parse(ticketID); err != nil {
		return []ValidationError{{
			Code:    errorcode.String(commonv1.ErrorCode_ERROR_CODE_INVALID_ARGUMENT),
			Field:   "ticketId",
			Message: "Ticket ID must be a valid UUID.",
		}}
	}
	return validateOrderUser(userID)
}

func validateOrderID(orderID string) []ValidationError {
	if _, err := uuid.Parse(orderID); err != nil {
		return []ValidationError{{
			Code:    errorcode.String(commonv1.ErrorCode_ERROR_CODE_INVALID_ARGUMENT),
			Field:   "orderId",
			Message: "Order ID must be a valid UUID.",
		}}
	}

	return nil
}

func validateOrderUser(userID string) []ValidationError {
	if strings.TrimSpace(userID) == "" {
		return []ValidationError{{
			Code:    errorcode.String(commonv1.ErrorCode_ERROR_CODE_INVALID_ARGUMENT),
			Field:   "userId",
			Message: "Order user is required.",
		}}
	}

	return nil
}
