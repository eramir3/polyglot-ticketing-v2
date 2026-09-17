// Package creation owns the durable payment-creation process.
package creation

import (
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"polyglot-ticketing-v2/apps/payments/internal/payment"
)

func activityContext(ctx workflow.Context) workflow.Context {
	return workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 10 * time.Second,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    time.Second,
			BackoffCoefficient: 2,
			MaximumInterval:    30 * time.Second,
		},
	})
}

// CreatePaymentWorkflow makes checkout initiation durable. It does not settle
// the payment; the database-backed mock settlement worker owns that later step.
func CreatePaymentWorkflow(ctx workflow.Context, input payment.CreateInput) (payment.CreateResult, error) {
	activitiesContext := activityContext(ctx)
	var activities *Activities
	var started payment.Order
	if err := workflow.ExecuteActivity(activitiesContext, activities.StartOrderPayment, input).Get(ctx, &started); err != nil {
		return payment.CreateResult{}, err
	}
	var created payment.CreateResult
	if err := workflow.ExecuteActivity(activitiesContext, activities.PersistPayment, input, started).Get(ctx, &created); err != nil {
		return payment.CreateResult{}, err
	}
	return created, nil
}
