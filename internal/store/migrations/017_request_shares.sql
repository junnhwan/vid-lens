CREATE TABLE request_shares(
  request_id text PRIMARY KEY REFERENCES generation_requests(id) ON DELETE CASCADE,
  id text NOT NULL UNIQUE,
  created_at timestamptz NOT NULL DEFAULT now()
);
