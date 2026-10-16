ALTER TABLE generation_requests ADD COLUMN http_trace_id text;
ALTER TABLE qa_turns ADD COLUMN http_trace_id text;
-- Unknown historical operation times stay NULL.
ALTER TABLE run_operations ADD COLUMN started_at timestamptz;
ALTER TABLE run_operations ADD COLUMN completed_at timestamptz;
ALTER TABLE run_operations ALTER COLUMN started_at SET DEFAULT now();
