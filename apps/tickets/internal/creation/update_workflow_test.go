package creation

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/testsuite"

	"polyglot-ticketing-v2/apps/tickets/internal/ticket"
)

func TestUpdateWorkflowSerializesProjectionBeforeNextUpdate(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	var activities *Activities
	firstInput := UpdateTicketInput{ID: "ticket-1", IdempotencyKey: "first", Title: "First", Price: 200, UserID: "owner"}
	secondInput := UpdateTicketInput{ID: "ticket-1", IdempotencyKey: "second", Title: "Second", Price: 300, UserID: "owner"}
	first := ticket.Ticket{ID: firstInput.ID, Title: firstInput.Title, Price: firstInput.Price, UserID: firstInput.UserID, AggregateVersion: 2}
	second := ticket.Ticket{ID: secondInput.ID, Title: secondInput.Title, Price: secondInput.Price, UserID: secondInput.UserID, AggregateVersion: 3}
	firstProjected := false

	env.OnActivity(activities.PersistTicketUpdate, mock.Anything, firstInput).Return(first, nil).Once()
	env.OnActivity(activities.ProjectTicketToOrders, mock.Anything, first).Return(errors.New("orders offline")).Once()
	env.OnActivity(activities.ProjectTicketToOrders, mock.Anything, first).Run(func(mock.Arguments) {
		firstProjected = true
	}).Return(nil).Once()
	env.OnActivity(activities.PersistTicketUpdate, mock.Anything, secondInput).Run(func(mock.Arguments) {
		require.True(t, firstProjected)
	}).Return(second, nil).Once()
	env.OnActivity(activities.ProjectTicketToOrders, mock.Anything, second).Return(nil).Once()

	env.RegisterDelayedCallback(func() {
		env.UpdateWorkflowNoRejection(UpdateTicketName, "first", t, firstInput)
		env.UpdateWorkflowNoRejection(UpdateTicketName, "second", t, secondInput)
	}, 0)
	env.RegisterDelayedCallback(env.CancelWorkflow, time.Minute)
	env.ExecuteWorkflow(UpdateTicketWorkflow)

	require.Error(t, env.GetWorkflowError())
	require.True(t, firstProjected)
	env.AssertExpectations(t)
}

func TestPersistTicketUpdateMakesDatabaseRetrySafe(t *testing.T) {
	const ticketID = "9d7d8e0a-7b31-4dd0-8fd8-1a7773f3df7d"
	updater := &updatePersister{result: ticket.Ticket{ID: ticketID, Title: "Updated", Price: 200, UserID: "owner", AggregateVersion: 2}}
	activities := Activities{Repository: updater}
	updated, err := activities.PersistTicketUpdate(context.Background(), UpdateTicketInput{
		ID: ticketID, IdempotencyKey: "retry-key", Title: "Updated", Price: 200, UserID: "owner",
	})
	require.NoError(t, err)
	require.Equal(t, updater.result, updated)
	require.Equal(t, "retry-key", updater.input.IdempotencyKey)
}

type updatePersister struct {
	input  ticket.UpdateInput
	result ticket.Ticket
}

func (persister *updatePersister) Create(_ context.Context, input ticket.CreateInput) (ticket.Ticket, error) {
	return ticket.Ticket{ID: input.ID, Title: input.Title, Price: input.Price, UserID: input.UserID, AggregateVersion: 1}, nil
}

func (*updatePersister) FindByID(context.Context, string) (ticket.Ticket, error) {
	return ticket.Ticket{}, ticket.ErrNotFound
}

func (*updatePersister) List(context.Context) ([]ticket.Ticket, error) {
	return nil, nil
}

func (persister *updatePersister) Update(_ context.Context, id string, input ticket.UpdateInput) (ticket.Ticket, error) {
	return ticket.Ticket{ID: id, Title: input.Title, Price: input.Price, UserID: input.UserID, AggregateVersion: persister.result.AggregateVersion}, nil
}

func (persister *updatePersister) UpdateWithIdempotency(_ context.Context, _ string, input ticket.UpdateInput) (ticket.Ticket, error) {
	persister.input = input
	return persister.result, nil
}

func (persister *updatePersister) ReserveForOrder(context.Context, string, string) error {
	return nil
}

func (persister *updatePersister) ReleaseOrderReservation(context.Context, string, string) error {
	return nil
}
