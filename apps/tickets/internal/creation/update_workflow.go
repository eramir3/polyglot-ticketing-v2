package creation

import (
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"polyglot-ticketing-v2/apps/tickets/internal/ticket"
)

const (
	UpdateTicketName      = "update-ticket"
	maxUpdatesPerWorkflow = 100
)

// UpdateTicketInput is the immutable payload of one idempotent ticket update.
// The workflow ID serializes all such payloads for one ticket.
type UpdateTicketInput struct {
	ID             string
	IdempotencyKey string
	Title          string
	Price          int64
	UserID         string
}

// UpdateTicketWorkflow stays open to receive serialized updates for one ticket.
func UpdateTicketWorkflow(ctx workflow.Context) error {
	activitiesContext := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 10 * time.Second,
		RetryPolicy:         &temporal.RetryPolicy{InitialInterval: time.Second, BackoffCoefficient: 2, MaximumInterval: 30 * time.Second},
	})
	mutex := workflow.NewMutex(ctx)
	completedUpdates := 0
	var activities *Activities
	if err := workflow.SetUpdateHandler(ctx, UpdateTicketName, func(ctx workflow.Context, input UpdateTicketInput) (ticket.Ticket, error) {
		// The mutex lock serializes updates for this ticket so only one update can execute the
		// persist-and-project sequence at a time. Workflows for other tickets
		// have their own mutexes and can continue concurrently.
		mutex.Lock(ctx)
		defer mutex.Unlock()

		var updated ticket.Ticket
		if err := workflow.ExecuteActivity(activitiesContext, activities.PersistTicketUpdate, input).Get(ctx, &updated); err != nil {
			return ticket.Ticket{}, err
		}
		if err := workflow.ExecuteActivity(activitiesContext, activities.ProjectTicketToOrders, updated).Get(ctx, nil); err != nil {
			return ticket.Ticket{}, err
		}
		completedUpdates++
		return updated, nil
	}); err != nil {
		return err
	}

	if err := workflow.Await(ctx, func() bool { return completedUpdates >= maxUpdatesPerWorkflow }); err != nil {
		return err
	}
	return workflow.NewContinueAsNewError(ctx, UpdateTicketWorkflow)
}
