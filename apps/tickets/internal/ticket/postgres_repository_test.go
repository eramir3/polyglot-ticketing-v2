package ticket

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPostgresCreationRetry(t *testing.T) {
	url := os.Getenv("TICKETS_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("requires isolated TICKETS_TEST_DATABASE_URL")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	repository := NewPostgresRepository(pool)
	input := CreateInput{ID: uuid.NewString(), Title: "Initial", Price: 100, UserID: "retry-test"}
	defer pool.Exec(ctx, "DELETE FROM tickets WHERE id = $1", input.ID)
	first, err := repository.Create(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	_, err = repository.Update(ctx, input.ID, UpdateInput{Title: "Edited", Price: 200, UserID: input.UserID})
	if err != nil {
		t.Fatal(err)
	}
	repeated, err := repository.Create(ctx, input)
	if err != nil || repeated != first {
		t.Fatalf("retry changed result: %+v, %v", repeated, err)
	}
	current, err := repository.FindByID(ctx, input.ID)
	if err != nil || current.Title != "Edited" || current.Price != 200 {
		t.Fatalf("retry overwrote edit: %+v, %v", current, err)
	}
}

func TestPostgresTicketUpdateRetry(t *testing.T) {
	url := os.Getenv("TICKETS_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("requires isolated TICKETS_TEST_DATABASE_URL")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	repository := NewPostgresRepository(pool)
	created, err := repository.Create(ctx, CreateInput{ID: uuid.NewString(), Title: "Initial", Price: 100, UserID: "retry-test"})
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Exec(ctx, "DELETE FROM ticket_update_operations WHERE ticket_id = $1", created.ID)
	defer pool.Exec(ctx, "DELETE FROM tickets WHERE id = $1", created.ID)

	first, err := repository.UpdateWithIdempotency(ctx, created.ID, UpdateInput{
		IdempotencyKey: "update-retry",
		Title:          "First update",
		Price:          200,
		UserID:         created.UserID,
	})
	if err != nil {
		t.Fatal(err)
	}
	repeated, err := repository.UpdateWithIdempotency(ctx, created.ID, UpdateInput{
		IdempotencyKey: "update-retry",
		Title:          "Different retry body",
		Price:          300,
		UserID:         created.UserID,
	})
	if err != nil || repeated != first {
		t.Fatalf("retry changed result: %+v, %v", repeated, err)
	}
	current, err := repository.FindByID(ctx, created.ID)
	if err != nil || current != first || current.AggregateVersion != 2 {
		t.Fatalf("unexpected current ticket: %+v, %v", current, err)
	}
}

func TestPostgresReservationForbidsEveryTicketUpdateAttempt(t *testing.T) {
	url := os.Getenv("TICKETS_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("requires isolated TICKETS_TEST_DATABASE_URL")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	repository := NewPostgresRepository(pool)
	created, err := repository.Create(ctx, CreateInput{ID: uuid.NewString(), Title: "Initial", Price: 100, UserID: "reservation-test"})
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Exec(ctx, "DELETE FROM ticket_update_operations WHERE ticket_id = $1", created.ID)
	defer pool.Exec(ctx, "DELETE FROM tickets WHERE id = $1", created.ID)

	first, err := repository.UpdateWithIdempotency(ctx, created.ID, UpdateInput{
		IdempotencyKey: "before-reservation", Title: "First update", Price: 200, UserID: created.UserID,
	})
	if err != nil {
		t.Fatal(err)
	}
	orderID := uuid.NewString()
	if err := repository.ReserveForOrder(ctx, created.ID, orderID); err != nil {
		t.Fatal(err)
	}
	if err := repository.ReserveForOrder(ctx, created.ID, orderID); err != nil {
		t.Fatalf("same order claim must be idempotent: %v", err)
	}
	if err := repository.ReserveForOrder(ctx, created.ID, uuid.NewString()); !errors.Is(err, ErrReserved) {
		t.Fatalf("expected conflicting claim to be reserved, got %v", err)
	}

	_, err = repository.UpdateWithIdempotency(ctx, created.ID, UpdateInput{
		IdempotencyKey: "before-reservation", Title: "Changed retry body", Price: 300, UserID: created.UserID,
	})
	if !errors.Is(err, ErrReserved) {
		t.Fatalf("expected a reservation to forbid an idempotent retry, got %v", err)
	}
	current, err := repository.FindByID(ctx, created.ID)
	if err != nil || current.Title != first.Title || current.Price != first.Price || current.AggregateVersion != first.AggregateVersion {
		t.Fatalf("reservation changed ticket: %+v, %v", current, err)
	}
	if err := repository.ReleaseOrderReservation(ctx, created.ID, uuid.NewString()); err != nil {
		t.Fatalf("stale release must be harmless: %v", err)
	}
	if err := repository.ReleaseOrderReservation(ctx, created.ID, orderID); err != nil {
		t.Fatal(err)
	}
	retried, err := repository.UpdateWithIdempotency(ctx, created.ID, UpdateInput{
		IdempotencyKey: "before-reservation", Title: "Changed retry body", Price: 300, UserID: created.UserID,
	})
	if err != nil || retried != first {
		t.Fatalf("expected the released retry to replay the original update: %+v, %v", retried, err)
	}
}
