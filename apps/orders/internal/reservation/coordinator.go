package reservation

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"go.temporal.io/api/serviceerror"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/temporal"

	"polyglot-ticketing-v2/apps/orders/internal/order"
)

const DefaultTaskQueue = "order-reservation"

// Coordinator starts durable order reservation operations. Its request timeout
// only bounds the caller's wait; accepted workflows keep retrying afterward.
type Coordinator struct {
	Client    client.Client
	TaskQueue string
}

func (coordinator *Coordinator) ReserveTicket(ctx context.Context, input order.TicketReservationInput) (order.ReservationResult, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	input.OrderID = uuid.NewString()
	run, err := coordinator.Client.ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID:        "create-order/" + input.OrderID,
		TaskQueue: coordinator.TaskQueue,
	}, CreateOrderWorkflow, input)
	if err != nil {
		return order.ReservationResult{}, order.ErrUnavailable
	}
	var result order.ReservationResult
	if err := run.Get(ctx, &result); err != nil {
		if ctx.Err() != nil {
			return order.ReservationResult{}, order.ErrUnavailable
		}
		return order.ReservationResult{}, reservationWorkflowError(err)
	}
	return result, nil
}

func (coordinator *Coordinator) CancelOrder(ctx context.Context, orderID string, userID string) (order.Order, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	run, err := coordinator.Client.ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID:        "cancel-order/" + orderID,
		TaskQueue: coordinator.TaskQueue,
	}, CancelOrderWorkflow, CancelInput{OrderID: orderID, UserID: userID})
	var alreadyStarted *serviceerror.WorkflowExecutionAlreadyStarted
	if errors.As(err, &alreadyStarted) {
		run = coordinator.Client.GetWorkflow(ctx, "cancel-order/"+orderID, alreadyStarted.RunId)
	} else if err != nil {
		return order.Order{}, order.ErrUnavailable
	}
	var result order.Order
	if err := run.Get(ctx, &result); err != nil {
		if ctx.Err() != nil {
			return order.Order{}, order.ErrUnavailable
		}
		return order.Order{}, reservationWorkflowError(err)
	}
	return result, nil
}

func reservationWorkflowError(err error) error {
	var applicationFailure *temporal.ApplicationError
	if errors.As(err, &applicationFailure) {
		switch applicationFailure.Type() {
		case "ProjectedTicketNotFound":
			return order.ErrNotFound
		case "TicketAlreadyReserved":
			return order.ErrReserved
		case "OrderNotFound":
			return order.ErrOrderNotFound
		case "OrderNotCancelable":
			return order.ErrOrderNotCancelable
		}
	}
	return fmt.Errorf("%w: %v", order.ErrUnavailable, err)
}
