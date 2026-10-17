CREATE TABLE storage_settings (
 singleton boolean PRIMARY KEY DEFAULT true CHECK(singleton),
 revision bigint NOT NULL DEFAULT 1,
 video_quota_bytes bigint NOT NULL DEFAULT 10737418240 CHECK(video_quota_bytes BETWEEN 1073741824 AND 10995116277760),
 image_quota_bytes bigint NOT NULL DEFAULT 5368709120 CHECK(image_quota_bytes BETWEEN 1073741824 AND 10995116277760)
);
INSERT INTO storage_settings(singleton) VALUES(true);
-- Retained originals are removed only by an explicit owner action. Keep the
-- existing article, frame, temporary workspace and export retention rules.
UPDATE media_assets SET expires_at=NULL WHERE kind IN ('video','audio') AND state='ready';
CREATE INDEX sources_requests ON generation_requests(source_id,created_at DESC);
CREATE INDEX sources_assets ON media_assets(source_id,kind,state);
