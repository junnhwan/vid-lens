ALTER TABLE qa_turns ADD COLUMN allow_external_sources boolean NOT NULL DEFAULT false;
ALTER TABLE qa_turns ADD COLUMN reference_urls jsonb NOT NULL DEFAULT '[]';
CREATE TABLE external_references(id text PRIMARY KEY,run_id text NOT NULL REFERENCES job_runs,turn_id text NOT NULL REFERENCES qa_turns,session_id text NOT NULL,request_id text NOT NULL REFERENCES generation_requests,url text NOT NULL,title text NOT NULL DEFAULT '',status text NOT NULL DEFAULT 'registered' CHECK(status IN ('registered','read','failed')),body text,content_sha256 text,read_at timestamptz,expires_at timestamptz NOT NULL DEFAULT now()+interval '24 hours',truncated boolean NOT NULL DEFAULT false,UNIQUE(run_id,url));
CREATE TABLE external_search_months(month text PRIMARY KEY,exhausted boolean NOT NULL DEFAULT false);
-- Credit receipts outlive operation-payload cleanup; deleting old responses
-- must never replenish this month's conservative free allowance.
CREATE TABLE external_search_credits(run_id text NOT NULL REFERENCES job_runs,operation_key text NOT NULL,month text NOT NULL REFERENCES external_search_months,created_at timestamptz NOT NULL DEFAULT now(),PRIMARY KEY(run_id,operation_key));
