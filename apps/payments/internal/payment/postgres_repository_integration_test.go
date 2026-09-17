package payment

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPostgresRepositoryCreatesOnePaymentAndStoresOrderSnapshot(t *testing.T) {
	ctx := context.Background()
	pool := startPaymentsPostgres(t, ctx)
	repository := NewPostgresRepository(pool)
	input := CreateInput{OrderID: uuid.NewString(), UserID: "user-1"}
	order := Order{ID: input.OrderID, UserID: input.UserID, Price: 10_000, Status: OrderStatusAwaitingPayment}

	created, wasCreated, err := repository.Create(ctx, input, order)
	if err != nil || !wasCreated || created.ID == "" || created.Status != StatusPending {
		t.Fatalf("create payment: payment=%+v created=%t err=%v", created, wasCreated, err)
	}
	replayed, wasCreated, err := repository.Create(ctx, input, order)
	if err != nil || wasCreated || replayed != created {
		t.Fatalf("replay payment: payment=%+v created=%t err=%v", replayed, wasCreated, err)
	}

	var userID string
	var price int64
	var status string
	if err := pool.QueryRow(ctx, `SELECT user_id, price, status::text FROM orders WHERE id = $1`, input.OrderID).Scan(&userID, &price, &status); err != nil || userID != input.UserID || price != order.Price || OrderStatus(status) != OrderStatusAwaitingPayment {
		t.Fatalf("unexpected order snapshot: user=%q price=%d status=%q err=%v", userID, price, status, err)
	}
}

func TestPostgresRepositoryPersistsMockOutcomeBeforeResolution(t *testing.T) {
	ctx := context.Background()
	pool := startPaymentsPostgres(t, ctx)
	repository := NewPostgresRepository(pool)
	input := CreateInput{OrderID: uuid.NewString(), UserID: "user-1"}
	if _, _, err := repository.Create(ctx, input, Order{ID: input.OrderID, UserID: input.UserID, Price: 10_000, Status: OrderStatusAwaitingPayment}); err != nil {
		t.Fatal(err)
	}

	claimed, found, err := repository.ClaimNextPending(ctx, StatusFailed, time.Now().UTC().Add(time.Minute))
	if err != nil || !found || claimed.MockOutcome == nil || *claimed.MockOutcome != StatusFailed {
		t.Fatalf("claim payment: payment=%+v found=%t err=%v", claimed, found, err)
	}
	if _, found, err := repository.ClaimNextPending(ctx, StatusSucceeded, time.Now().UTC().Add(time.Minute)); err != nil || found {
		t.Fatalf("expected active lease to prevent duplicate claim, found=%t err=%v", found, err)
	}
	if err := repository.MarkResolved(ctx, claimed.ID, StatusFailed); err != nil {
		t.Fatal(err)
	}
	var status string
	if err := pool.QueryRow(ctx, `SELECT status::text FROM payments WHERE id = $1`, claimed.ID).Scan(&status); err != nil || Status(status) != StatusFailed {
		t.Fatalf("expected failed payment, got status=%q err=%v", status, err)
	}
	if err := pool.QueryRow(ctx, `SELECT status::text FROM orders WHERE id = $1`, input.OrderID).Scan(&status); err != nil || OrderStatus(status) != OrderStatusCanceled {
		t.Fatalf("expected canceled order projection, got status=%q err=%v", status, err)
	}
}

func startPaymentsPostgres(t *testing.T, ctx context.Context) *pgxpool.Pool {
	t.Helper()
	databaseURL := os.Getenv("PAYMENTS_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("requires isolated PAYMENTS_TEST_DATABASE_URL")
	}
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("create payments database pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}
