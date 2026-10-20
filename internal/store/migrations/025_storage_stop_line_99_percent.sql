ALTER TABLE storage_settings
    DROP CONSTRAINT IF EXISTS storage_settings_disk_stop_percent_check,
    ADD CONSTRAINT storage_settings_disk_stop_percent_check
        CHECK (disk_stop_percent BETWEEN 80 AND 99);
