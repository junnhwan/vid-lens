CREATE TABLE article_reviews (
 id text PRIMARY KEY,
 article_id text NOT NULL REFERENCES articles,
 config_id text NOT NULL REFERENCES runtime_config_revisions,
 trigger text NOT NULL CHECK (trigger IN ('automatic','manual')),
 session_id text,
 status text NOT NULL DEFAULT 'queued' CHECK (status IN ('queued','running','completed','incomplete')),
 epoch bigint NOT NULL DEFAULT 0,
 submissions integer NOT NULL DEFAULT 0 CHECK (submissions BETWEEN 0 AND 1),
 input_hash text,
 result jsonb,
 reporting jsonb,
 failure_code text,
 created_at timestamptz NOT NULL DEFAULT now(),
 started_at timestamptz,
 deadline_at timestamptz NOT NULL DEFAULT now()+interval '30 minutes',
 updated_at timestamptz NOT NULL DEFAULT now(),
 CHECK ((trigger='automatic' AND session_id IS NULL) OR (trigger='manual' AND session_id IS NOT NULL))
);
CREATE UNIQUE INDEX article_review_once ON article_reviews(article_id) WHERE trigger='automatic';
CREATE INDEX article_reviews_pending ON article_reviews(status,created_at);
CREATE INDEX article_reviews_reader ON article_reviews(article_id,session_id,created_at DESC);
