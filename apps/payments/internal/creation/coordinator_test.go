package creation

import (
	"context"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.temporal.io/api/serviceerror"
	"go.temporal.io/sdk/client"
	temporalmocks "go.temporal.io/sdk/mocks"

	"polyglot-ticketing-v2/apps/payments/internal/payment"
)

func TestCoordinatorStartsPaymentCreationWorkflow(t *testing.T) {
	input := payment.CreateInput{OrderID: "f446d2f3-4515-4b78-8e6a-81797a2517a3", UserID: "user-1"}
	want := payment.CreateResult{Payment: payment.Payment{ID: "payment-1", OrderID: input.OrderID}, Created: true}
	temporalClient := &temporalmocks.Client{}
	run := &temporalmocks.WorkflowRun{}
	temporalClient.On(
		"ExecuteWorkflow",
		mock.Anything,
		mock.MatchedBy(func(options client.StartWorkflowOptions) bool {
			return options.ID == createPaymentWorkflowID(input) && options.TaskQueue == DefaultTaskQueue
		}),
		mock.Anything,
		input,
	).Return(run, nil).Once()
	run.On("Get", mock.Anything, mock.Anything).Return(func(_ context.Context, value any) error {
		*value.(*payment.CreateResult) = want
		return nil
	}).Once()

	result, err := (&Coordinator{Client: temporalClient, TaskQueue: DefaultTaskQueue}).CreatePayment(context.Background(), input)

	require.NoError(t, err)
	require.Equal(t, want, result)
	temporalClient.AssertExpectations(t)
	run.AssertExpectations(t)
}

func TestCoordinatorJoinsAnExistingPaymentCreationWorkflow(t *testing.T) {
	input := payment.CreateInput{OrderID: "f446d2f3-4515-4b78-8e6a-81797a2517a3", UserID: "user-1"}
	want := payment.CreateResult{Payment: payment.Payment{ID: "payment-1", OrderID: input.OrderID}, Created: true}
	temporalClient := &temporalmocks.Client{}
	run := &temporalmocks.WorkflowRun{}
	workflowID := createPaymentWorkflowID(input)
	temporalClient.On("ExecuteWorkflow", mock.Anything, mock.Anything, mock.Anything, input).
		Return(nil, serviceerror.NewWorkflowExecutionAlreadyStarted("already running", "request-1", "run-1")).Once()
	temporalClient.On("GetWorkflow", mock.Anything, workflowID, "run-1").Return(run).Once()
	run.On("Get", mock.Anything, mock.Anything).Return(func(_ context.Context, value any) error {
		*value.(*payment.CreateResult) = want
		return nil
	}).Once()

	result, err := (&Coordinator{Client: temporalClient, TaskQueue: DefaultTaskQueue}).CreatePayment(context.Background(), input)

	require.NoError(t, err)
	require.Equal(t, want, result)
	temporalClient.AssertExpectations(t)
	run.AssertExpectations(t)
}
