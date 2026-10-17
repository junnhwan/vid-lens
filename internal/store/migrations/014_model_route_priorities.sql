ALTER TABLE catalog_models
  ADD COLUMN route_priority integer NOT NULL DEFAULT 0
  CHECK (route_priority BETWEEN 0 AND 1000);
