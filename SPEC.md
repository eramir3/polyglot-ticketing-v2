# Polyglot Ticketing Specification

## Purpose

This repository is a ticketing-system monorepo. It uses a REST API gateway at
the cluster boundary and gRPC between internal services. NestJS is used for the
gateway and identity service; Go is used for Tickets and Orders.

## Services and ownership

| Service | Runtime | Responsibility | Data ownership |
| --- | --- | --- | --- |
| API Gateway | NestJS | Public REST API, authentication boundary, and gRPC adaptation | None |
| Identity | NestJS | Sign-up, sign-in, sessions, and email verification | Identity PostgreSQL database |
| Tickets | Go | Ticket CRUD and durable ticket creation | Tickets PostgreSQL database |
| Orders | Go | Ticket projection and order reservation lifecycle | Orders PostgreSQL database |
| Temporal | Temporal development server | Durable orchestration of ticket creation | Persistent local SQLite volume |

Each service writes only its own database. Internal contracts live in `proto/`;
`protogen/go` and `protogen/ts` are generated artifacts.

## API and contracts

The gateway exposes `/api`-prefixed REST routes. Current resource groups are:

- `/api/auth`: sign-up, sign-in, sign-out, current user, and email
  verification.
- `/api/tickets`: list, get, create, and update tickets.
- `/api/orders`: create, list, get, and cancel orders.

The gateway calls Identity, Tickets, and Orders over gRPC. The protobuf service
definitions are `IdentityService`, `TicketsService`, and `OrdersService`.
Orders also exposes the internal `EnsureTicketProjection` RPC, used only by the
Tickets creation activity.

## Ticket creation workflow

`POST /api/tickets` validates the request and forwards the authenticated user
ID to Tickets. Tickets uses `TicketCreationCoordinator` to start or join
`CreateTicketWorkflow` in Temporal.

```text
Gateway REST request
  -> Tickets gRPC CreateTicket
  -> Temporal CreateTicketWorkflow
     -> PersistTicket activity (Tickets database)
     -> ProjectTicketToOrders activity (Orders EnsureTicketProjection gRPC)
  -> original ticket response
```

The workflow is complete only after the ticket exists in both the Tickets source
table and Orders projection table. Activities retry transient failures. The
workflow and worker run in the Tickets process; Temporal history persists across
normal local container restarts.

### Idempotency

- The Tickets backend creates the ticket UUID.
- A client may supply `Idempotency-Key` (1–128 printable ASCII characters) on
  `POST /api/tickets`.
- The key is scoped to the authenticated user and maps to a Temporal workflow.
  Retries with that user/key return the original result, including if the retry
  body changes.
- Requests without a key are independent creations.
- The local Temporal namespace retains completed workflow history for seven
  days. Use a new key for a new intended ticket creation.
- A request waits up to 30 seconds; `503` means the durable operation may still
  be running and should be retried with the same key.

This design intentionally does not use events, an outbox pattern, or aggregate
versioning. It also intentionally has no observability implementation yet.

## Persistence

- Identity, Tickets, and Orders each run PostgreSQL 17 locally.
- Tickets owns its `tickets` source table, including title, price, user ID, and
  ticket ID.
- Orders owns a ticket projection table and its orders table. Orders may create
  an order only for a projected ticket.
- SQL migrations are forward-only and live under the owning service's
  `migrations/` directory. Historical Tickets migrations `000002` and `000003`
  add then remove the former fingerprint column; the active design has no
  fingerprint.

## Local development

1. Copy `.env.example` to `.env` and replace placeholder credentials.
2. Install dependencies with `pnpm install --frozen-lockfile`.
3. Run `make docker-up` to generate protobuf bindings, build images, migrate
   databases, and start the local stack.

Local ports:

| Component | Local address |
| --- | --- |
| API Gateway | http://localhost:3000 |
| Identity gRPC | identity:50051 (Compose-internal) |
| Tickets gRPC | tickets:50052 (Compose-internal) |
| Orders gRPC | orders:50053 (Compose-internal) |
| Temporal | localhost:7233 (gRPC) |
| Temporal UI | http://localhost:8233 |
| Mailpit UI | http://localhost:8025 |
| Identity PostgreSQL | postgresql://localhost:5432 |
| Tickets PostgreSQL | postgresql://localhost:5433 |
| Orders PostgreSQL | postgresql://localhost:5434 |

Useful commands:

- `make generate-proto`: regenerate Go and TypeScript protobuf bindings.
- `make build`: build all services.
- `make test`: run executable test suites.
- `make docker-logs`: follow Compose service logs.
- `make docker-reset`: recreate the stack and delete all local service and
  Temporal data.

## Testing expectations

- Use `pnpm nx` or the corresponding Make target for workspace tasks.
- Gateway integration tests use Docker/Testcontainers and exercise the real
  Temporal Tickets-to-Orders path.
- Go unit and integration tests cover Tickets workflow/activity behavior and
  Orders projection behavior.
- After changing protobuf contracts, regenerate bindings before building or
  testing dependent services.
