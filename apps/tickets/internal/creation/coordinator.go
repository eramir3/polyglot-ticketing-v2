package creation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	enums "go.temporal.io/api/enums/v1"
	"go.temporal.io/api/serviceerror"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/temporal"

	"polyglot-ticketing-v2/apps/tickets/internal/ticket"
)

type Coordinator struct {
	Client    client.Client
	TaskQueue string
}

func operationKey(value []string) string {
	encoded, _ := json.Marshal(value)
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}

func (coordinator *Coordinator) Create(ctx context.Context, input ticket.CreateInput) (ticket.Ticket, error) {
	// This deadline bounds only the request's wait. It does not cancel an
	// accepted workflow or its activity retries.
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	workflowID := "create-ticket/" + uuid.NewString()
	if input.IdempotencyKey != nil {
		workflowID = "create-ticket/" + operationKey([]string{input.UserID, *input.IdempotencyKey})
	}
	run, err := coordinator.Client.ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID: workflowID, TaskQueue: coordinator.TaskQueue,
		WorkflowIDReusePolicy:                    enums.WORKFLOW_ID_REUSE_POLICY_REJECT_DUPLICATE,
		WorkflowIDConflictPolicy:                 enums.WORKFLOW_ID_CONFLICT_POLICY_FAIL,
		WorkflowExecutionErrorWhenAlreadyStarted: true,
	}, CreateTicketWorkflow, Input{ID: uuid.NewString(), Title: input.Title, Price: input.Price, UserID: input.UserID})
	var alreadyStarted *serviceerror.WorkflowExecutionAlreadyStarted
	if errors.As(err, &alreadyStarted) {
		run = coordinator.Client.GetWorkflow(ctx, workflowID, alreadyStarted.RunId)
	} else if err != nil {
		return ticket.Ticket{}, fmt.Errorf("%w: %v", ticket.ErrUnavailable, err)
	}
	var result ticket.Ticket
	err = run.Get(ctx, &result)
	if err != nil {
		if ctx.Err() != nil {
			return ticket.Ticket{}, ticket.ErrUnavailable
		}
		var unavailable *serviceerror.Unavailable
		if errors.As(err, &unavailable) {
			return ticket.Ticket{}, ticket.ErrUnavailable
		}
		// A workflow that reaches an infrastructure failure can complete with a
		// WorkflowExecutionError rather than a transport-level Unavailable. Keep
		// that distinction out of the HTTP API: callers should retry the same
		// idempotency key while Temporal/Orders recovers. Permanent validation
		// failures remain internal errors.
		var workflowFailure *temporal.WorkflowExecutionError
		if errors.As(err, &workflowFailure) {
			var applicationFailure *temporal.ApplicationError
			if errors.As(err, &applicationFailure) {
				switch applicationFailure.Type() {
				case "InvalidTicket", "InvalidProjection":
					return ticket.Ticket{}, err
				}
			}
			return ticket.Ticket{}, ticket.ErrUnavailable
		}
		return ticket.Ticket{}, err
	}
	return result, nil
}
