ALTER TABLE payments
  ADD COLUMN mock_outcome payment_status,
  ADD COLUMN settlement_lease_expires_at TIMESTAMPTZ;

ALTER TABLE payments
  ADD CONSTRAINT payments_mock_outcome_terminal
  CHECK (mock_outcome IS NULL OR mock_outcome IN ('Succeeded', 'Failed'));

CREATE INDEX payments_pending_settlement_idx
  ON payments (id)
  WHERE status = 'Pending';
