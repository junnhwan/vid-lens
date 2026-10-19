ALTER TABLE storage_settings
    ADD COLUMN host_min_free_bytes bigint NOT NULL DEFAULT 1073741824
        CHECK (host_min_free_bytes BETWEEN 536870912 AND 21474836480),
    ADD COLUMN source_download_limit_bytes bigint NOT NULL DEFAULT 201326592
        CHECK (source_download_limit_bytes BETWEEN 67108864 AND 2147483648);

ALTER TABLE storage_settings
    DROP CONSTRAINT IF EXISTS storage_settings_disk_stop_percent_check,
    ADD CONSTRAINT storage_settings_disk_stop_percent_check
        CHECK (disk_stop_percent BETWEEN 80 AND 98),
    ALTER COLUMN disk_stop_percent SET DEFAULT 98;

UPDATE storage_settings SET disk_stop_percent=98 WHERE singleton;
