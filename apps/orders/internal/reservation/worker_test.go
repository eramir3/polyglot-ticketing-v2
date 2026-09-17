package reservation

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/client"

	"polyglot-ticketing-v2/apps/orders/internal/order"
)

func TestNewWorkerBuildsWorkerWithoutConnecting(t *testing.T) {
	temporalClient, err := client.NewLazyClient(client.Options{})
	require.NoError(t, err)
	defer temporalClient.Close()

	worker := NewWorker(temporalClient, DefaultTaskQueue, workerRepository{}, nil)
	require.NotNil(t, worker)
}

type workerRepository struct{}

func (workerRepository) ReserveTicket(context.Context, order.TicketReservationInput) (order.ReservationResult, error) {
	return order.ReservationResult{}, nil
}

func (workerRepository) CancelByIDAndUser(context.Context, string, string) (order.Order, error) {
	return order.Order{}, nil
}

func (workerRepository) ExpireCreatedOrder(context.Context, string) (order.ExpirationResult, error) {
	return order.ExpirationResult{}, nil
}

func (workerRepository) StartPayment(context.Context, string, string) (order.PaymentOrder, error) {
	return order.PaymentOrder{}, nil
}

func (workerRepository) ResolvePayment(context.Context, string, order.PaymentOutcome) (order.PaymentResolutionResult, error) {
	return order.PaymentResolutionResult{}, nil
}

func (workerRepository) GetByIDAndUser(context.Context, string, string) (order.Order, error) {
	return order.Order{}, nil
}

func (workerRepository) ListByUser(context.Context, string) ([]order.Order, error) {
	return nil, nil
}
