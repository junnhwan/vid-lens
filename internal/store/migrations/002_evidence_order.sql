ALTER TABLE observations ADD COLUMN created_at timestamptz NOT NULL DEFAULT clock_timestamp();
ALTER TABLE job_runs ADD COLUMN dependency_seq bigint NOT NULL DEFAULT 0;
