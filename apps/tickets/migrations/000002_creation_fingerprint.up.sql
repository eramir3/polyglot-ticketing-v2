-- Immutable creation data lets activity retries distinguish an ID collision
-- from a ticket whose mutable title/price changed after creation.
ALTER TABLE tickets ADD COLUMN creation_fingerprint TEXT;
