package store

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"vidlens/internal/config"
	"vidlens/internal/domain"
)

type Provider struct {
	ProxyPolicy   string `json:"proxy_policy"`
	ID            string `json:"id"`
	RevisionID    string `json:"revision_id"`
	Revision      int64  `json:"revision"`
	Name          string `json:"name"`
	BaseURL       string `json:"base_url"`
	KeyRef        string `json:"-"`
	KeyConfigured bool   `json:"key_configured"`
	Enabled       bool   `json:"enabled"`
}
type ProviderInput struct {
	ProxyPolicy      string `json:"proxy_policy"`
	Name             string `json:"name"`
	BaseURL          string `json:"base_url"`
	APIKey           string `json:"api_key"`
	Enabled          bool   `json:"enabled"`
	ExpectedRevision int64  `json:"expected_revision"`
}
type CatalogModel struct {
	InputCharsLimit    int    `json:"input_chars_limit"`
	ContextTokens      int    `json:"context_tokens"`
	RoutePriority      int    `json:"route_priority"`
	RouteFamily        string `json:"route_family"`
	ID                 string `json:"id"`
	RevisionID         string `json:"revision_id"`
	Revision           int64  `json:"revision"`
	ProviderID         string `json:"provider_id"`
	ProviderRevisionID string `json:"provider_revision_id"`
	ConfigID           string `json:"-"`
	DisplayName        string `json:"display_name"`
	BrandID            string `json:"brand_id"`
	Description        string `json:"description"`
	UpstreamID         string `json:"upstream_id"`
	Enabled            bool   `json:"enabled"`
	ProviderEnabled    bool   `json:"provider_enabled"`
	Verified           bool   `json:"verified"`
	IsDefault          bool   `json:"is_default"`
	Recommended        bool   `json:"recommended"`
	MaxOutputTokens    int    `json:"max_output_tokens"`
	MaxInputChars      int    `json:"max_input_chars"`
	DisableThinking    bool   `json:"disable_thinking"`
	CachePolicy        string `json:"cache_policy"`
	CacheHintsVerified bool   `json:"cache_hints_verified"`
}
type ModelInput struct {
	InputCharsLimit  *int   `json:"input_chars_limit,omitempty"`
	ContextTokens    *int   `json:"context_tokens,omitempty"`
	RoutePriority    *int   `json:"route_priority,omitempty"`
	ProviderID       string `json:"provider_id"`
	DisplayName      string `json:"display_name"`
	BrandID          string `json:"brand_id"`
	Description      string `json:"description"`
	UpstreamID       string `json:"upstream_id"`
	Enabled          bool   `json:"enabled"`
	ExpectedRevision int64  `json:"expected_revision"`
	MaxOutputTokens  int    `json:"max_output_tokens"`
	MaxInputChars    int    `json:"max_input_chars"`
	DisableThinking  bool   `json:"disable_thinking"`
	CachePolicy      string `json:"cache_policy"`
}

type ModelRouteOrderEntry struct {
	ID               string `json:"id"`
	ExpectedRevision int64  `json:"expected_revision"`
}

const providerSelect = `SELECT jsonb_build_object('id',p.id,'revision_id',r.id,'revision',p.revision,'name',r.name,'base_url',r.base_url,'proxy_policy',r.proxy_policy,'key_configured',r.key_ref<>'','enabled',p.enabled) FROM model_providers p JOIN provider_revisions r ON r.id=p.current_revision_id`
const modelSelect = `SELECT jsonb_build_object('id',m.id,'revision_id',r.id,'revision',m.revision,'route_priority',m.route_priority,'provider_id',m.provider_id,'provider_revision_id',r.provider_revision_id,'display_name',r.display_name,'brand_id',r.brand_id,'description',r.description,'upstream_id',c.profile->>'model_name','enabled',m.enabled,'provider_enabled',p.enabled,'verified',EXISTS(SELECT 1 FROM model_probes b WHERE b.revision_id=r.id AND b.mode='capabilities' AND b.status='succeeded'),'is_default',COALESCE(m.id=s.default_model_id,false),'recommended',m.recommended,'max_output_tokens',c.profile->'max_output_tokens','max_input_chars',c.profile->'max_input_chars','input_chars_limit',COALESCE((c.profile->>'input_chars_limit')::int,0),'context_tokens',COALESCE((c.profile->>'context_tokens')::int,0),'disable_thinking',COALESCE((c.profile->>'model_disable_thinking')::boolean,false),'cache_policy',COALESCE(c.profile->>'cache_policy','provider_default'),'cache_hints_verified',EXISTS(SELECT 1 FROM model_probes b WHERE b.revision_id=r.id AND b.mode='cache' AND b.status='succeeded' AND b.result->>'hints_verified'='true')) FROM catalog_models m JOIN model_revisions r ON r.id=m.current_revision_id JOIN model_providers p ON p.id=m.provider_id JOIN runtime_config_revisions c ON c.id=r.config_id CROSS JOIN model_settings s`

// Older databases may already have enabled models without a catalog default.
// Once enabled, the catalog stays active even if all its models are stopped.
const catalogEnabledSQL = `(s.selection_enabled OR EXISTS(SELECT 1 FROM catalog_models WHERE enabled))`

func (s *Store) Providers(ctx context.Context) ([]Provider, error) {
	return many[Provider](ctx, s.Pool, providerSelect+" ORDER BY r.name,p.id")
}
func (s *Store) Models(ctx context.Context) ([]CatalogModel, error) {
	items, err := many[CatalogModel](ctx, s.Pool, modelSelect+" ORDER BY r.display_name,m.id")
	if err != nil {
		return nil, err
	}
	for i := range items {
		items[i].RouteFamily = domain.ModelFamily(items[i].BrandID, items[i].UpstreamID)
	}
	return items, nil
}
func (s *Store) Provider(ctx context.Context, id string) (Provider, error) {
	v, e := one[Provider](ctx, s.Pool, providerSelect+" WHERE p.id=$1", id)
	return v, NotFound(e)
}
func (s *Store) Model(ctx context.Context, id string) (CatalogModel, error) {
	v, e := one[CatalogModel](ctx, s.Pool, modelSelect+" WHERE m.id=$1", id)
	if e == nil {
		v.RouteFamily = domain.ModelFamily(v.BrandID, v.UpstreamID)
	}
	return v, NotFound(e)
}

// ReorderModelRoutes validates and updates every current catalog route for one
// exact model family as a single change. The table lock prevents a route from
// being added or revised between membership validation and priority updates.
func (s *Store) ReorderModelRoutes(ctx context.Context, in []ModelRouteOrderEntry) ([]CatalogModel, error) {
	if len(in) < 2 || len(in) > 100 {
		return nil, domain.Err(422, "INVALID_ARGUMENT", "同型号线路至少要有两条，且一次最多调整100条。")
	}

	requested := make(map[string]ModelRouteOrderEntry, len(in))
	for _, item := range in {
		if item.ID == "" || item.ExpectedRevision < 1 {
			return nil, domain.Err(422, "INVALID_ARGUMENT", "线路顺序请求缺少有效的模型和修订号。")
		}
		if _, exists := requested[item.ID]; exists {
			return nil, domain.Err(422, "INVALID_ARGUMENT", "线路顺序中不能重复包含同一个模型。")
		}
		requested[item.ID] = item
	}

	family := ""
	err := s.Tx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "LOCK TABLE catalog_models IN SHARE ROW EXCLUSIVE MODE"); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `SELECT m.id,m.revision,r.brand_id,c.profile->>'model_name'
FROM catalog_models m
JOIN model_revisions r ON r.id=m.current_revision_id
JOIN runtime_config_revisions c ON c.id=r.config_id`)
		if err != nil {
			return err
		}
		type member struct {
			id       string
			revision int64
			family   string
		}
		members := make(map[string]member, len(in))
		familyMembers := make(map[string]member)
		for rows.Next() {
			var item member
			var brand, upstream string
			if err = rows.Scan(&item.id, &item.revision, &brand, &upstream); err != nil {
				rows.Close()
				return err
			}
			item.family = domain.ModelFamily(brand, upstream)
			if _, ok := requested[item.id]; ok {
				members[item.id] = item
			}
		}
		if err = rows.Err(); err != nil {
			rows.Close()
			return err
		}
		rows.Close()

		for _, item := range in {
			member, ok := members[item.ID]
			if !ok {
				return domain.Err(409, "REVISION_CONFLICT", "同型号线路已变化，请重新读取后再保存。")
			}
			if family == "" {
				family = member.family
			} else if member.family != family {
				return domain.Err(422, "INVALID_ARGUMENT", "只能调整完全相同型号的供应商线路。")
			}
			if item.ExpectedRevision != member.revision {
				return domain.Err(409, "REVISION_CONFLICT", "模型配置已更新，请重新读取后再保存线路顺序。")
			}
		}

		rows, err = tx.Query(ctx, `SELECT m.id,m.revision,r.brand_id,c.profile->>'model_name'
FROM catalog_models m
JOIN model_revisions r ON r.id=m.current_revision_id
JOIN runtime_config_revisions c ON c.id=r.config_id`)
		if err != nil {
			return err
		}
		count := 0
		for rows.Next() {
			var id, brand, upstream string
			var revision int64
			if err = rows.Scan(&id, &revision, &brand, &upstream); err != nil {
				rows.Close()
				return err
			}
			if domain.ModelFamily(brand, upstream) == family {
				count++
				familyMembers[id] = member{id: id, revision: revision, family: family}
			}
		}
		if err = rows.Err(); err != nil {
			rows.Close()
			return err
		}
		rows.Close()
		if count != len(in) {
			return domain.Err(409, "REVISION_CONFLICT", "同型号线路已变化，请重新读取后再保存。")
		}
		for _, item := range in {
			if _, ok := familyMembers[item.ID]; !ok {
				return domain.Err(409, "REVISION_CONFLICT", "同型号线路已变化，请重新读取后再保存。")
			}
		}

		for index, item := range in {
			if _, err = tx.Exec(ctx, `UPDATE catalog_models
SET route_priority=$2,revision=revision+1
WHERE id=$1 AND route_priority<>$2`, item.ID, index+1); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	items, err := s.Models(ctx)
	if err != nil {
		return nil, err
	}
	group := make([]CatalogModel, 0, len(in))
	for _, item := range items {
		if item.RouteFamily == family {
			group = append(group, item)
		}
	}
	return group, nil
}
func providerRevision(ctx context.Context, q Querier, id string) (Provider, error) {
	return one[Provider](ctx, q, `SELECT jsonb_build_object('id',p.id,'revision_id',r.id,'revision',p.revision,'name',r.name,'base_url',r.base_url,'proxy_policy',r.proxy_policy,'enabled',p.enabled,'key_ref',r.key_ref) FROM model_providers p JOIN provider_revisions r ON r.id=p.current_revision_id WHERE p.id=$1`, id)
}

// Internal key references are loaded separately; they never enter an API view.
func providerKey(ctx context.Context, q Querier, revision string) (string, error) {
	var key string
	e := q.QueryRow(ctx, "SELECT key_ref FROM provider_revisions WHERE id=$1", revision).Scan(&key)
	return key, e
}
func (s *Store) ProviderCredential(ctx context.Context, id string) (Provider, string, error) {
	p, e := s.Provider(ctx, id)
	if e != nil {
		return p, "", e
	}
	ref, e := providerKey(ctx, s.Pool, p.RevisionID)
	if e != nil {
		return p, "", e
	}
	key, e := s.Credential(ctx, ref)
	return p, key, e
}
func (s *Store) SaveProvider(ctx context.Context, id string, in ProviderInput) (Provider, error) {
	if in.ProxyPolicy == "" {
		in.ProxyPolicy = "server"
	}
	if in.ProxyPolicy != "server" && in.ProxyPolicy != "direct" {
		return Provider{}, domain.Err(422, "INVALID_ARGUMENT", "未知代理策略。")
	}
	in.Name = strings.TrimSpace(in.Name)
	in.BaseURL = strings.TrimRight(strings.TrimSpace(in.BaseURL), "/")
	if in.Name == "" || utf8.RuneCountInString(in.Name) > 80 {
		return Provider{}, domain.Err(422, "INVALID_ARGUMENT", "供应商名称为 1–80 字。")
	}
	if len(in.APIKey) > 8192 {
		return Provider{}, domain.Err(422, "INVALID_ARGUMENT", "密钥过长。")
	}
	if id == "" {
		id = "prv_" + domain.ID()
	}
	err := s.Tx(ctx, func(tx pgx.Tx) error {
		var catalogEnabled bool
		if e := tx.QueryRow(ctx, "SELECT "+catalogEnabledSQL+" FROM model_settings s").Scan(&catalogEnabled); e != nil {
			return e
		}
		old, e := providerRevision(ctx, tx, id)
		create := errors.Is(e, pgx.ErrNoRows)
		if e != nil && !create {
			return e
		}
		if !create && old.Revision != in.ExpectedRevision {
			return domain.Err(409, "REVISION_CONFLICT", "供应商已更新，请重新读取后保存。")
		}
		ref := ""
		if !create {
			ref, e = providerKey(ctx, tx, old.RevisionID)
			if e != nil {
				return e
			}
		}
		rev := domain.ID()
		if !create && old.Name == in.Name && old.BaseURL == in.BaseURL && old.ProxyPolicy == in.ProxyPolicy && in.APIKey == "" {
			rev = old.RevisionID
		}
		if in.APIKey != "" {
			ref = "provider:" + rev
			if e = s.putCredential(ctx, tx, ref, in.APIKey); e != nil {
				return e
			}
		}
		if create {
			if _, e = tx.Exec(ctx, "INSERT INTO model_providers(id,current_revision_id,enabled) VALUES($1,$2,$3)", id, rev, in.Enabled); e != nil {
				return e
			}
		} else {
			if _, e = tx.Exec(ctx, "UPDATE model_providers SET current_revision_id=$2,revision=revision+1,enabled=$3 WHERE id=$1", id, rev, in.Enabled); e != nil {
				return e
			}
		}
		if create || rev != old.RevisionID {
			if _, e = tx.Exec(ctx, "INSERT INTO provider_revisions(id,provider_id,name,base_url,key_ref,proxy_policy) VALUES($1,$2,$3,$4,$5,$6)", rev, id, in.Name, in.BaseURL, ref, in.ProxyPolicy); e != nil {
				return e
			}
			if !create {
				type prior struct {
					ID          string         `json:"id"`
					DisplayName string         `json:"display_name"`
					BrandID     string         `json:"brand_id"`
					Description string         `json:"description"`
					Profile     config.Profile `json:"profile"`
				}
				models, err := many[prior](ctx, tx, "SELECT jsonb_build_object('id',m.id,'display_name',r.display_name,'brand_id',r.brand_id,'description',r.description,'profile',c.profile) FROM catalog_models m JOIN model_revisions r ON r.id=m.current_revision_id JOIN runtime_config_revisions c ON c.id=r.config_id WHERE m.provider_id=$1 ORDER BY m.id", id)
				if err != nil {
					return err
				}
				for _, m := range models {
					cid, mrev := domain.ID(), domain.ID()
					p := m.Profile
					p.ModelBaseURL = in.BaseURL
					p.ModelKeyRef = ref
					p.ModelProxyPolicy = in.ProxyPolicy
					p.CacheHintsVerified = false
					if _, e = tx.Exec(ctx, "INSERT INTO runtime_config_revisions(id,profile) VALUES($1,$2)", cid, domain.JSON(p)); e != nil {
						return e
					}
					if _, e = tx.Exec(ctx, "INSERT INTO model_revisions(id,model_id,provider_revision_id,config_id,display_name,brand_id,description) VALUES($1,$2,$3,$4,$5,$6,$7)", mrev, m.ID, rev, cid, m.DisplayName, m.BrandID, m.Description); e != nil {
						return e
					}
					if _, e = tx.Exec(ctx, "UPDATE catalog_models SET current_revision_id=$2,revision=revision+1,enabled=false WHERE id=$1", m.ID, mrev); e != nil {
						return e
					}
				}
			}
		}
		_, e = tx.Exec(ctx, "UPDATE model_settings SET selection_enabled=selection_enabled OR $1,revision=revision+1", catalogEnabled)
		return e
	})
	if err != nil {
		return Provider{}, err
	}
	return s.Provider(ctx, id)
}
func modelIdentity(ctx context.Context, q Querier, cid string) (domain.GenerationModel, error) {
	v, e := one[domain.GenerationModel](ctx, q, `SELECT jsonb_build_object('id',r.model_id,'revision_id',r.id,'display_name',r.display_name,'brand_id',r.brand_id,'description',r.description,'selection_locked',true) FROM model_revisions r WHERE r.config_id=$1 ORDER BY r.created_at LIMIT 1`, cid)
	if errors.Is(e, pgx.ErrNoRows) {
		var name string
		e = q.QueryRow(ctx, "SELECT profile->>'model_name' FROM runtime_config_revisions WHERE id=$1", cid).Scan(&name)
		v = domain.GenerationModel{ID: "legacy", RevisionID: cid, DisplayName: name, BrandID: "generic", SelectionLocked: true, Legacy: true}
	}
	return v, e
}
func (s *Store) SaveModel(ctx context.Context, id string, in ModelInput) (CatalogModel, error) {
	in.DisplayName = strings.TrimSpace(in.DisplayName)
	in.UpstreamID = strings.TrimSpace(in.UpstreamID)
	if in.RoutePriority != nil && (*in.RoutePriority < 0 || *in.RoutePriority > 1000) {
		return CatalogModel{}, domain.Err(422, "INVALID_ARGUMENT", "线路尝试顺序须为 0–1000。")
	}
	if in.DisplayName == "" || utf8.RuneCountInString(in.DisplayName) > 80 || in.UpstreamID == "" || len(in.UpstreamID) > 256 || utf8.RuneCountInString(in.Description) > 240 {
		return CatalogModel{}, domain.Err(422, "INVALID_ARGUMENT", "请填写有效的模型名称、上游 ID 和简短说明。")
	}
	switch in.BrandID {
	case "openai", "qwen", "deepseek", "gemini", "claude", "kimi", "glm", "mimo", "doubao", "hunyuan", "generic":
	default:
		in.BrandID = "generic"
	}
	if in.CachePolicy == "" {
		in.CachePolicy = "provider_default"
	}
	if in.CachePolicy != "provider_default" && in.CachePolicy != "disabled_hints" && in.CachePolicy != "qwen_explicit" {
		return CatalogModel{}, domain.Err(422, "INVALID_ARGUMENT", "未知缓存策略。")
	}
	if id == "" {
		id = "mdl_" + domain.ID()
	}
	err := s.Tx(ctx, func(tx pgx.Tx) error {
		old, e := one[CatalogModel](ctx, tx, modelSelect+" WHERE m.id=$1", id)
		create := errors.Is(e, pgx.ErrNoRows)
		if e != nil && !create {
			return e
		}
		if !create && old.Revision != in.ExpectedRevision {
			return domain.Err(409, "REVISION_CONFLICT", "模型已更新，请重新读取后保存。")
		}
		provider, e := providerRevision(ctx, tx, in.ProviderID)
		if e != nil {
			return NotFound(e)
		}
		if !create && old.ProviderID != in.ProviderID {
			return domain.Err(422, "INVALID_ARGUMENT", "请为另一供应商新增模型。")
		}
		if in.InputCharsLimit == nil {
			in.InputCharsLimit = domain.Ptr(old.InputCharsLimit)
		}
		if in.ContextTokens == nil {
			in.ContextTokens = domain.Ptr(old.ContextTokens)
		}
		routePriority := old.RoutePriority
		if create {
			routePriority = 0
		}
		if in.RoutePriority != nil {
			routePriority = *in.RoutePriority
		}
		same := !create && old.ProviderRevisionID == provider.RevisionID && old.DisplayName == in.DisplayName && old.BrandID == in.BrandID && old.Description == in.Description && old.UpstreamID == in.UpstreamID && old.MaxOutputTokens == in.MaxOutputTokens && old.MaxInputChars == in.MaxInputChars && old.DisableThinking == in.DisableThinking && old.CachePolicy == in.CachePolicy && old.InputCharsLimit == *in.InputCharsLimit && old.ContextTokens == *in.ContextTokens
		if same {
			if in.Enabled && !old.Enabled && (!old.Verified || !provider.Enabled) {
				return domain.Err(409, "MODEL_NOT_VERIFIED", "启用前请先通过当前修订的图片与工具检查，并启用供应商。")
			}
			if _, e = tx.Exec(ctx, "UPDATE catalog_models SET enabled=$2,route_priority=$3,revision=revision+1 WHERE id=$1", id, in.Enabled, routePriority); e != nil {
				return e
			}
		} else {
			if in.Enabled {
				return domain.Err(409, "MODEL_NOT_VERIFIED", "新修订先保存为停用，检查通过后再启用。")
			}
			p, e := one[config.Profile](ctx, tx, "SELECT profile FROM runtime_config_revisions WHERE id=(SELECT config_id FROM runtime_settings)")
			if e != nil {
				return e
			}
			p.ModelBaseURL = provider.BaseURL
			p.ModelProxyPolicy = provider.ProxyPolicy
			p.ModelName = in.UpstreamID
			p.ModelDisableThinking = in.DisableThinking
			p.CachePolicy = in.CachePolicy
			p.CacheHintsVerified = false
			p.InputCharsLimit = *in.InputCharsLimit
			p.ContextTokens = *in.ContextTokens
			p.ModelKeyRef, e = providerKey(ctx, tx, provider.RevisionID)
			if e != nil {
				return e
			}
			if in.MaxOutputTokens != 0 {
				p.MaxOutputTokens = in.MaxOutputTokens
			}
			if in.MaxInputChars != 0 {
				p.MaxInputChars = in.MaxInputChars
			}
			if e = p.Validate(); e != nil {
				return domain.Err(422, "INVALID_ARGUMENT", e.Error())
			}
			rev, cid := domain.ID(), domain.ID()
			if create {
				if _, e = tx.Exec(ctx, "INSERT INTO catalog_models(id,provider_id,current_revision_id,route_priority) VALUES($1,$2,$3,$4)", id, in.ProviderID, rev, routePriority); e != nil {
					return e
				}
			} else {
				if _, e = tx.Exec(ctx, "UPDATE catalog_models SET current_revision_id=$2,revision=revision+1,enabled=false,route_priority=$3 WHERE id=$1", id, rev, routePriority); e != nil {
					return e
				}
			}
			if _, e = tx.Exec(ctx, "INSERT INTO runtime_config_revisions(id,profile) VALUES($1,$2)", cid, domain.JSON(p)); e != nil {
				return e
			}
			if _, e = tx.Exec(ctx, "INSERT INTO model_revisions(id,model_id,provider_revision_id,config_id,display_name,brand_id,description) VALUES($1,$2,$3,$4,$5,$6,$7)", rev, id, provider.RevisionID, cid, in.DisplayName, in.BrandID, in.Description); e != nil {
				return e
			}
		}
		_, e = tx.Exec(ctx, "UPDATE model_settings SET selection_enabled=selection_enabled OR $1,revision=revision+1", in.Enabled || old.Enabled)
		return e
	})
	if err != nil {
		return CatalogModel{}, err
	}
	return s.Model(ctx, id)
}
func selectable(ctx context.Context, q Querier, id, expected string) (domain.GenerationModel, string, error) {
	var rev, cid string
	var enabled, verified bool
	e := q.QueryRow(ctx, `SELECT r.id,r.config_id,m.enabled AND p.enabled,EXISTS(SELECT 1 FROM model_probes b WHERE b.revision_id=r.id AND b.mode='capabilities' AND b.status='succeeded') FROM catalog_models m JOIN model_revisions r ON r.id=m.current_revision_id JOIN model_providers p ON p.id=m.provider_id WHERE m.id=$1`, id).Scan(&rev, &cid, &enabled, &verified)
	if errors.Is(e, pgx.ErrNoRows) {
		return domain.GenerationModel{}, "", domain.Err(422, "MODEL_NOT_SELECTABLE", "模型不在可选目录中。")
	}
	if e != nil {
		return domain.GenerationModel{}, "", e
	}
	if expected != "" && expected != rev {
		return domain.GenerationModel{}, "", domain.Err(409, "MODEL_SELECTION_STALE", "模型目录已更新，请保留输入并重新选择。")
	}
	if !enabled || !verified {
		return domain.GenerationModel{}, "", domain.Err(503, "MODEL_UNAVAILABLE", "所选模型暂不可用；不会自动换用其他模型。")
	}
	m, e := modelIdentity(ctx, q, cid)
	return m, cid, e
}
func (s *Store) SetModelRecommended(ctx context.Context, id string, recommended bool, expected int64) (CatalogModel, error) {
	err := s.Tx(ctx, func(tx pgx.Tx) error {
		var rev int64
		e := tx.QueryRow(ctx, "SELECT revision FROM catalog_models WHERE id=$1", id).Scan(&rev)
		if errors.Is(e, pgx.ErrNoRows) {
			return domain.Err(404, "RESOURCE_NOT_FOUND", "模型不存在。")
		}
		if e != nil {
			return e
		}
		if rev != expected {
			return domain.Err(409, "REVISION_CONFLICT", "模型已更新，请重新读取后保存。")
		}
		if _, e = tx.Exec(ctx, "UPDATE catalog_models SET recommended=$2,revision=revision+1 WHERE id=$1", id, recommended); e != nil {
			return e
		}
		_, e = tx.Exec(ctx, "UPDATE model_settings SET revision=revision+1")
		return e
	})
	if err != nil {
		return CatalogModel{}, err
	}
	return s.Model(ctx, id)
}

func (s *Store) SetDefaultModel(ctx context.Context, id string, expected int64) error {
	return s.Tx(ctx, func(tx pgx.Tx) error {
		var rev int64
		if e := tx.QueryRow(ctx, "SELECT revision FROM model_settings").Scan(&rev); e != nil {
			return e
		}
		if rev != expected {
			return domain.Err(409, "REVISION_CONFLICT", "目录已更新，请重新读取。")
		}
		if _, _, e := selectable(ctx, tx, id, ""); e != nil {
			return e
		}
		_, e := tx.Exec(ctx, "UPDATE model_settings SET default_model_id=$1,revision=revision+1", id)
		return e
	})
}

type catalogState struct {
	enabled   bool
	defaultID *string
	revision  int64
	models    []domain.GenerationModel
}

// Visibility and implicit request selection use the same current eligibility.
// A stopped default is retained in owner settings, but cannot preselect a model.
func loadCatalog(ctx context.Context, q Querier) (catalogState, error) {
	c := catalogState{models: []domain.GenerationModel{}}
	var preferred *string
	var raw json.RawMessage
	// Read settings and models in one snapshot, including during first enablement.
	// Materialize the singleton once. A second unbounded settings scan in the
	// model projection inflated row estimates enough to trigger expensive JIT
	// compilation inside short request-control transactions on fresh databases.
	projection := strings.Replace(modelSelect, "CROSS JOIN model_settings s", "CROSS JOIN settings s", 1)
	err := q.QueryRow(ctx, `WITH settings AS MATERIALIZED (SELECT * FROM model_settings LIMIT 1)
SELECT `+catalogEnabledSQL+`,s.default_model_id,s.revision,
COALESCE((SELECT jsonb_agg(item ORDER BY item->>'display_name',item->>'id') FROM (`+projection+`) catalog(item)), '[]'::jsonb)
FROM settings s`).Scan(&c.enabled, &preferred, &c.revision, &raw)
	if err != nil {
		return c, err
	}
	var models []CatalogModel
	if err = json.Unmarshal(raw, &models); err != nil {
		return c, err
	}
	c.models, c.defaultID = publicModels(models, preferred)
	return c, nil
}

func (c catalogState) preselectedID() string {
	if c.defaultID != nil {
		return *c.defaultID
	}
	if len(c.models) > 0 {
		return c.models[0].ID
	}
	return ""
}

func (s *Store) ModelCatalog(ctx context.Context) (map[string]any, error) {
	c, e := loadCatalog(ctx, s.Pool)
	if e != nil {
		return nil, e
	}
	if !c.enabled {
		cid, p, e := s.CurrentConfig(ctx)
		if e != nil {
			return nil, e
		}
		if p.Validate() == nil {
			m, e := modelIdentity(ctx, s.Pool, cid)
			if e != nil {
				return nil, e
			}
			m.ID = "legacy"
			m.RevisionID = cid
			m.Legacy = true
			m.SelectionLocked = false
			c.models = append(c.models, m)
			c.defaultID = domain.Ptr("legacy")
		}
	}
	return map[string]any{"schema_version": 1, "revision": c.revision, "default_model_id": c.defaultID, "selection_enabled": c.enabled, "models": c.models}, nil
}
func resolveSpec(ctx context.Context, tx pgx.Tx, session string, in domain.RequestInput) (domain.GenerationSpec, error) {
	var spec domain.GenerationSpec
	spec.Version = 2
	// New source work records the parallel-window checkpoints and complete
	// extractor metadata contract; old specifications keep their own work key.
	spec.SourcePipelineRevision = "subtitle-adoption-parallel-v3"
	spec.DetailLevel = "standard"
	if in.DetailLevel != nil {
		spec.DetailLevel = *in.DetailLevel
	}
	policy, valid := domain.InterpretationPolicy(spec.DetailLevel)
	if !valid {
		return spec, &domain.Problem{Status: 422, Code: "INVALID_ARGUMENT", Detail: "请选择精简、标准或详细。", Meta: map[string]any{"field_errors": []map[string]string{{"field": "detail_level", "message": "仅允许 concise、standard、detailed。"}}}}
	}
	spec.DetailPolicy = &policy
	spec.DetailPolicyRevision = policy.Revision + ":" + Hash(policy)
	spec.Purpose = domain.NormalizePurpose(in.Purpose)
	spec.Scope = "public"
	if spec.Purpose != "" {
		spec.Scope = session
	}
	if utf8.RuneCountInString(spec.Purpose) > domain.PurposeCharsMax {
		return spec, &domain.Problem{Status: 422, Code: "INPUT_LIMIT_EXCEEDED", Detail: "关注点最多 500 字。", Meta: map[string]any{"field_errors": []map[string]string{{"field": "purpose", "message": "关注点最多 500 字。"}}}}
	}
	catalog, e := loadCatalog(ctx, tx)
	if e != nil {
		return spec, e
	}
	if catalog.enabled || in.ModelID != "" && in.ModelID != "legacy" {
		id := in.ModelID
		if id == "" {
			id = catalog.preselectedID()
		}
		if id == "" {
			return spec, domain.Err(503, "MODEL_UNAVAILABLE", "暂时没有可用模型。")
		}
		m, cid, e := selectable(ctx, tx, id, in.ModelRevisionID)
		spec.Model = m
		spec.ConfigID = cid
		return spec, e
	}
	if e := tx.QueryRow(ctx, "SELECT config_id FROM runtime_settings").Scan(&spec.ConfigID); e != nil {
		return spec, e
	}
	if in.ModelRevisionID != "" && in.ModelRevisionID != spec.ConfigID {
		return spec, domain.Err(409, "MODEL_SELECTION_STALE", "现有模型配置已更新，请重新读取目录。")
	}
	spec.Model, e = modelIdentity(ctx, tx, spec.ConfigID)
	// New requests freeze even the compatibility single-model profile. Only
	// pre-migration records retain the historical-Q&A provenance caveat.
	spec.Model.Legacy = false
	spec.Model.SelectionLocked = true
	return spec, e
}
func checkAvailable(ctx context.Context, q Querier, cid string) error {
	_, err := modelRoutes(ctx, q, cid)
	return err
}
func checkLegacyAvailable(ctx context.Context, q Querier, cid string) error {
	var found, bound, enabled, directory bool
	e := q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM model_revisions WHERE config_id=$1),EXISTS(SELECT 1 FROM legacy_model_bindings WHERE config_id=$1),COALESCE((SELECT m.enabled AND p.enabled FROM catalog_models m JOIN model_providers p ON p.id=m.provider_id WHERE m.id=COALESCE((SELECT model_id FROM model_revisions WHERE config_id=$1 LIMIT 1),(SELECT model_id FROM legacy_model_bindings WHERE config_id=$1))),true),(SELECT `+catalogEnabledSQL+` FROM model_settings s)`, cid).Scan(&found, &bound, &enabled, &directory)
	if e != nil {
		return e
	}
	if (found || bound && directory) && !enabled {
		return domain.Err(503, "MODEL_UNAVAILABLE", "本次解读的原模型已停用；文章仍可阅读和导出，问答暂不可用。")
	}
	return nil
}
func (s *Store) CheckModelAvailable(ctx context.Context, cid string) error {
	return checkAvailable(ctx, s.Pool, cid)
}
func (s *Store) GenerationForRun(ctx context.Context, run string) (domain.GenerationSpec, error) {
	return one[domain.GenerationSpec](ctx, s.Pool, `SELECT generation_spec FROM generation_requests WHERE active_run_id=$1 OR id=(SELECT request_id FROM qa_turns WHERE run_id=$1) ORDER BY created_at LIMIT 1`, run)
}
func (s *Store) CacheHintsVerified(ctx context.Context, cid string) (bool, error) {
	var yes bool
	e := s.Pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM model_probes p JOIN model_revisions m ON m.id=p.revision_id WHERE m.config_id=$1 AND p.mode='cache' AND p.status='succeeded' AND p.result->>'hints_verified'='true')`, cid).Scan(&yes)
	return yes, e
}
