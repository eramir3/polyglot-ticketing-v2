# Ticket creation with Temporal

`POST /api/tickets` calls Tickets over gRPC. Tickets starts `CreateTicketWorkflow`
before writing ticket data. Its Go worker, hosted in the Tickets process, runs
`PersistTicket` and then `ProjectTicketToOrders`. The latter calls Orders'
internal `EnsureTicketProjection` RPC. Each service owns its database writes.

A `201` response retains the existing ticket JSON and means both records exist;
the ticket can immediately be used with `POST /api/orders`. Only creation is
synchronized. Ticket edits and backfill of older tickets are not implemented.

## Request retries

Send an optional `Idempotency-Key` header (1–128 printable ASCII characters).
Reuse the same key when retrying an uncertain request. Keys are scoped to the
authenticated user. Identical concurrent requests join one workflow, and later
requests with the same key replay the original result even if their body differs.
Missing keys preserve the previous behavior: each HTTP request creates a separate
ticket.

The server waits up to 30 seconds for the workflow. `503` can mean the workflow
is still running, so retry with the same key. Closing the connection or timing
out does not cancel accepted work. Temporal not accepting a workflow causes no
ticket write. Source and projection writes are repeatable by ticket UUID.

Key deduplication lasts while Temporal retains the workflow history, configured
here as seven days after completion. Always choose a new key for a new intended
creation. Do not retry an old key after retention expires. Repeated successful
requests return the original creation result, even if the ticket was later edited.

Activities retry transient failures indefinitely with 1-second initial backoff,
doubling up to 30 seconds, and a 10-second execution timeout per attempt. Invalid
or conflicting records stop the workflow. A ticket already written is retained;
projection resumes after a transient Orders outage. This is eventual completion,
not a transaction spanning both databases. Pending tickets may appear in reads
before projection completes.

## Local operation

`make docker-up` regenerates protobuf bindings and starts the Temporal development server with persistent SQLite
storage, configures seven-day namespace retention, and starts the Go worker.
Temporal listens on port 7233; its built-in UI is at http://localhost:8233.
This development server is for local use, not a production deployment.

Local Go processes use `TEMPORAL_ADDRESS`, `TEMPORAL_NAMESPACE`,
`TEMPORAL_TASK_QUEUE`, and `ORDERS_GRPC_URL` from their environment; see
`.env.example`. Compose supplies internal addresses. Normal container restarts
preserve workflow history. `make docker-reset` deletes Temporal history along
with application database volumes.

Run `node scripts/test-temporal-compose.mjs` after protobuf generation to verify
fresh-volume startup with isolated containers and volumes. It builds temporary
Tickets/Orders images and cleans up its stack automatically. The gateway's Nx
`integration` target also tests the real Temporal flow, including recovery after
an Orders outage and worker restart, using isolated Testcontainers databases.
