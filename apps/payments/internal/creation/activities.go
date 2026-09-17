package creation

import (
	"context"
	"errors"

	"go.temporal.io/sdk/temporal"

	"polyglot-ticketing-v2/apps/payments/internal/payment"
)

type Activities struct {
	Orders     payment.OrdersClient
	Repository payment.Repository
}

func (activities *Activities) StartOrderPayment(ctx context.Context, input payment.CreateInput) (payment.Order, error) {
	started, err := activities.Orders.StartPayment(ctx, input)
	if errors.Is(err, payment.ErrOrderNotFound) {
		return payment.Order{}, temporal.NewNonRetryableApplicationError("Order not found", "OrderNotFound", err)
	}
	if errors.Is(err, payment.ErrOrderNotPayable) {
		return payment.Order{}, temporal.NewNonRetryableApplicationError("Order cannot be paid", "OrderNotPayable", err)
	}
	return started, err
}

func (activities *Activities) PersistPayment(ctx context.Context, input payment.CreateInput, order payment.Order) (payment.CreateResult, error) {
	created, wasCreated, err := activities.Repository.Create(ctx, input, order)
	return payment.CreateResult{Payment: created, Created: wasCreated}, err
}
