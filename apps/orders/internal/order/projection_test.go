package order

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPostgresProjectionRetry(t *testing.T) {
	url := os.Getenv("ORDERS_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("requires isolated ORDERS_TEST_DATABASE_URL")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	repository := NewPostgresRepository(pool)
	input := Ticket{ID: uuid.NewString(), Title: "Initial", Price: 100, AggregateVersion: 1}
	defer pool.Exec(ctx, "DELETE FROM tickets WHERE id = $1", input.ID)
	for range 2 {
		if err := repository.EnsureTicketProjection(ctx, input); err != nil {
			t.Fatal(err)
		}
	}
	updated := Ticket{ID: input.ID, Title: "Edited", Price: 200, AggregateVersion: 2}
	if err := repository.EnsureTicketProjection(ctx, updated); err != nil {
		t.Fatal(err)
	}
	if err := repository.EnsureTicketProjection(ctx, updated); err != nil {
		t.Fatalf("expected idempotent retry, got %v", err)
	}
	if err := repository.EnsureTicketProjection(ctx, Ticket{ID: input.ID, Title: "Future", Price: 300, AggregateVersion: 4}); !errors.Is(err, ErrProjectionOutOfOrder) {
		t.Fatalf("expected out-of-order error, got %v", err)
	}
	if err := repository.EnsureTicketProjection(ctx, input); !errors.Is(err, ErrProjectionConflict) {
		t.Fatalf("expected conflict, got %v", err)
	}
	var price int64
	if err := pool.QueryRow(ctx, "SELECT price FROM tickets WHERE id = $1", input.ID).Scan(&price); err != nil || price != 200 {
		t.Fatalf("versioned retry overwrote edit: %d, %v", price, err)
	}
}
