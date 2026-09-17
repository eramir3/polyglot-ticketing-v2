package reservation

import (
	"context"
	"errors"

	"go.temporal.io/sdk/temporal"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"polyglot-ticketing-v2/apps/orders/internal/order"
	ticketsv1 "polyglot-ticketing-v2/protogen/go/tickets/v1"
)

type Activities struct {
	Repository order.OrderRepository
	Tickets    ticketsv1.TicketsServiceClient
}

func (activities *Activities) ReserveOrder(ctx context.Context, input order.TicketReservationInput) (order.ReservationResult, error) {
	result, err := activities.Repository.ReserveTicket(ctx, input)
	if errors.Is(err, order.ErrNotFound) {
		return order.ReservationResult{}, temporal.NewNonRetryableApplicationError("Projected ticket not found", "ProjectedTicketNotFound", err)
	}
	if errors.Is(err, order.ErrReserved) {
		return order.ReservationResult{}, temporal.NewNonRetryableApplicationError("Ticket already reserved", "TicketAlreadyReserved", err)
	}
	return result, err
}

func (activities *Activities) ClaimTicket(ctx context.Context, reservation order.Order) error {
	_, err := activities.Tickets.ReserveTicketForOrder(ctx, &ticketsv1.ReserveTicketForOrderRequest{
		TicketId: reservation.TicketID,
		OrderId:  reservation.ID,
	})
	return reservationRPCError(err)
}

func (activities *Activities) CancelOrder(ctx context.Context, input CancelInput) (order.Order, error) {
	canceled, err := activities.Repository.CancelByIDAndUser(ctx, input.OrderID, input.UserID)
	if errors.Is(err, order.ErrOrderNotFound) {
		return order.Order{}, temporal.NewNonRetryableApplicationError("Order not found", "OrderNotFound", err)
	}
	if errors.Is(err, order.ErrOrderNotCancelable) {
		return order.Order{}, temporal.NewNonRetryableApplicationError("Order cannot be canceled", "OrderNotCancelable", err)
	}
	return canceled, err
}

func (activities *Activities) ExpireOrder(ctx context.Context, orderID string) (order.ExpirationResult, error) {
	result, err := activities.Repository.ExpireCreatedOrder(ctx, orderID)
	if errors.Is(err, order.ErrOrderNotFound) {
		return order.ExpirationResult{}, temporal.NewNonRetryableApplicationError("Order not found", "OrderNotFound", err)
	}
	return result, err
}

func (activities *Activities) ResolvePayment(ctx context.Context, input ResolvePaymentInput) (order.PaymentResolutionResult, error) {
	result, err := activities.Repository.ResolvePayment(ctx, input.OrderID, input.Outcome)
	if errors.Is(err, order.ErrOrderNotFound) {
		return order.PaymentResolutionResult{}, temporal.NewNonRetryableApplicationError("Order not found", "OrderNotFound", err)
	}
	if errors.Is(err, order.ErrOrderNotPayable) {
		return order.PaymentResolutionResult{}, temporal.NewNonRetryableApplicationError("Order cannot be resolved by payment", "OrderNotPayable", err)
	}
	return result, err
}

func (activities *Activities) ReleaseTicket(ctx context.Context, found order.Order) error {
	_, err := activities.Tickets.ReleaseTicketReservation(ctx, &ticketsv1.ReleaseTicketReservationRequest{
		TicketId: found.TicketID,
		OrderId:  found.ID,
	})
	return reservationRPCError(err)
}

func reservationRPCError(err error) error {
	switch status.Code(err) {
	case codes.NotFound:
		return temporal.NewNonRetryableApplicationError("Projected ticket not found", "ProjectedTicketNotFound", err)
	case codes.AlreadyExists:
		return temporal.NewNonRetryableApplicationError("Ticket already reserved", "TicketAlreadyReserved", err)
	case codes.InvalidArgument, codes.PermissionDenied, codes.Unauthenticated, codes.Unimplemented:
		return temporal.NewNonRetryableApplicationError("Tickets rejected reservation operation", "InvalidTicketReservation", err)
	default:
		return err
	}
}
