package grpc

import (
	"context"
	"encoding/json"
	"testing"

	"buf.build/go/protovalidate"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"polyglot-ticketing-v2/apps/orders/internal/order"
	ordersv1 "polyglot-ticketing-v2/protogen/go/orders/v1"
)

func TestValidationInterceptorRejectsInvalidRequestsBeforeHandler(t *testing.T) {
	validator, err := protovalidate.New()
	require.NoError(t, err)
	interceptor := ValidationInterceptor(validator)

	testCases := []struct {
		name  string
		input any
		field string
	}{
		{
			name:  "ticket ID",
			input: &ordersv1.CreateOrderRequest{TicketId: "not-a-uuid", UserId: "user-1"},
			field: "ticketId",
		},
		{
			name:  "order ID",
			input: &ordersv1.GetOrderRequest{OrderId: "not-a-uuid", UserId: "user-1"},
			field: "orderId",
		},
		{
			name:  "blank user ID",
			input: &ordersv1.ListOrdersRequest{UserId: " \t"},
			field: "userId",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			called := false
			response, err := interceptor(
				context.Background(),
				testCase.input,
				&grpc.UnaryServerInfo{},
				func(context.Context, any) (any, error) {
					called = true
					return "handled", nil
				},
			)

			require.Nil(t, response)
			require.False(t, called)
			require.Equal(t, codes.InvalidArgument, status.Code(err))
			var payload struct {
				Errors []order.ValidationError `json:"errors"`
			}
			require.NoError(t, json.Unmarshal([]byte(status.Convert(err).Message()), &payload))
			require.Equal(t, []order.ValidationError{{
				Code:    "INVALID_ARGUMENT",
				Field:   testCase.field,
				Message: validationMessage(testCase.field),
			}}, payload.Errors)
		})
	}
}

func TestValidationInterceptorRunsHandlerForValidRequest(t *testing.T) {
	validator, err := protovalidate.New()
	require.NoError(t, err)

	response, err := ValidationInterceptor(validator)(
		context.Background(),
		&ordersv1.CreateOrderRequest{TicketId: ticketID, UserId: userID},
		&grpc.UnaryServerInfo{},
		func(context.Context, any) (any, error) { return "handled", nil },
	)
	require.NoError(t, err)
	require.Equal(t, "handled", response)
}

func validationMessage(field string) string {
	switch field {
	case "ticketId":
		return "Ticket ID must be a valid UUID."
	case "orderId":
		return "Order ID must be a valid UUID."
	default:
		return "Order user is required."
	}
}
