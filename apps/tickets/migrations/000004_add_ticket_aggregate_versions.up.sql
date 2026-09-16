ALTER TABLE tickets
  ADD COLUMN aggregate_version BIGINT NOT NULL DEFAULT 1
    CHECK (aggregate_version >= 1);

CREATE TABLE ticket_update_operations (
  ticket_id UUID NOT NULL REFERENCES tickets (id),
  user_id TEXT NOT NULL,
  idempotency_key TEXT NOT NULL,
  aggregate_version BIGINT NOT NULL CHECK (aggregate_version >= 1),
  title TEXT NOT NULL,
  price BIGINT NOT NULL CHECK (price > 0),
  PRIMARY KEY (ticket_id, user_id, idempotency_key)
);
