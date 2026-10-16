CREATE TABLE model_submissions (
 run_id text NOT NULL REFERENCES job_runs,
 operation_key text NOT NULL,
 submission integer NOT NULL,
 attempt_id text NOT NULL REFERENCES job_attempts ON DELETE CASCADE,
 reporting jsonb NOT NULL DEFAULT '{"schema":"unobserved","stream_status":"incomplete"}',
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(run_id,operation_key,submission),
 FOREIGN KEY(run_id,operation_key) REFERENCES run_operations(run_id,operation_key) ON DELETE CASCADE
);
CREATE INDEX model_submission_attempt ON model_submissions(attempt_id);
