-- Receipts deliberately have no session FK: totals survive session expiry.
CREATE TABLE completed_article_receipts(session_id text NOT NULL,article_id text NOT NULL,created_at timestamptz NOT NULL DEFAULT now(),PRIMARY KEY(session_id,article_id));
CREATE TABLE site_likes(session_id text PRIMARY KEY,created_at timestamptz NOT NULL DEFAULT now());
CREATE TABLE site_metric_totals(singleton boolean PRIMARY KEY DEFAULT true CHECK(singleton),completed_readings bigint NOT NULL DEFAULT 0,likes bigint NOT NULL DEFAULT 0,updated_at timestamptz NOT NULL DEFAULT now());
INSERT INTO completed_article_receipts(session_id,article_id) SELECT DISTINCT session_id,article_id FROM generation_requests WHERE status='completed' AND article_id IS NOT NULL ON CONFLICT DO NOTHING;
INSERT INTO site_metric_totals(completed_readings) SELECT count(*) FROM completed_article_receipts;
