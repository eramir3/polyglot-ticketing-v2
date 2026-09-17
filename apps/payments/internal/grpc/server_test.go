package grpc

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"polyglot-ticketing-v2/apps/payments/internal/payment"
	paymentsv1 "polyglot-ticketing-v2/protogen/go/payments/v1"
)

func TestCreatePaymentLogsUnexpectedFailure(t *testing.T) {
	const orderID = "f446d2f3-4515-4b78-8e6a-81797a2517a3"

	var logs bytes.Buffer
	server := NewServer(
		payment.NewService(failingPaymentRepository{}, failingCreateCoordinator{}),
		slog.New(slog.NewTextHandler(&logs, nil)),
	)

	_, err := server.CreatePayment(context.Background(), &paymentsv1.CreatePaymentRequest{
		OrderId: orderID,
		UserId:  "user-1",
	})

	if status.Code(err) != codes.Internal {
		t.Fatalf("expected internal status, got %v", status.Code(err))
	}
	for _, expected := range []string{
		"level=ERROR",
		"msg=\"payment creation failed\"",
		"operation=create_payment",
		"order_id=" + orderID,
		"error=\"database unavailable\"",
	} {
		if !strings.Contains(logs.String(), expected) {
			t.Fatalf("expected log output to contain %q, got %q", expected, logs.String())
		}
	}
}

func TestCreatePaymentReturnsUnavailableWhileWorkflowContinues(t *testing.T) {
	server := NewServer(
		payment.NewService(failingPaymentRepository{}, unavailableCreateCoordinator{}),
		slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)),
	)

	_, err := server.CreatePayment(context.Background(), &paymentsv1.CreatePaymentRequest{
		OrderId: "f446d2f3-4515-4b78-8e6a-81797a2517a3",
		UserId:  "user-1",
	})

	if status.Code(err) != codes.Unavailable {
		t.Fatalf("expected unavailable status, got %v", status.Code(err))
	}
}

type failingPaymentRepository struct{}

func (failingPaymentRepository) Create(context.Context, payment.CreateInput, payment.Order) (payment.Payment, bool, error) {
	return payment.Payment{}, false, errors.New("database unavailable")
}

func (failingPaymentRepository) FindByOrderAndUser(context.Context, string, string) (payment.Payment, bool, error) {
	return payment.Payment{}, false, nil
}

type failingCreateCoordinator struct{}

func (failingCreateCoordinator) CreatePayment(context.Context, payment.CreateInput) (payment.CreateResult, error) {
	return payment.CreateResult{}, errors.New("database unavailable")
}

type unavailableCreateCoordinator struct{}

func (unavailableCreateCoordinator) CreatePayment(context.Context, payment.CreateInput) (payment.CreateResult, error) {
	return payment.CreateResult{}, payment.ErrUnavailable
}
