package payment

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"
)

func TestSettlementWorkerMarksPaymentResolvedAfterOrdersAcknowledges(t *testing.T) {
	outcome := StatusSucceeded
	repository := &fakeSettlementRepository{claimed: Payment{ID: "payment-1", OrderID: "order-1", Status: StatusPending, MockOutcome: &outcome}, found: true}
	orders := &fakeOrdersClient{resolved: OrderStatusComplete}
	worker := NewSettlementWorker(repository, orders, outcome, slog.New(slog.NewTextHandler(io.Discard, nil)))

	worker.settleOne(context.Background())

	if repository.markedPaymentID != "payment-1" || repository.markedOutcome != outcome {
		t.Fatalf("expected resolved payment to be persisted, got %+v", repository)
	}
}

func TestSettlementWorkerLeavesPaymentPendingWhenOrdersFails(t *testing.T) {
	outcome := StatusFailed
	repository := &fakeSettlementRepository{claimed: Payment{ID: "payment-1", OrderID: "order-1", Status: StatusPending, MockOutcome: &outcome}, found: true}
	orders := &fakeOrdersClient{resolveErr: errors.New("Orders unavailable")}
	worker := NewSettlementWorker(repository, orders, outcome, slog.New(slog.NewTextHandler(io.Discard, nil)))

	worker.settleOne(context.Background())

	if repository.markedPaymentID != "" {
		t.Fatalf("expected failed Orders call to leave payment pending, got %+v", repository)
	}
}

type fakeSettlementRepository struct {
	claimed         Payment
	claimErr        error
	found           bool
	markedOutcome   Status
	markedPaymentID string
	markErr         error
}

func (repository *fakeSettlementRepository) ClaimNextPending(_ context.Context, _ Status, _ time.Time) (Payment, bool, error) {
	return repository.claimed, repository.found, repository.claimErr
}

func (repository *fakeSettlementRepository) MarkResolved(_ context.Context, paymentID string, outcome Status) error {
	repository.markedPaymentID = paymentID
	repository.markedOutcome = outcome
	return repository.markErr
}
