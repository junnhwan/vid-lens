-- Raise the current production intake limits once, while preserving the
-- profile revisions referenced by already accepted requests.
UPDATE storage_settings
SET source_download_limit_bytes = 524288000,
    revision = revision + 1
WHERE singleton;

WITH next_profile AS (
    SELECT md5(random()::text || clock_timestamp()::text || s.config_id) AS id,
           jsonb_set(c.profile, '{max_video_ms}', '7200000'::jsonb, true) AS profile
    FROM runtime_settings s
    JOIN runtime_config_revisions c ON c.id = s.config_id
), inserted AS (
    INSERT INTO runtime_config_revisions(id, profile)
    SELECT id, profile FROM next_profile
    RETURNING id
)
UPDATE runtime_settings s
SET config_id = inserted.id
FROM inserted
WHERE s.singleton;
