ALTER TABLE provider_revisions ADD COLUMN proxy_policy text NOT NULL DEFAULT 'server' CHECK(proxy_policy IN ('server','direct'));
CREATE TABLE legacy_model_bindings(config_id text PRIMARY KEY REFERENCES runtime_config_revisions,model_id text NOT NULL REFERENCES catalog_models);
