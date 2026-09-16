package main

import (
	"context"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.temporal.io/sdk/client"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"polyglot-ticketing-v2/apps/tickets/internal/creation"
	grpcserver "polyglot-ticketing-v2/apps/tickets/internal/grpc"
	"polyglot-ticketing-v2/apps/tickets/internal/ticket"
	ordersv1 "polyglot-ticketing-v2/protogen/go/orders/v1"
	ticketsv1 "polyglot-ticketing-v2/protogen/go/tickets/v1"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := pgxpool.New(ctx, requiredEnvironmentVariable("DATABASE_URL"))
	if err != nil {
		slog.Error("failed to create tickets database pool", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	repository := ticket.NewPostgresRepository(pool)
	temporalClient, err := client.Dial(client.Options{
		HostPort:  environmentVariable("TEMPORAL_ADDRESS", "localhost:7233"),
		Namespace: environmentVariable("TEMPORAL_NAMESPACE", "default"),
	})
	if err != nil {
		slog.Error("failed to connect to Temporal", "error", err)
		os.Exit(1)
	}
	defer temporalClient.Close()
	ordersConnection, err := grpc.NewClient(environmentVariable("ORDERS_GRPC_URL", "localhost:50053"), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		slog.Error("failed to create Orders client", "error", err)
		os.Exit(1)
	}
	defer ordersConnection.Close()
	taskQueue := environmentVariable("TEMPORAL_TASK_QUEUE", creation.DefaultTaskQueue)
	temporalWorker := creation.NewWorker(
		temporalClient,
		taskQueue,
		repository,
		ordersv1.NewOrdersServiceClient(ordersConnection),
	)
	if err := temporalWorker.Start(); err != nil {
		slog.Error("failed to start Temporal worker", "error", err)
		os.Exit(1)
	}
	defer temporalWorker.Stop()
	coordinator := &creation.Coordinator{Client: temporalClient, TaskQueue: taskQueue}

	listener, err := net.Listen("tcp", ":"+environmentVariable("GRPC_PORT", "50052"))
	if err != nil {
		slog.Error("failed to listen for gRPC", "error", err)
		os.Exit(1)
	}

	server := grpc.NewServer()
	ticketsv1.RegisterTicketsServiceServer(
		server,
		grpcserver.NewServer(ticket.NewService(repository, coordinator), slog.Default()),
	)

	go func() {
		slog.Info("tickets gRPC service started", "address", listener.Addr().String())
		if err := server.Serve(listener); err != nil {
			slog.Error("tickets gRPC service stopped unexpectedly", "error", err)
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
