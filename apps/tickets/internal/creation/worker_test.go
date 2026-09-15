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

	worker := NewWorker(temporalClient, DefaultTaskQueue, fakePersister{}, nil)
	require.NotNil(t, worker)
}

type fakePersister struct{}

func (fakePersister) Create(_ context.Context, input ticket.CreateInput) (ticket.Ticket, error) {
	return ticket.Ticket{ID: input.ID, Title: input.Title, Price: input.Price, UserID: input.UserID}, nil
}
