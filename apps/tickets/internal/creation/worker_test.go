package creation

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/client"

	"polyglot-ticketing-v2/apps/tickets/internal/ticket"
)

func TestNewWorkerBuildsWorkerWithoutConnecting(t *testing.T) {
	temporalClient, err := client.NewLazyClient(client.Options{})
	require.NoError(t, err)
	defer temporalClient.Close()

	worker := NewWorker(temporalClient, DefaultTaskQueue, fakeRepository{}, nil)
	require.NotNil(t, worker)
}

type fakeRepository struct{}

func (fakeRepository) Create(_ context.Context, input ticket.CreateInput) (ticket.Ticket, error) {
	return ticket.Ticket{ID: input.ID, Title: input.Title, Price: input.Price, UserID: input.UserID}, nil
}

func (fakeRepository) FindByID(context.Context, string) (ticket.Ticket, error) {
	return ticket.Ticket{}, ticket.ErrNotFound
}

func (fakeRepository) List(context.Context) ([]ticket.Ticket, error) {
	return nil, nil
}

func (fakeRepository) Update(_ context.Context, id string, input ticket.UpdateInput) (ticket.Ticket, error) {
	return ticket.Ticket{ID: id, Title: input.Title, Price: input.Price, UserID: input.UserID, AggregateVersion: 2}, nil
}

func (fakeRepository) UpdateWithIdempotency(_ context.Context, id string, input ticket.UpdateInput) (ticket.Ticket, error) {
	return ticket.Ticket{ID: id, Title: input.Title, Price: input.Price, UserID: input.UserID, AggregateVersion: 2}, nil
}
