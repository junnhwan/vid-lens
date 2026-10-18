package store

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"vidlens/internal/config"
	"vidlens/internal/domain"
)

// Import creates a disabled catalog revision from the current server profile.
// No secret is returned, no probe is fabricated, and the live default stays put.
func (s *Store) ImportCurrentModel(ctx context.Context) (CatalogModel, error) {
	id := ""
	e := s.Tx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var cid string
		if e := tx.QueryRow(ctx, "SELECT config_id FROM runtime_settings").Scan(&cid); e != nil {
			return e
		}
		e := tx.QueryRow(ctx, "SELECT model_id FROM legacy_model_bindings WHERE config_id=$1", cid).Scan(&id)
		if e == nil {
			return nil
		}
		if !errors.Is(e, pgx.ErrNoRows) {
			return e
		}
		p, e := one[config.Profile](ctx, tx, "SELECT profile FROM runtime_config_revisions WHERE id=$1", cid)
		if e != nil {
			return e
		}
		if e = p.Validate(); e != nil {
			return domain.Err(503, "SERVICE_UNAVAILABLE", "当前没有可导入的模型配置。")
		}
		provider, pr, rev, newCID := domain.ID(), domain.ID(), domain.ID(), domain.ID()
		id = "mdl_" + domain.ID()
		if _, e = tx.Exec(ctx, "INSERT INTO model_providers(id,current_revision_id,enabled) VALUES($1,$2,true)", provider, pr); e != nil {
			return e
		}
		if _, e = tx.Exec(ctx, "INSERT INTO provider_revisions(id,provider_id,name,base_url,key_ref,proxy_policy) VALUES($1,$2,'现有供应商',$3,$4,$5)", pr, provider, p.ModelBaseURL, p.ModelKeyRef, func() string {
			if p.ModelProxyPolicy == "direct" {
				return "direct"
			}
			return "server"
		}()); e != nil {
			return e
		}
		p.CacheHintsVerified = false
		if _, e = tx.Exec(ctx, "INSERT INTO runtime_config_revisions(id,profile) VALUES($1,$2)", newCID, domain.JSON(p)); e != nil {
			return e
		}
		if _, e = tx.Exec(ctx, "INSERT INTO catalog_models(id,provider_id,current_revision_id,enabled) VALUES($1,$2,$3,false)", id, provider, rev); e != nil {
			return e
		}
		if _, e = tx.Exec(ctx, "INSERT INTO model_revisions(id,model_id,provider_revision_id,config_id,display_name,brand_id,description) VALUES($1,$2,$3,$4,$5,'generic','由现有配置导入，需完成能力检查。')", rev, id, pr, newCID, p.ModelName); e != nil {
			return e
		}
		if _, e = tx.Exec(ctx, "INSERT INTO legacy_model_bindings(config_id,model_id) VALUES($1,$2)", cid, id); e != nil {
			return e
		}
		_, e = tx.Exec(ctx, "UPDATE model_settings SET revision=revision+1")
		return e
	})
	if e != nil {
		return CatalogModel{}, e
	}
	return s.Model(ctx, id)
}
