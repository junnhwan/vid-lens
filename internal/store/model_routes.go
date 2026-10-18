package store

import (
	"context"
	"sort"

	"vidlens/internal/config"
	"vidlens/internal/domain"
)

// ModelRoute is private execution metadata. The public catalog only exposes a
// representative model; each physical submission retains its actual route.
type ModelRoute struct {
	RoutePriority      int            `json:"route_priority"`
	ConfigID           string         `json:"config_id"`
	ModelID            string         `json:"model_id"`
	RevisionID         string         `json:"model_revision_id"`
	ProviderID         string         `json:"provider_id"`
	ProviderRevisionID string         `json:"provider_revision_id"`
	BrandID            string         `json:"brand_id"`
	Profile            config.Profile `json:"profile"`
	Eligible           bool           `json:"eligible"`
}

func publicModels(models []CatalogModel, preferred *string) ([]domain.GenerationModel, *string) {
	groups := map[string]CatalogModel{}
	recommended := map[string]bool{}
	preferredFamily := ""
	for _, m := range models {
		if preferred != nil && m.ID == *preferred {
			preferredFamily = domain.ModelFamily(m.BrandID, m.UpstreamID)
			break
		}
	}
	for _, m := range models {
		if !m.Enabled || !m.ProviderEnabled || !m.Verified {
			continue
		}
		key := domain.ModelFamily(m.BrandID, m.UpstreamID)
		if m.Recommended {
			recommended[key] = true
		}
		prior, exists := groups[key]
		if !exists || preferred != nil && m.ID == *preferred || (preferred == nil || prior.ID != *preferred) && m.ID < prior.ID {
			groups[key] = m
		}
	}
	out := make([]domain.GenerationModel, 0, len(groups))
	var def *string
	for family, m := range groups {
		out = append(out, domain.GenerationModel{ID: m.ID, RevisionID: m.RevisionID, DisplayName: m.DisplayName, BrandID: m.BrandID, Description: m.Description, Recommended: recommended[family]})
		if family == preferredFamily {
			def = domain.Ptr(m.ID)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].DisplayName == out[j].DisplayName {
			return out[i].ID < out[j].ID
		}
		return out[i].DisplayName < out[j].DisplayName
	})
	return out, def
}

// The chosen model and execution budgets stay frozen. Routing may use current,
// enabled, verified suppliers of that exact model. Retired revisions/keys are
// excluded, including when a provider rotates its credential during a run.
func modelRoutes(ctx context.Context, q Querier, cid string) ([]ModelRoute, error) {
	origin, err := one[ModelRoute](ctx, q, `SELECT jsonb_build_object('route_priority',COALESCE(m.route_priority,0),'config_id',c.id,'profile',c.profile,'model_id',r.model_id,'model_revision_id',r.id,'provider_id',m.provider_id,'provider_revision_id',r.provider_revision_id,'brand_id',r.brand_id,'eligible',COALESCE(m.enabled AND p.enabled AND m.current_revision_id=r.id AND p.current_revision_id=r.provider_revision_id AND EXISTS(SELECT 1 FROM model_probes b WHERE b.revision_id=r.id AND b.mode='capabilities' AND b.status='succeeded'),false)) FROM runtime_config_revisions c LEFT JOIN model_revisions r ON r.config_id=c.id LEFT JOIN catalog_models m ON m.id=r.model_id LEFT JOIN model_providers p ON p.id=m.provider_id WHERE c.id=$1`, cid)
	if err != nil {
		return nil, err
	}
	if origin.ModelID == "" {
		// Compatibility profiles have no verified catalog identity. Preserve the
		// legacy binding gate rather than guessing equivalence from a name.
		if err = checkLegacyAvailable(ctx, q, cid); err != nil {
			return nil, err
		}
		return []ModelRoute{origin}, nil
	}
	routes, err := many[ModelRoute](ctx, q, `SELECT jsonb_build_object('route_priority',m.route_priority,'config_id',r.config_id,'profile',c.profile,'model_id',m.id,'model_revision_id',r.id,'provider_id',m.provider_id,'provider_revision_id',r.provider_revision_id,'brand_id',r.brand_id,'eligible',true) FROM catalog_models m JOIN model_revisions r ON r.id=m.current_revision_id JOIN runtime_config_revisions c ON c.id=r.config_id JOIN model_providers p ON p.id=m.provider_id WHERE m.enabled AND p.enabled AND p.current_revision_id=r.provider_revision_id AND EXISTS(SELECT 1 FROM model_probes b WHERE b.revision_id=r.id AND b.mode='capabilities' AND b.status='succeeded') ORDER BY m.id`)
	if err != nil {
		return nil, err
	}
	family := domain.ModelFamily(origin.BrandID, origin.Profile.ModelName)
	out := []ModelRoute{}
	seen := map[string]bool{}
	if origin.Eligible {
		out = append(out, origin)
		seen[origin.ProviderID] = true
	}
	for _, route := range routes {
		if !seen[route.ProviderID] && domain.ModelFamily(route.BrandID, route.Profile.ModelName) == family {
			out = append(out, route)
			seen[route.ProviderID] = true
		}
	}
	if len(out) == 0 {
		return nil, domain.Err(503, "MODEL_UNAVAILABLE", "本型号暂时没有可用线路；文章仍可阅读和导出。")
	}
	return out, nil
}

func (s *Store) ModelRoutes(ctx context.Context, cid string) ([]ModelRoute, error) {
	return modelRoutes(ctx, s.Pool, cid)
}

// Attempts remain durable across lease recovery. Missing route metadata belongs
// to the original frozen profile, as in records created before failover existed.
func (s *Store) ModelRouteAttempts(ctx context.Context, run, key, original string) (map[string]int, error) {
	rows, err := s.Pool.Query(ctx, `SELECT COALESCE(reporting->>'route_config_id',$3),count(*) FROM model_submissions WHERE run_id=$1 AND operation_key=$2 GROUP BY 1`, run, key, original)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var cid string
		var n int
		if err = rows.Scan(&cid, &n); err != nil {
			return nil, err
		}
		out[cid] = n
	}
	return out, rows.Err()
}

func (s *Store) SuccessfulModelRoute(ctx context.Context, run string) (string, error) {
	var cid string
	err := s.Pool.QueryRow(ctx, `SELECT COALESCE((SELECT s.reporting->>'route_config_id' FROM model_submissions s JOIN run_operations o USING(run_id,operation_key) WHERE s.run_id=$1 AND o.status='completed' AND s.submission=o.submissions ORDER BY s.created_at DESC LIMIT 1),'')`, run).Scan(&cid)
	return cid, err
}
