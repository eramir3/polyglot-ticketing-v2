package creation

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/temporal"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"polyglot-ticketing-v2/apps/tickets/internal/ticket"
	ordersv1 "polyglot-ticketing-v2/protogen/go/orders/v1"
)

func TestProjectionActivityClassifiesErrors(t *testing.T) {
	for _, code := range []codes.Code{codes.InvalidArgument, codes.AlreadyExists, codes.Unavailable, codes.DeadlineExceeded} {
		t.Run(code.String(), func(t *testing.T) {
			activities := Activities{Orders: failingOrdersClient{err: status.Error(code, "failure")}}
			err := activities.ProjectTicketToOrders(context.Background(), ticket.Ticket{})
			var applicationError *temporal.ApplicationError
			permanent := errors.As(err, &applicationError) && applicationError.NonRetryable()
			require.Equal(t, code == codes.InvalidArgument || code == codes.AlreadyExists, permanent)
		})
	}
}

func TestPersistActivityRejectsMissingStableID(t *testing.T) {
	activities := Activities{}
	_, err := activities.PersistTicket(context.Background(), Input{Title: "Concert", Price: 100, UserID: "owner"})
	var applicationError *temporal.ApplicationError
	require.ErrorAs(t, err, &applicationError)
	require.True(t, applicationError.NonRetryable())
}

type failingOrdersClient struct {
	ordersv1.OrdersServiceClient
	err error
}

func (c failingOrdersClient) EnsureTicketProjection(context.Context, *ordersv1.EnsureTicketProjectionRequest, ...grpc.CallOption) (*ordersv1.EnsureTicketProjectionResponse, error) {
	return nil, c.err
}
