package creation

import (
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"

	"polyglot-ticketing-v2/apps/payments/internal/payment"
)

func NewWorker(
	temporalClient client.Client,
	taskQueue string,
	repository payment.Repository,
	orders payment.OrdersClient,
) worker.Worker {
	temporalWorker := worker.New(temporalClient, taskQueue, worker.Options{})
	temporalWorker.RegisterWorkflow(CreatePaymentWorkflow)
	temporalWorker.RegisterActivity(&Activities{Orders: orders, Repository: repository})
	return temporalWorker
}
