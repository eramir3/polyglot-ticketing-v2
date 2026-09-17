package creation

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"

	"polyglot-ticketing-v2/apps/payments/internal/payment"
)

func TestCreatePaymentWorkflowStartsOrderThenPersistsPayment(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	var activities *Activities
	input := payment.CreateInput{OrderID: "f446d2f3-4515-4b78-8e6a-81797a2517a3", UserID: "user-1"}
	started := payment.Order{ID: input.OrderID, Price: 10_000, Status: payment.OrderStatusAwaitingPayment, UserID: input.UserID}
	want := payment.CreateResult{Payment: payment.Payment{ID: "payment-1", OrderID: input.OrderID, Status: payment.StatusPending}, Created: true}
	env.OnActivity(activities.StartOrderPayment, mock.Anything, input).Return(started, nil).Once()
	env.OnActivity(activities.PersistPayment, mock.Anything, input, started).Return(want, nil).Once()

	env.ExecuteWorkflow(CreatePaymentWorkflow, input)

	var got payment.CreateResult
	require.NoError(t, env.GetWorkflowError())
	require.NoError(t, env.GetWorkflowResult(&got))
	require.Equal(t, want, got)
	env.AssertExpectations(t)
}

func TestCreatePaymentWorkflowDoesNotRetryOrdersRejection(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	var activities *Activities
	input := payment.CreateInput{OrderID: "f446d2f3-4515-4b78-8e6a-81797a2517a3", UserID: "user-1"}
	env.OnActivity(activities.StartOrderPayment, mock.Anything, input).
		Return(payment.Order{}, temporal.NewNonRetryableApplicationError("Order cannot be paid", "OrderNotPayable", payment.ErrOrderNotPayable)).Once()

	env.ExecuteWorkflow(CreatePaymentWorkflow, input)

	require.Error(t, env.GetWorkflowError())
	env.AssertExpectations(t)
}

func TestCreatePaymentWorkflowRetriesTransientOrderStartFailure(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	var activities *Activities
	input := payment.CreateInput{OrderID: "f446d2f3-4515-4b78-8e6a-81797a2517a3", UserID: "user-1"}
	started := payment.Order{ID: input.OrderID, Price: 10_000, Status: payment.OrderStatusAwaitingPayment, UserID: input.UserID}
	want := payment.CreateResult{Payment: payment.Payment{ID: "payment-1", OrderID: input.OrderID, Status: payment.StatusPending}, Created: true}
	env.OnActivity(activities.StartOrderPayment, mock.Anything, input).Return(payment.Order{}, errors.New("Orders unavailable")).Once()
	env.OnActivity(activities.StartOrderPayment, mock.Anything, input).Return(started, nil).Once()
	env.OnActivity(activities.PersistPayment, mock.Anything, input, started).Return(want, nil).Once()

	env.ExecuteWorkflow(CreatePaymentWorkflow, input)

	var got payment.CreateResult
	require.NoError(t, env.GetWorkflowError())
	require.NoError(t, env.GetWorkflowResult(&got))
	require.Equal(t, want, got)
	env.AssertExpectations(t)
}

func TestStartOrderPaymentClassifiesOrdersDomainErrorsAsNonRetryable(t *testing.T) {
	activities := &Activities{Orders: &ordersClient{startErr: payment.ErrOrderNotFound}}
	_, err := activities.StartOrderPayment(context.Background(), payment.CreateInput{})

	var applicationError *temporal.ApplicationError
	require.True(t, errors.As(err, &applicationError))
	require.Equal(t, "OrderNotFound", applicationError.Type())
}

func TestCreatePaymentWorkflowIDIsScopedToOrderAndUser(t *testing.T) {
	first := payment.CreateInput{OrderID: "order-1", UserID: "user-1"}
	if createPaymentWorkflowID(first) != createPaymentWorkflowID(first) {
		t.Fatal("expected the same input to produce the same workflow ID")
	}
	if createPaymentWorkflowID(first) == createPaymentWorkflowID(payment.CreateInput{OrderID: first.OrderID, UserID: "user-2"}) {
		t.Fatal("expected different users to have distinct workflow IDs")
	}
}

type ordersClient struct {
	startErr error
}

func (client *ordersClient) StartPayment(context.Context, payment.CreateInput) (payment.Order, error) {
	return payment.Order{}, client.startErr
}

func (*ordersClient) ResolvePayment(context.Context, string, payment.Status) (payment.OrderStatus, error) {
	return "", nil
}
