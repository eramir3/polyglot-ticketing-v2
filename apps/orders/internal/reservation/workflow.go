package reservation

import (
	"time"

	enums "go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"polyglot-ticketing-v2/apps/orders/internal/order"
)

type CancelInput struct {
	OrderID string
	UserID  string
}

type ExpireInput struct {
	ExpiresAt time.Time
	OrderID   string
	TicketID  string
}

func activityContext(ctx workflow.Context) workflow.Context {
	return workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 10 * time.Second,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    time.Second,
			BackoffCoefficient: 2,
			MaximumInterval:    30 * time.Second,
		},
	})
}

// CreateOrderWorkflow does not complete until the Tickets-owned source row is
// claimed. This prevents a successful order response from leaving an editable
// ticket behind. It starts a child workflow to handle the long-lived expiration
// timer independently, allowing CreateOrderWorkflow to return without waiting
// for the order to expire.
func CreateOrderWorkflow(ctx workflow.Context, input order.TicketReservationInput) (order.ReservationResult, error) {
	activitiesContext := activityContext(ctx)
	var activities *Activities
	var reservation order.ReservationResult
	if err := workflow.ExecuteActivity(activitiesContext, activities.ReserveOrder, input).Get(ctx, &reservation); err != nil {
		return order.ReservationResult{}, err
	}
	if err := workflow.ExecuteActivity(activitiesContext, activities.ClaimTicket, reservation.Order).Get(ctx, nil); err != nil {
		return order.ReservationResult{}, err
	}
	if !reservation.Created {
		return reservation, nil
	}

	childContext := workflow.WithChildOptions(ctx, workflow.ChildWorkflowOptions{
		WorkflowID:        "expire-order/" + reservation.Order.ID,
		ParentClosePolicy: enums.PARENT_CLOSE_POLICY_ABANDON,
	})
	child := workflow.ExecuteChildWorkflow(childContext, ExpireOrderWorkflow, ExpireInput{
		ExpiresAt: reservation.Order.ExpiresAt,
		OrderID:   reservation.Order.ID,
		TicketID:  reservation.Order.TicketID,
	})
	if err := child.GetChildWorkflowExecution().Get(ctx, nil); err != nil {
		return order.ReservationResult{}, err
	}
	return reservation, nil
}

// CancelOrderWorkflow persists Canceled before releasing the ticket. If the
// release activity is unavailable, it retries while the ticket remains safely
// locked.
func CancelOrderWorkflow(ctx workflow.Context, input CancelInput) (order.Order, error) {
	activitiesContext := activityContext(ctx)
	var activities *Activities
	var canceled order.Order
	if err := workflow.ExecuteActivity(activitiesContext, activities.CancelOrder, input).Get(ctx, &canceled); err != nil {
		return order.Order{}, err
	}
	if err := workflow.ExecuteActivity(activitiesContext, activities.ReleaseTicket, canceled).Get(ctx, nil); err != nil {
		return order.Order{}, err
	}
	return canceled, nil
}

// ExpireOrderWorkflow owns the 15-minute Created-order deadline. It changes
// only a still-Created order to Canceled; later payment states are left for the
// Payments service to manage.
func ExpireOrderWorkflow(ctx workflow.Context, input ExpireInput) error {
	delay := input.ExpiresAt.Sub(workflow.Now(ctx))
	if delay > 0 {
		if err := workflow.Sleep(ctx, delay); err != nil {
			return err
		}
	}
	activitiesContext := activityContext(ctx)
	var activities *Activities
	var expiration order.ExpirationResult
	if err := workflow.ExecuteActivity(activitiesContext, activities.ExpireOrder, input.OrderID).Get(ctx, &expiration); err != nil {
		return err
	}
	if !expiration.ShouldReleaseTicket {
		return nil
	}
	return workflow.ExecuteActivity(activitiesContext, activities.ReleaseTicket, expiration.Order).Get(ctx, nil)
}
