package payment

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
)

func TestServiceStartsPaymentCreationWorkflow(t *testing.T) {
	orderID := uuid.NewString()
	repository := &fakeRepository{}
	coordinator := &fakeCreateCoordinator{result: CreateResult{Payment: Payment{ID: uuid.NewString(), OrderID: orderID}, Created: true}}

	created, wasCreated, validationErrors, err := NewService(repository, coordinator).CreatePayment(context.Background(), CreateInput{
		OrderID: orderID,
		UserID:  "user-1",
	})

	if err != nil || len(validationErrors) != 0 || !wasCreated || created != coordinator.result.Payment {
		t.Fatalf("create payment: payment=%+v created=%t errors=%+v err=%v", created, wasCreated, validationErrors, err)
	}
	if coordinator.input.OrderID != orderID || coordinator.input.UserID != "user-1" || repository.called {
		t.Fatalf("unexpected coordination: coordinator=%+v repository=%+v", coordinator, repository)
	}
}

func TestServiceReturnsExistingPaymentWithoutRestartingTerminalOrder(t *testing.T) {
	orderID := uuid.NewString()
	repository := &fakeRepository{found: true, payment: Payment{ID: uuid.NewString(), OrderID: orderID, Status: StatusSucceeded}}
	coordinator := &fakeCreateCoordinator{}

	payment, created, validationErrors, err := NewService(repository, coordinator).CreatePayment(context.Background(), CreateInput{
		OrderID: orderID,
		UserID:  "user-1",
	})

	if err != nil || len(validationErrors) != 0 || created || payment != repository.payment {
		t.Fatalf("replay payment: payment=%+v created=%t errors=%+v err=%v", payment, created, validationErrors, err)
	}
	if coordinator.called || repository.called {
		t.Fatal("existing payment must not start a workflow or create another payment")
	}
}

func TestServiceRejectsInvalidPaymentInputBeforeStartingOrder(t *testing.T) {
	repository := &fakeRepository{}
	coordinator := &fakeCreateCoordinator{}
	_, _, validationErrors, err := NewService(repository, coordinator).CreatePayment(context.Background(), CreateInput{
		OrderID: "not-a-uuid",
		UserID:  "user-1",
	})

	if err != nil || len(validationErrors) != 1 || validationErrors[0].Field != "orderId" {
		t.Fatalf("expected order ID validation error, got errors=%+v err=%v", validationErrors, err)
	}
	if coordinator.called || repository.called {
		t.Fatal("invalid payment input reached a dependency")
	}
}

func TestServiceReturnsWorkflowError(t *testing.T) {
	coordinator := &fakeCreateCoordinator{err: ErrOrderNotPayable}
	_, _, validationErrors, err := NewService(&fakeRepository{}, coordinator).CreatePayment(context.Background(), CreateInput{
		OrderID: uuid.NewString(),
		UserID:  "user-1",
	})

	if len(validationErrors) != 0 || !errors.Is(err, ErrOrderNotPayable) {
		t.Fatalf("expected payable error, got errors=%+v err=%v", validationErrors, err)
	}
}

type fakeCreateCoordinator struct {
	called bool
	err    error
	input  CreateInput
	result CreateResult
}

func (coordinator *fakeCreateCoordinator) CreatePayment(_ context.Context, input CreateInput) (CreateResult, error) {
	coordinator.called = true
	coordinator.input = input
	return coordinator.result, coordinator.err
}

type fakeRepository struct {
	called  bool
	created bool
	err     error
	found   bool
	input   CreateInput
	order   Order
	payment Payment
}

func (repository *fakeRepository) FindByOrderAndUser(_ context.Context, _ string, _ string) (Payment, bool, error) {
	return repository.payment, repository.found, nil
}

func (repository *fakeRepository) Create(_ context.Context, input CreateInput, order Order) (Payment, bool, error) {
	repository.called = true
	repository.input = input
	repository.order = order
	return repository.payment, repository.created, repository.err
}

type fakeOrdersClient struct {
	input         CreateInput
	resolveErr    error
	resolved      OrderStatus
	startErr      error
	started       Order
	startedCalled bool
}

func (client *fakeOrdersClient) StartPayment(_ context.Context, input CreateInput) (Order, error) {
	client.startedCalled = true
	client.input = input
	return client.started, client.startErr
}

func (client *fakeOrdersClient) ResolvePayment(_ context.Context, _ string, _ Status) (OrderStatus, error) {
	return client.resolved, client.resolveErr
}
