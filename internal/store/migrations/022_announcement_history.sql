-- Append-only announcement history. The previous singleton row becomes the first entry.
CREATE TABLE site_announcement_entries (
    id text PRIMARY KEY CHECK (id ~ '^[a-z0-9-]{8,80}$'),
    title text NOT NULL DEFAULT '' CHECK (char_length(title) <= 120),
    body text NOT NULL DEFAULT '' CHECK (char_length(body) <= 8000),
    status text NOT NULL CHECK (status IN ('current', 'published', 'withdrawn', 'draft')),
    version text NOT NULL DEFAULT '',
    published_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    CHECK (
        (status IN ('current', 'published') AND btrim(title) <> '' AND btrim(body) <> '' AND version <> '' AND published_at IS NOT NULL)
        OR (status = 'draft' AND version = '' AND published_at IS NULL)
        OR status = 'withdrawn'
    )
);
CREATE UNIQUE INDEX site_announcement_one_current ON site_announcement_entries (status) WHERE status = 'current';
CREATE UNIQUE INDEX site_announcement_one_draft ON site_announcement_entries (status) WHERE status = 'draft';

CREATE TABLE site_announcement_state (
    singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
    revision bigint NOT NULL CHECK (revision > 0)
);

INSERT INTO site_announcement_state(singleton, revision)
SELECT true, revision FROM site_announcement WHERE singleton AND revision > 0;

INSERT INTO site_announcement_entries(id, title, body, status, version, published_at, created_at)
SELECT
    CASE WHEN version <> '' THEN version ELSE 'legacy-draft' END,
    title,
    body,
    CASE
        WHEN enabled AND version <> '' THEN 'current'
        WHEN version <> '' THEN 'withdrawn'
        ELSE 'draft'
    END,
    version,
    CASE WHEN version <> '' THEN updated_at ELSE NULL END,
    updated_at
FROM site_announcement
WHERE singleton AND (btrim(title) <> '' OR btrim(body) <> '' OR version <> '');

DROP TABLE site_announcement;
