package creation

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"time"

	"go.temporal.io/api/serviceerror"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/temporal"

	"polyglot-ticketing-v2/apps/payments/internal/payment"
)

const DefaultTaskQueue = "payment-processing"

// Coordinator starts durable payment-creation operations. Its request timeout
// limits the caller's wait; a workflow accepted by Temporal keeps retrying.
type Coordinator struct {
	Client    client.Client
	TaskQueue string
}

func (coordinator *Coordinator) CreatePayment(ctx context.Context, input payment.CreateInput) (payment.CreateResult, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	workflowID := createPaymentWorkflowID(input)
	run, err := coordinator.Client.ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID:        workflowID,
		TaskQueue: coordinator.TaskQueue,
	}, CreatePaymentWorkflow, input)
	var alreadyStarted *serviceerror.WorkflowExecutionAlreadyStarted
	if errors.As(err, &alreadyStarted) {
		run = coordinator.Client.GetWorkflow(ctx, workflowID, alreadyStarted.RunId)
	} else if err != nil {
		return payment.CreateResult{}, payment.ErrUnavailable
	}

	var result payment.CreateResult
	if err := run.Get(ctx, &result); err != nil {
		if ctx.Err() != nil {
			return payment.CreateResult{}, payment.ErrUnavailable
		}
		return payment.CreateResult{}, createPaymentWorkflowError(err)
	}
	return result, nil
}

func createPaymentWorkflowID(input payment.CreateInput) string {
	userHash := sha256.Sum256([]byte(input.UserID))
	return fmt.Sprintf("create-payment/%s/%x", input.OrderID, userHash)
}

func createPaymentWorkflowError(err error) error {
	var applicationFailure *temporal.ApplicationError
	if errors.As(err, &applicationFailure) {
		switch applicationFailure.Type() {
		case "OrderNotFound":
			return payment.ErrOrderNotFound
		case "OrderNotPayable":
			return payment.ErrOrderNotPayable
		}
	}
	return fmt.Errorf("%w: %v", payment.ErrUnavailable, err)
}

var _ payment.CreateCoordinator = (*Coordinator)(nil)
