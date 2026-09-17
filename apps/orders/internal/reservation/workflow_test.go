package reservation

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/testsuite"

	"polyglot-ticketing-v2/apps/orders/internal/order"
)

const (
	testOrderID  = "f446d2f3-4515-4b78-8e6a-81797a2517a3"
	testTicketID = "9d7d8e0a-7b31-4dd0-8fd8-1a7773f3df7d"
)

func TestExpireOrderWorkflowCancelsAndReleasesCreatedReservation(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	start := time.Date(2026, time.September, 16, 12, 0, 0, 0, time.UTC)
	env.SetStartTime(start)
	var activities *Activities
	reserved := order.Order{ID: testOrderID, TicketID: testTicketID, Status: order.StatusCanceled}
	env.OnActivity(activities.ExpireOrder, mock.Anything, testOrderID).
		Return(order.ExpirationResult{Order: reserved, ShouldReleaseTicket: true}, nil).Once()
	env.OnActivity(activities.ReleaseTicket, mock.Anything, reserved).Return(nil).Once()

	env.ExecuteWorkflow(ExpireOrderWorkflow, ExpireInput{
		ExpiresAt: start.Add(order.ExpirationWindow),
		OrderID:   testOrderID,
		TicketID:  testTicketID,
	})

	require.NoError(t, env.GetWorkflowError())
	env.AssertExpectations(t)
}

func TestExpireOrderWorkflowLeavesAwaitingPaymentForPaymentsService(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	var activities *Activities
	env.OnActivity(activities.ExpireOrder, mock.Anything, testOrderID).
		Return(order.ExpirationResult{Order: order.Order{ID: testOrderID, TicketID: testTicketID, Status: order.StatusAwaitingPayment}}, nil).Once()

	env.ExecuteWorkflow(ExpireOrderWorkflow, ExpireInput{
		ExpiresAt: time.Time{},
		OrderID:   testOrderID,
		TicketID:  testTicketID,
	})

	require.NoError(t, env.GetWorkflowError())
	env.AssertExpectations(t)
}

func TestExpirationActivityPreservesAwaitingPayment(t *testing.T) {
	repository := expirationRepository{result: order.ExpirationResult{
		Order:               order.Order{ID: testOrderID, Status: order.StatusAwaitingPayment},
		ShouldReleaseTicket: false,
	}}
	activities := Activities{Repository: repository}
	result, err := activities.ExpireOrder(context.Background(), testOrderID)
	require.NoError(t, err)
	require.False(t, result.ShouldReleaseTicket)
}

func TestResolvePaymentWorkflowReleasesTicketAfterFailedPayment(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	var activities *Activities
	canceled := order.Order{ID: testOrderID, TicketID: testTicketID, Status: order.StatusCanceled}
	env.OnActivity(activities.ResolvePayment, mock.Anything, ResolvePaymentInput{
		OrderID: testOrderID,
		Outcome: order.PaymentOutcomeFailed,
	}).Return(order.PaymentResolutionResult{Order: canceled, ShouldReleaseTicket: true}, nil).Once()
	env.OnActivity(activities.ReleaseTicket, mock.Anything, canceled).Return(nil).Once()

	env.ExecuteWorkflow(ResolvePaymentWorkflow, ResolvePaymentInput{OrderID: testOrderID, Outcome: order.PaymentOutcomeFailed})

	require.NoError(t, env.GetWorkflowError())
	env.AssertExpectations(t)
}

func TestResolvePaymentWorkflowRetainsTicketAfterSuccessfulPayment(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	var activities *Activities
	completed := order.Order{ID: testOrderID, TicketID: testTicketID, Status: order.StatusComplete}
	env.OnActivity(activities.ResolvePayment, mock.Anything, ResolvePaymentInput{
		OrderID: testOrderID,
		Outcome: order.PaymentOutcomeSucceeded,
	}).Return(order.PaymentResolutionResult{Order: completed}, nil).Once()

	env.ExecuteWorkflow(ResolvePaymentWorkflow, ResolvePaymentInput{OrderID: testOrderID, Outcome: order.PaymentOutcomeSucceeded})

	require.NoError(t, env.GetWorkflowError())
	env.AssertExpectations(t)
}

type expirationRepository struct {
	result order.ExpirationResult
}

func (repository expirationRepository) ReserveTicket(context.Context, order.TicketReservationInput) (order.ReservationResult, error) {
	return order.ReservationResult{}, nil
}

func (repository expirationRepository) CancelByIDAndUser(context.Context, string, string) (order.Order, error) {
	return order.Order{}, nil
}

func (repository expirationRepository) ExpireCreatedOrder(context.Context, string) (order.ExpirationResult, error) {
	return repository.result, nil
}

func (expirationRepository) GetByIDAndUser(context.Context, string, string) (order.Order, error) {
	return order.Order{}, nil
}

func (expirationRepository) StartPayment(context.Context, string, string) (order.PaymentOrder, error) {
	return order.PaymentOrder{}, nil
}

func (expirationRepository) ResolvePayment(context.Context, string, order.PaymentOutcome) (order.PaymentResolutionResult, error) {
	return order.PaymentResolutionResult{}, nil
}

func (expirationRepository) ListByUser(context.Context, string) ([]order.Order, error) {
	return nil, nil
}
