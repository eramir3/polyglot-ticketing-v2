package creation

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.temporal.io/api/serviceerror"
	"go.temporal.io/sdk/mocks"

	"polyglot-ticketing-v2/apps/tickets/internal/ticket"
)

func TestCoordinatorStartFailureDoesNotRunActivities(t *testing.T) {
	c := &mocks.Client{}
	c.On("ExecuteWorkflow", mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(nil, serviceerror.NewUnavailable("offline")).Once()
	coordinator := Coordinator{Client: c, TaskQueue: DefaultTaskQueue}
	_, err := coordinator.Create(context.Background(), ticket.CreateInput{Title: "Concert", Price: 100, UserID: "owner"})
	require.ErrorIs(t, err, ticket.ErrUnavailable)
	c.AssertExpectations(t)
	c.AssertNotCalled(t, "GetWorkflow", mock.Anything, mock.Anything, mock.Anything)
}

func TestCoordinatorReusesExistingWorkflowWithoutComparingPayload(t *testing.T) {
	c := &mocks.Client{}
	key := "request-key"
	run := &mocks.WorkflowRun{}
	c.On("ExecuteWorkflow", mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(nil, &serviceerror.WorkflowExecutionAlreadyStarted{RunId: "existing-run"}).Once()
	c.On("GetWorkflow", mock.Anything, mock.Anything, "existing-run").Return(run).Once()
	run.On("Get", mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
		*args.Get(1).(*ticket.Ticket) = ticket.Ticket{ID: "original", Title: "Original", Price: 100, UserID: "owner"}
	}).Return(nil).Once()
	coordinator := Coordinator{Client: c, TaskQueue: DefaultTaskQueue}
	result, err := coordinator.Create(context.Background(), ticket.CreateInput{Title: "Changed", Price: 200, UserID: "owner", IdempotencyKey: &key})
	require.NoError(t, err)
	require.Equal(t, ticket.Ticket{ID: "original", Title: "Original", Price: 100, UserID: "owner"}, result)
	c.AssertNotCalled(t, "DescribeWorkflowExecution", mock.Anything, mock.Anything, mock.Anything)
	c.AssertExpectations(t)
}

func TestCoordinatorCanceledWaitDoesNotCancelWorkflow(t *testing.T) {
	c := &mocks.Client{}
	run := &mocks.WorkflowRun{}
	ctx, cancel := context.WithCancel(context.Background())
	c.On("ExecuteWorkflow", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(run, nil).Once()
	run.On("Get", mock.Anything, mock.Anything).Run(func(mock.Arguments) { cancel() }).Return(context.Canceled).Once()
	coordinator := Coordinator{Client: c, TaskQueue: DefaultTaskQueue}
	_, err := coordinator.Create(ctx, ticket.CreateInput{})
	require.True(t, errors.Is(err, ticket.ErrUnavailable))
	c.AssertNotCalled(t, "CancelWorkflow", mock.Anything, mock.Anything, mock.Anything)
	c.AssertExpectations(t)
}
