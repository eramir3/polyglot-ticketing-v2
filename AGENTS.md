<!-- nx configuration start-->
<!-- Leave the start & end comments to automatically receive updates. -->

# General Guidelines for working with Nx

- For navigating/exploring the workspace, invoke the `nx-workspace` skill first - it has patterns for querying projects, targets, and dependencies
- When running tasks (for example build, lint, test, e2e, etc.), always prefer running the task through `nx` (i.e. `nx run`, `nx run-many`, `nx affected`) instead of using the underlying tooling directly
- Prefix nx commands with the workspace's package manager (e.g., `pnpm nx build`, `npm exec nx test`) - avoids using globally installed CLI
- You have access to the Nx MCP server and its tools, use them to help the user
- For Nx plugin best practices, check `node_modules/@nx/<plugin>/PLUGIN.md`. Not all plugins have this file - proceed without it if unavailable.
- NEVER guess CLI flags - always check nx_docs or `--help` first when unsure

## Scaffolding & Generators

- For scaffolding tasks (creating apps, libs, project structure, setup), ALWAYS invoke the `nx-generate` skill FIRST before exploring or calling MCP tools

## When to use nx_docs

- USE for: advanced config options, unfamiliar flags, migration guides, plugin configuration, edge cases
- DON'T USE for: basic generator syntax (`nx g @nx/react:app`), standard commands, things you already know
- The `nx-generate` skill handles generator discovery internally - don't call nx_docs just to look up generator syntax

<!-- nx configuration end-->

# Project Guide

## Architecture

- This is an Nx monorepo. TypeScript/NestJS services are `api-gateway` and
  `identity`; Go services are `tickets` and `orders`.
- The API gateway is the only REST entry point (`/api/*`). Internal service
  communication uses gRPC contracts in `proto/`.
- `proto/` is the source of truth. Generate bindings with `pnpm proto:generate`
  (or `make generate-proto`); do not hand-edit `protogen/go` or `protogen/ts`.
- Each service owns its PostgreSQL database and migrations. Do not query or
  write another service's database directly.
- Ticket creation and updates are Temporal workflows. Orders also uses Temporal
  for its ticket-reservation lifecycle. Tickets uses aggregate versions to
  order writes to the Orders ticket projection through Orders gRPC.
  There are no events or outbox patterns in the current design.

## Working Conventions

- Use `pnpm` (pinned in `package.json`) for JavaScript dependencies and Nx
  commands. Use `pnpm nx ...`, not a global Nx binary.
- Run workspace tasks through Make targets when available: `make test`,
  `make test-tickets`, `make test-orders`, and `make test-api-gateway`.
- Run `make docker-up` for the full local stack. It regenerates protobuf
  bindings and starts PostgreSQL, Mailpit, Temporal, migrations, and services.
  `make docker-reset` deliberately destroys Compose volumes.
- Copy `.env.example` to `.env` for local Compose credentials. Never commit
  secrets or `.env` files.
- Add forward-only SQL migrations under the owning service's `migrations/`
  directory. Do not edit a migration that may already have run.
- Preserve a dirty worktree: treat unrelated changes as user work. Do not use
  destructive Git commands unless explicitly requested.

## Ticket Creation Rules

- The backend generates ticket UUIDs. The frontend optionally supplies an
  `Idempotency-Key` to identify one logical POST retry.
- Keys are scoped to the authenticated user. A repeated key returns the first
  workflow result even if the retry body differs; a request without a key is a
  new creation.
- `PUT /api/tickets/:id` requires an `Idempotency-Key`. Updates are serialized
  by ticket in Temporal, increment the Tickets aggregate version, and apply the
  same next version to the Orders projection.
- Keep Temporal workflow code deterministic. I/O belongs in activities, and
  activities must remain safe to retry by the stable ticket UUID.
- The Tickets process hosts both the gRPC server and Temporal worker. Keep
  Temporal components organized under `apps/tickets/internal/creation/`:
  `coordinator.go`, `workflow.go`, `activities.go`, and `worker.go`.

## Ticket Reservation Rules

- Orders claims a Tickets source row through internal Tickets gRPC; Orders must
  never write the Tickets database directly.
- A `Created` order holds `tickets.reserved_by_order_id`. All ticket update
  attempts, including idempotency-key replays, return `FORBIDDEN` while held.
- Orders' Temporal expiry workflow waits 15 minutes, changes a still-`Created`
  order to `Canceled`, then releases its matching claim. Explicit cancellation
  follows the same durable order-then-release sequence.
- `Complete` retains its claim. `AwaitingPayment` policy belongs to the future
  Payments service and must be coordinated with the reservation workflow.

## Verification

- Changes to `.proto` files require regenerated Go and TypeScript bindings and
  affected service builds/tests.
- Gateway integration tests use Docker/Testcontainers. Orders projection tests
  also require Docker.
- Run `git diff --check` before handing off changes.
