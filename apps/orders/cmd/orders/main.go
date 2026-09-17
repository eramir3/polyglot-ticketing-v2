package main

import (
	"context"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"

	"buf.build/go/protovalidate"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.temporal.io/sdk/client"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	grpcserver "polyglot-ticketing-v2/apps/orders/internal/grpc"
	"polyglot-ticketing-v2/apps/orders/internal/order"
	"polyglot-ticketing-v2/apps/orders/internal/reservation"
	ordersv1 "polyglot-ticketing-v2/protogen/go/orders/v1"
	ticketsv1 "polyglot-ticketing-v2/protogen/go/tickets/v1"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	pool, err := pgxpool.New(ctx, requiredEnvironmentVariable("DATABASE_URL"))
	if err != nil {
		slog.Error("failed to create orders database pool", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	repository := order.NewPostgresRepository(pool)
	temporalClient, err := client.Dial(client.Options{
		HostPort:  environmentVariable("TEMPORAL_ADDRESS", "localhost:7233"),
		Namespace: environmentVariable("TEMPORAL_NAMESPACE", "default"),
	})
	if err != nil {
		slog.Error("failed to connect to Temporal", "error", err)
		os.Exit(1)
	}
	defer temporalClient.Close()
	ticketsConnection, err := grpc.NewClient(
		environmentVariable("TICKETS_GRPC_URL", "localhost:50052"),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		slog.Error("failed to create Tickets client", "error", err)
		os.Exit(1)
	}
	defer ticketsConnection.Close()
	taskQueue := environmentVariable("ORDERS_TEMPORAL_TASK_QUEUE", reservation.DefaultTaskQueue)
	temporalWorker := reservation.NewWorker(
		temporalClient,
		taskQueue,
		repository,
		ticketsv1.NewTicketsServiceClient(ticketsConnection),
	)
	if err := temporalWorker.Start(); err != nil {
		slog.Error("failed to start Temporal worker", "error", err)
		os.Exit(1)
	}
	defer temporalWorker.Stop()
	coordinator := &reservation.Coordinator{Client: temporalClient, TaskQueue: taskQueue}
	validator, err := protovalidate.New()
	if err != nil {
		slog.Error("failed to create request validator", "error", err)
		os.Exit(1)
	}

	listener, err := net.Listen("tcp", ":"+environmentVariable("GRPC_PORT", "50053"))
	if err != nil {
		slog.Error("failed to listen for gRPC", "error", err)
		os.Exit(1)
	}

	server := grpc.NewServer(
		grpc.UnaryInterceptor(grpcserver.ValidationInterceptor(validator)),
	)
	ordersv1.RegisterOrdersServiceServer(
		server,
		grpcserver.NewServer(order.NewService(repository, coordinator), slog.Default(), repository),
	)

	go func() {
		slog.Info("orders gRPC service started", "address", listener.Addr().String())
		if err := server.Serve(listener); err != nil {
			slog.Error("orders gRPC service stopped unexpectedly", "error", err)
		}
	}()

	<-ctx.Done()
	server.GracefulStop()
}

func environmentVariable(name string, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}

	return fallback
}

func requiredEnvironmentVariable(name string) string {
	value := os.Getenv(name)
	if value == "" {
		slog.Error("required environment variable is missing", "name", name)
		os.Exit(1)
	}

	return value
}
