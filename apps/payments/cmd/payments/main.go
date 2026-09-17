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

	"polyglot-ticketing-v2/apps/payments/internal/creation"
	grpcserver "polyglot-ticketing-v2/apps/payments/internal/grpc"
	"polyglot-ticketing-v2/apps/payments/internal/payment"
	ordersv1 "polyglot-ticketing-v2/protogen/go/orders/v1"
	paymentsv1 "polyglot-ticketing-v2/protogen/go/payments/v1"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := pgxpool.New(ctx, requiredEnvironmentVariable("DATABASE_URL"))
	if err != nil {
		slog.Error("failed to create payments database pool", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	repository := payment.NewPostgresRepository(pool)
	ordersConnection, err := grpc.NewClient(
		environmentVariable("ORDERS_GRPC_URL", "localhost:50053"),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		slog.Error("failed to create Orders client", "error", err)
		os.Exit(1)
	}
	defer ordersConnection.Close()
	temporalClient, err := client.Dial(client.Options{
		HostPort:  environmentVariable("TEMPORAL_ADDRESS", "localhost:7233"),
		Namespace: environmentVariable("TEMPORAL_NAMESPACE", "default"),
	})
	if err != nil {
		slog.Error("failed to connect to Temporal", "error", err)
		os.Exit(1)
	}
	defer temporalClient.Close()
	processorOutcome, err := payment.ParseProcessorOutcome(environmentVariable("PAYMENT_PROCESSOR_OUTCOME", "success"))
	if err != nil {
		slog.Error("invalid payment processor outcome", "error", err)
		os.Exit(1)
	}
	ordersClient := payment.NewGRPCOrdersClient(ordersv1.NewOrdersServiceClient(ordersConnection))
	taskQueue := environmentVariable("PAYMENTS_TEMPORAL_TASK_QUEUE", creation.DefaultTaskQueue)
	temporalWorker := creation.NewWorker(temporalClient, taskQueue, repository, ordersClient)
	if err := temporalWorker.Start(); err != nil {
		slog.Error("failed to start Payments Temporal worker", "error", err)
		os.Exit(1)
	}
	defer temporalWorker.Stop()
	settlementWorker := payment.NewSettlementWorker(repository, ordersClient, processorOutcome, slog.Default())
	go settlementWorker.Run(ctx)

	listener, err := net.Listen("tcp", ":"+environmentVariable("GRPC_PORT", "50054"))
	if err != nil {
		slog.Error("failed to listen for gRPC", "error", err)
		os.Exit(1)
	}

	server := grpc.NewServer()
	paymentsv1.RegisterPaymentsServiceServer(
		server,
		grpcserver.NewServer(payment.NewService(repository, &creation.Coordinator{Client: temporalClient, TaskQueue: taskQueue}), slog.Default()),
	)
	go func() {
		slog.Info("payments gRPC service started", "address", listener.Addr().String())
		if err := server.Serve(listener); err != nil {
			slog.Error("payments gRPC service stopped unexpectedly", "error", err)
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
