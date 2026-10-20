ALTER TABLE storage_settings
    DROP CONSTRAINT IF EXISTS storage_settings_host_min_free_bytes_check;

UPDATE storage_settings
SET host_min_free_bytes = GREATEST(host_min_free_bytes, 3221225472),
    disk_stop_percent = 99,
    revision = revision + 1
WHERE singleton;

ALTER TABLE storage_settings
    ADD CONSTRAINT storage_settings_host_min_free_bytes_check
        CHECK (host_min_free_bytes BETWEEN 3221225472 AND 21474836480),
    ALTER COLUMN host_min_free_bytes SET DEFAULT 3221225472;
