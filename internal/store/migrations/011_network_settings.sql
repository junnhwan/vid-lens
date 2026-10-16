CREATE TABLE network_settings (
    singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
    revision bigint NOT NULL CHECK (revision > 0),
    reference_read_enabled boolean NOT NULL,
    search_enabled boolean NOT NULL,
    daily_limit integer NOT NULL CHECK (daily_limit BETWEEN 0 AND 1000),
    monthly_limit integer NOT NULL CHECK (monthly_limit BETWEEN 0 AND 1000),
    key_ref text NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX external_search_credits_created_at_idx ON external_search_credits(created_at);
