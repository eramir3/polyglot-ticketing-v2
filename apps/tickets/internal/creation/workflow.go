// Package creation owns the durable ticket creation process.
package creation

import (
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"polyglot-ticketing-v2/apps/tickets/internal/ticket"
)

const DefaultTaskQueue = "ticket-creation"

type Input struct {
	ID     string
	Title  string
	Price  int64
	UserID string
}

func CreateTicketWorkflow(ctx workflow.Context, input Input) (ticket.Ticket, error) {
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 10 * time.Second,
		RetryPolicy:         &temporal.RetryPolicy{InitialInterval: time.Second, BackoffCoefficient: 2, MaximumInterval: 30 * time.Second},
	})
	var activities *Activities
	var created ticket.Ticket
	if err := workflow.ExecuteActivity(ctx, activities.PersistTicket, input).Get(ctx, &created); err != nil {
		return ticket.Ticket{}, err
	}
	if err := workflow.ExecuteActivity(ctx, activities.ProjectTicketToOrders, created).Get(ctx, nil); err != nil {
		return ticket.Ticket{}, err
	}
	return created, nil
}
