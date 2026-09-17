package reservation

import (
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"

	"polyglot-ticketing-v2/apps/orders/internal/order"
	ticketsv1 "polyglot-ticketing-v2/protogen/go/tickets/v1"
)

func NewWorker(
	temporalClient client.Client,
	taskQueue string,
	repository order.OrderRepository,
	ticketsClient ticketsv1.TicketsServiceClient,
) worker.Worker {
	temporalWorker := worker.New(temporalClient, taskQueue, worker.Options{})
	temporalWorker.RegisterWorkflow(CreateOrderWorkflow)
	temporalWorker.RegisterWorkflow(CancelOrderWorkflow)
	temporalWorker.RegisterWorkflow(ExpireOrderWorkflow)
	temporalWorker.RegisterActivity(&Activities{Repository: repository, Tickets: ticketsClient})
	return temporalWorker
}
