package payment

import (
	"context"
	"fmt"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	ordersv1 "polyglot-ticketing-v2/protogen/go/orders/v1"
)

type GRPCOrdersClient struct {
	client ordersv1.OrdersServiceClient
}

func NewGRPCOrdersClient(client ordersv1.OrdersServiceClient) *GRPCOrdersClient {
	return &GRPCOrdersClient{client: client}
}

func (client *GRPCOrdersClient) StartPayment(ctx context.Context, input CreateInput) (Order, error) {
	response, err := client.client.StartPayment(ctx, &ordersv1.StartPaymentRequest{
		OrderId: input.OrderID,
		UserId:  input.UserID,
	})
	if err != nil {
		return Order{}, mapOrdersError(err)
	}
	started := response.GetOrder()
	if started == nil {
		return Order{}, fmt.Errorf("Orders start payment returned no order")
	}
	return Order{
		ID:     started.GetId(),
		Price:  started.GetPrice(),
		Status: toOrderStatus(started.GetStatus()),
		UserID: started.GetUserId(),
	}, nil
}

func (client *GRPCOrdersClient) ResolvePayment(ctx context.Context, orderID string, outcome Status) (OrderStatus, error) {
	response, err := client.client.ResolvePayment(ctx, &ordersv1.ResolvePaymentRequest{
		OrderId: orderID,
		Outcome: toPaymentOutcome(outcome),
	})
	if err != nil {
		return "", mapOrdersError(err)
	}
	if response.GetOrder() == nil {
		return "", fmt.Errorf("Orders resolve payment returned no order")
	}
	return toOrderStatus(response.GetOrder().GetStatus()), nil
}

func mapOrdersError(err error) error {
	switch status.Code(err) {
	case codes.NotFound:
		return ErrOrderNotFound
	case codes.AlreadyExists:
		return ErrOrderNotPayable
	default:
		return fmt.Errorf("Orders RPC failed: %w", err)
	}
}

func toOrderStatus(value ordersv1.OrderStatus) OrderStatus {
	switch value {
	case ordersv1.OrderStatus_ORDER_STATUS_AWAITING_PAYMENT:
		return OrderStatusAwaitingPayment
	case ordersv1.OrderStatus_ORDER_STATUS_CANCELED:
		return OrderStatusCanceled
	case ordersv1.OrderStatus_ORDER_STATUS_COMPLETE:
		return OrderStatusComplete
	default:
		return ""
	}
}

func toPaymentOutcome(value Status) ordersv1.PaymentOutcome {
	switch value {
	case StatusSucceeded:
		return ordersv1.PaymentOutcome_PAYMENT_OUTCOME_SUCCEEDED
	case StatusFailed:
		return ordersv1.PaymentOutcome_PAYMENT_OUTCOME_FAILED
	default:
		return ordersv1.PaymentOutcome_PAYMENT_OUTCOME_UNSPECIFIED
	}
}

var _ OrdersClient = (*GRPCOrdersClient)(nil)
