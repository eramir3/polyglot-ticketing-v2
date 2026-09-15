package creation

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"

	"polyglot-ticketing-v2/apps/tickets/internal/ticket"
)

func TestWorkflowRetriesProjectionAfterPersisting(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	input := Input{ID: "ticket-id", Title: "Concert", Price: 100, UserID: "owner"}
	created := ticket.Ticket{ID: input.ID, Title: input.Title, Price: input.Price, UserID: input.UserID}
	var activities *Activities
	persisted := false
	env.OnActivity(activities.PersistTicket, mock.Anything, input).Return(func(context.Context, Input) (ticket.Ticket, error) {
		persisted = true
		return created, nil
	}).Once()
	env.OnActivity(activities.ProjectTicketToOrders, mock.Anything, created).Return(func(context.Context, ticket.Ticket) error {
		require.True(t, persisted)
		return errors.New("orders offline")
	}).Once()
	env.OnActivity(activities.ProjectTicketToOrders, mock.Anything, created).Return(nil).Once()
	env.ExecuteWorkflow(CreateTicketWorkflow, input)
	require.NoError(t, env.GetWorkflowError())
	var result ticket.Ticket
	require.NoError(t, env.GetWorkflowResult(&result))
	require.Equal(t, created, result)
	env.AssertExpectations(t)
}

func TestWorkflowStopsOnPermanentPersistenceError(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	var activities *Activities
	env.OnActivity(activities.PersistTicket, mock.Anything, mock.Anything).
		Return(ticket.Ticket{}, temporal.NewNonRetryableApplicationError("invalid", "InvalidTicket", nil)).Once()
	env.ExecuteWorkflow(CreateTicketWorkflow, Input{})
	require.Error(t, env.GetWorkflowError())
	env.AssertExpectations(t)
}

func TestWorkflowCannotSucceedIfProjectionPermanentlyFails(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	var activities *Activities
	env.OnActivity(activities.PersistTicket, mock.Anything, mock.Anything).Return(ticket.Ticket{ID: "created"}, nil).Once()
	env.OnActivity(activities.ProjectTicketToOrders, mock.Anything, mock.Anything).
		Return(temporal.NewNonRetryableApplicationError("conflict", "InvalidProjection", nil)).Once()
	env.ExecuteWorkflow(CreateTicketWorkflow, Input{})
	require.Error(t, env.GetWorkflowError())
	env.AssertExpectations(t)
}
