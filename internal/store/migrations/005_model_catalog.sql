CREATE TABLE owner_auth(singleton boolean PRIMARY KEY DEFAULT true CHECK(singleton), password_hash text NOT NULL, auth_version bigint NOT NULL DEFAULT 1);
CREATE TABLE admin_sessions(token_hash text PRIMARY KEY, auth_version bigint NOT NULL, expires_at timestamptz NOT NULL, idle_until timestamptz NOT NULL, created_at timestamptz NOT NULL DEFAULT now());
CREATE TABLE admin_login_limits(source_hash text PRIMARY KEY, failures integer NOT NULL DEFAULT 0, window_start timestamptz NOT NULL DEFAULT now(), next_attempt_at timestamptz NOT NULL DEFAULT now());
CREATE TABLE model_providers(id text PRIMARY KEY, current_revision_id text NOT NULL, revision bigint NOT NULL DEFAULT 1, enabled boolean NOT NULL DEFAULT true);
CREATE TABLE provider_revisions(id text PRIMARY KEY, provider_id text NOT NULL REFERENCES model_providers, name text NOT NULL, base_url text NOT NULL, key_ref text NOT NULL, created_at timestamptz NOT NULL DEFAULT now());
CREATE TABLE catalog_models(id text PRIMARY KEY, provider_id text NOT NULL REFERENCES model_providers, current_revision_id text NOT NULL, revision bigint NOT NULL DEFAULT 1, enabled boolean NOT NULL DEFAULT false);
CREATE TABLE model_revisions(id text PRIMARY KEY, model_id text NOT NULL REFERENCES catalog_models, provider_revision_id text NOT NULL REFERENCES provider_revisions, config_id text NOT NULL REFERENCES runtime_config_revisions, display_name text NOT NULL, brand_id text NOT NULL, description text NOT NULL DEFAULT '', created_at timestamptz NOT NULL DEFAULT now());
CREATE TABLE model_settings(singleton boolean PRIMARY KEY DEFAULT true CHECK(singleton), default_model_id text REFERENCES catalog_models, revision bigint NOT NULL DEFAULT 1, selection_enabled boolean NOT NULL DEFAULT false);
INSERT INTO model_settings(singleton) VALUES(true);
CREATE TABLE model_probes(id text PRIMARY KEY, model_id text NOT NULL REFERENCES catalog_models, revision_id text NOT NULL REFERENCES model_revisions, mode text NOT NULL CHECK(mode IN ('capabilities','cache')), status text NOT NULL DEFAULT 'queued' CHECK(status IN ('queued','running','succeeded','failed','unknown')), epoch bigint NOT NULL DEFAULT 0, submissions integer NOT NULL DEFAULT 0, result jsonb, failure_code text, created_at timestamptz NOT NULL DEFAULT now(), deadline_at timestamptz NOT NULL DEFAULT now()+interval '180 seconds', updated_at timestamptz NOT NULL DEFAULT now());
CREATE TABLE probe_operations(probe_id text NOT NULL REFERENCES model_probes, submission integer NOT NULL, epoch bigint NOT NULL, reporting jsonb NOT NULL DEFAULT '{}', completed boolean NOT NULL DEFAULT false, PRIMARY KEY(probe_id,submission));
ALTER TABLE resource_leases ALTER COLUMN run_id DROP NOT NULL;
ALTER TABLE resource_leases ADD COLUMN probe_id text REFERENCES model_probes;
ALTER TABLE resource_leases ADD CONSTRAINT lease_one_owner CHECK((run_id IS NOT NULL)::integer+(probe_id IS NOT NULL)::integer=1);
ALTER TABLE generation_requests ADD COLUMN generation_spec jsonb;
ALTER TABLE articles ADD COLUMN generation_model jsonb;
-- Only actual archived config identities prove historical provenance. The
-- article content and its hash are deliberately left unchanged.
UPDATE generation_requests r SET config_id=a.config_id FROM articles a WHERE a.id=r.article_id AND r.config_id<>a.config_id;
UPDATE generation_requests r SET generation_spec=jsonb_build_object('version',1,'purpose','','scope','public','config_id',r.config_id,'model',jsonb_build_object('id','legacy','revision_id',r.config_id,'display_name',COALESCE(c.profile->>'model_name','历史模型'),'brand_id','generic','selection_locked',true,'legacy',true)) FROM runtime_config_revisions c WHERE c.id=r.config_id;
UPDATE articles a SET generation_model=jsonb_build_object('id','legacy','revision_id',a.config_id,'display_name',COALESCE(c.profile->>'model_name','历史模型'),'brand_id','generic','selection_locked',true,'legacy',true) FROM runtime_config_revisions c WHERE c.id=a.config_id;
