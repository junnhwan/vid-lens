CREATE TABLE article_questions (
 article_id text PRIMARY KEY REFERENCES articles ON DELETE CASCADE,
 config_id text NOT NULL REFERENCES runtime_config_revisions,
 policy text NOT NULL,
 status text NOT NULL DEFAULT 'queued' CHECK(status IN ('queued','running','completed','incomplete')),
 epoch bigint NOT NULL DEFAULT 0,
 submissions integer NOT NULL DEFAULT 0 CHECK(submissions BETWEEN 0 AND 1),
 input_hash text,
 result jsonb,
 reporting jsonb,
 failure_code text,
 created_at timestamptz NOT NULL DEFAULT now(),
 started_at timestamptz,
 deadline_at timestamptz NOT NULL DEFAULT now()+interval '20 minutes',
 updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX article_questions_pending ON article_questions(created_at) WHERE status='queued';
