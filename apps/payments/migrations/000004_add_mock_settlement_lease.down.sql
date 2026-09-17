DROP INDEX payments_pending_settlement_idx;

ALTER TABLE payments
  DROP CONSTRAINT payments_mock_outcome_terminal,
  DROP COLUMN settlement_lease_expires_at,
  DROP COLUMN mock_outcome;
