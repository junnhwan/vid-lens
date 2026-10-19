ALTER TABLE storage_settings
    ADD COLUMN disk_stop_percent integer NOT NULL DEFAULT 90
    CHECK (disk_stop_percent BETWEEN 80 AND 95);
