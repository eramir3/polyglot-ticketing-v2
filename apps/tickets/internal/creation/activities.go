package creation

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"go.temporal.io/sdk/temporal"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"polyglot-ticketing-v2/apps/tickets/internal/ticket"
	ordersv1 "polyglot-ticketing-v2/protogen/go/orders/v1"
)

// Activities performs the I/O steps of the ticket creation and update workflows.
type Activities struct {
	Repository ticket.Repository
	Orders     ordersv1.OrdersServiceClient
}

func (activities *Activities) PersistTicket(ctx context.Context, input Input) (ticket.Ticket, error) {
	if _, err := uuid.Parse(input.ID); err != nil || strings.TrimSpace(input.Title) == "" || input.Price <= 0 || input.Price > ticket.MaxPrice || input.UserID == "" {
		return ticket.Ticket{}, temporal.NewNonRetryableApplicationError("Invalid ticket creation input", "InvalidTicket", nil)
	}
	created, err := activities.Repository.Create(ctx, ticket.CreateInput{ID: input.ID, Title: input.Title, Price: input.Price, UserID: input.UserID})
	var databaseError *pgconn.PgError
	if errors.Is(err, ticket.ErrConflict) || (errors.As(err, &databaseError) && (databaseError.Code == "23514" || databaseError.Code == "23505" || databaseError.Code == "22P02")) {
		return ticket.Ticket{}, temporal.NewNonRetryableApplicationError("Invalid or conflicting ticket", "InvalidTicket", err)
	}
	return created, err
}

func (activities *Activities) PersistTicketUpdate(ctx context.Context, input UpdateTicketInput) (ticket.Ticket, error) {
	if _, err := uuid.Parse(input.ID); err != nil || strings.TrimSpace(input.Title) == "" || input.Price <= 0 || input.Price > ticket.MaxPrice || input.UserID == "" || input.IdempotencyKey == "" {
		return ticket.Ticket{}, temporal.NewNonRetryableApplicationError("Invalid ticket update input", "InvalidTicketUpdate", nil)
	}
	updated, err := activities.Repository.UpdateWithIdempotency(ctx, input.ID, ticket.UpdateInput{
		IdempotencyKey: input.IdempotencyKey,
		Title:          input.Title,
		Price:          input.Price,
		UserID:         input.UserID,
	})
	if errors.Is(err, ticket.ErrForbidden) {
		return ticket.Ticket{}, temporal.NewNonRetryableApplicationError("Ticket update forbidden", "TicketUpdateForbidden", err)
	}
	if errors.Is(err, ticket.ErrNotFound) {
		return ticket.Ticket{}, temporal.NewNonRetryableApplicationError("Ticket not found", "TicketUpdateNotFound", err)
	}
	var databaseError *pgconn.PgError
	if errors.As(err, &databaseError) && (databaseError.Code == "23514" || databaseError.Code == "23505" || databaseError.Code == "22P02") {
		return ticket.Ticket{}, temporal.NewNonRetryableApplicationError("Invalid ticket update", "InvalidTicketUpdate", err)
	}
	return updated, err
}

func (activities *Activities) ProjectTicketToOrders(ctx context.Context, created ticket.Ticket) error {
	_, err := activities.Orders.EnsureTicketProjection(ctx, &ordersv1.EnsureTicketProjectionRequest{
		Id: created.ID, Title: created.Title, Price: created.Price, AggregateVersion: created.AggregateVersion,
	})
	switch status.Code(err) {
	case codes.InvalidArgument, codes.AlreadyExists, codes.PermissionDenied, codes.Unauthenticated, codes.Unimplemented:
		return temporal.NewNonRetryableApplicationError("Orders rejected ticket projection", "InvalidProjection", err)
	}
	return err
}
