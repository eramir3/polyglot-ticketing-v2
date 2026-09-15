package creation

import (
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"

	"polyglot-ticketing-v2/apps/tickets/internal/ticket"
	ordersv1 "polyglot-ticketing-v2/protogen/go/orders/v1"
)

// NewWorker configures the Temporal worker that executes durable ticket
// creation workflows and their activities.
func NewWorker(
	temporalClient client.Client,
	taskQueue string,
	repository ticket.TicketPersister,
	ordersClient ordersv1.OrdersServiceClient,
) worker.Worker {
	temporalWorker := worker.New(temporalClient, taskQueue, worker.Options{})
	temporalWorker.RegisterWorkflow(CreateTicketWorkflow)
	temporalWorker.RegisterActivity(&Activities{
		Repository: repository,
		Orders:     ordersClient,
	})
	return temporalWorker
}
