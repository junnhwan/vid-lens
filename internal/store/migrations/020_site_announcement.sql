CREATE TABLE site_announcement (
    singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
    revision bigint NOT NULL CHECK (revision > 0),
    enabled boolean NOT NULL DEFAULT false,
    title text NOT NULL DEFAULT '' CHECK (char_length(title) <= 120),
    body text NOT NULL DEFAULT '' CHECK (char_length(body) <= 8000),
    version text NOT NULL DEFAULT '',
    updated_at timestamptz NOT NULL DEFAULT now(),
    CHECK (NOT enabled OR (btrim(title) <> '' AND btrim(body) <> '' AND version <> ''))
);
