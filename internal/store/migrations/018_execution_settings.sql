CREATE TABLE execution_settings (
    singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
    revision bigint NOT NULL CHECK (revision > 0),
    max_video_running integer NOT NULL CHECK (max_video_running BETWEEN 1 AND 64),
    updated_at timestamptz NOT NULL DEFAULT now()
);
