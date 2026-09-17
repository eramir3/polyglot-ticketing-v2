package payment

import (
	"context"
	"log/slog"
	"time"
)

const (
	defaultSettlementLease = 10 * time.Second
	defaultSettlementPoll  = time.Second
)

type SettlementWorker struct {
	leaseDuration time.Duration
	logger        *slog.Logger
	orders        OrdersClient
	outcome       Status
	pollInterval  time.Duration
	repository    SettlementRepository
}

func NewSettlementWorker(repository SettlementRepository, orders OrdersClient, outcome Status, logger *slog.Logger) *SettlementWorker {
	return &SettlementWorker{
		leaseDuration: defaultSettlementLease,
		logger:        logger,
		orders:        orders,
		outcome:       outcome,
		pollInterval:  defaultSettlementPoll,
		repository:    repository,
	}
}

func (worker *SettlementWorker) Run(ctx context.Context) {
	ticker := time.NewTicker(worker.pollInterval)
	defer ticker.Stop()
	for {
		worker.settleOne(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (worker *SettlementWorker) settleOne(ctx context.Context) {
	claimed, found, err := worker.repository.ClaimNextPending(ctx, worker.outcome, time.Now().UTC().Add(worker.leaseDuration))
	if err != nil {
		worker.logger.Error("payment settlement claim failed", "error", err)
		return
	}
	if !found {
		return
	}
	if claimed.MockOutcome == nil {
		worker.logger.Error("claimed payment has no mock outcome", "payment_id", claimed.ID)
		return
	}

	resolutionContext, cancel := context.WithTimeout(ctx, worker.leaseDuration)
	defer cancel()
	resolvedStatus, err := worker.orders.ResolvePayment(resolutionContext, claimed.OrderID, *claimed.MockOutcome)
	if err != nil {
		worker.logger.Warn("payment settlement will retry", "payment_id", claimed.ID, "order_id", claimed.OrderID, "error", err)
		return
	}
	if resolvedStatus != ExpectedOrderStatus(*claimed.MockOutcome) {
		worker.logger.Error("payment settlement returned conflicting order status", "payment_id", claimed.ID, "order_id", claimed.OrderID, "status", resolvedStatus)
		return
	}
	if err := worker.repository.MarkResolved(ctx, claimed.ID, *claimed.MockOutcome); err != nil {
		worker.logger.Error("payment settlement persistence failed", "payment_id", claimed.ID, "error", err)
	}
}
