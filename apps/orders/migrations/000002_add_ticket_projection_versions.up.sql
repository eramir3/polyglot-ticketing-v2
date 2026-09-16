ALTER TABLE tickets
  ADD COLUMN aggregate_version BIGINT NOT NULL DEFAULT 1
    CHECK (aggregate_version >= 1);
