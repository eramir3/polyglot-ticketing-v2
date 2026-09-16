package creation

import (
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"

	"polyglot-ticketing-v2/apps/tickets/internal/ticket"
	ordersv1 "polyglot-ticketing-v2/protogen/go/orders/v1"
)

// NewWorker configures the Temporal worker that executes durable ticket
// creation and update workflows with the Tickets repository.
func NewWorker(
	temporalClient client.Client,
	taskQueue string,
	repository ticket.Repository,
	ordersClient ordersv1.OrdersServiceClient,
) worker.Worker {
	temporalWorker := worker.New(temporalClient, taskQueue, worker.Options{})
	temporalWorker.RegisterWorkflow(CreateTicketWorkflow)
	temporalWorker.RegisterWorkflow(UpdateTicketWorkflow)
	temporalWorker.RegisterActivity(&Activities{
		Repository: repository,
		Orders:     ordersClient,
	})
	return temporalWorker
}
