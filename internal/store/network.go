package store

import (
	"context"
	"errors"
	"strings"
	"unicode"

	"github.com/jackc/pgx/v5"
	"vidlens/internal/config"
	"vidlens/internal/domain"
)

type NetworkSettings struct {
	Revision             int64  `json:"revision"`
	ReferenceReadEnabled bool   `json:"reference_read_enabled"`
	SearchEnabled        bool   `json:"search_enabled"`
	DailyLimit           int    `json:"daily_limit"`
	MonthlyLimit         int    `json:"monthly_limit"`
	KeyConfigured        bool   `json:"key_configured"`
	KeyRef               string `json:"-"`
}

type NetworkInput struct {
	ExpectedRevision     int64  `json:"expected_revision"`
	ReferenceReadEnabled bool   `json:"reference_read_enabled"`
	SearchEnabled        bool   `json:"search_enabled"`
	DailyLimit           int    `json:"daily_limit"`
	MonthlyLimit         int    `json:"monthly_limit"`
	APIKey               string `json:"api_key"`
	ClearKey             bool   `json:"clear_key"`
}

func networkSettings(ctx context.Context, q Querier, fallback config.Config) (NetworkSettings, error) {
	var v NetworkSettings
	e := q.QueryRow(ctx, "SELECT revision,reference_read_enabled,search_enabled,daily_limit,monthly_limit,key_ref FROM network_settings WHERE singleton").Scan(&v.Revision, &v.ReferenceReadEnabled, &v.SearchEnabled, &v.DailyLimit, &v.MonthlyLimit, &v.KeyRef)
	if errors.Is(e, pgx.ErrNoRows) {
		return NetworkSettings{ReferenceReadEnabled: fallback.ReferenceReadEnabled, SearchEnabled: fallback.SearchEnabled, DailyLimit: 20, MonthlyLimit: fallback.SearchMonthlyLimit, KeyConfigured: fallback.TavilyAPIKey != ""}, nil
	}
	v.KeyConfigured = v.KeyRef != ""
	return v, e
}

// Until the first owner save, retain the existing YAML/environment configuration.
// Thereafter the database is authoritative, including an explicitly cleared key.
func (s *Store) NetworkSettings(ctx context.Context, fallback config.Config) (NetworkSettings, error) {
	return networkSettings(ctx, s.Pool, fallback)
}

func (s *Store) NetworkConfig(ctx context.Context, fallback config.Config) (config.Config, error) {
	v, e := s.NetworkSettings(ctx, fallback)
	if e != nil {
		return fallback, e
	}
	fallback.ReferenceReadEnabled, fallback.SearchEnabled, fallback.SearchMonthlyLimit = v.ReferenceReadEnabled, v.SearchEnabled, v.MonthlyLimit
	if v.Revision > 0 {
		fallback.TavilyAPIKey = ""
		if v.KeyRef != "" {
			fallback.TavilyAPIKey, e = s.Credential(ctx, v.KeyRef)
		}
	}
	return fallback, e
}

func (s *Store) SaveNetworkSettings(ctx context.Context, fallback config.Config, in NetworkInput) (NetworkSettings, error) {
	in.APIKey = strings.TrimSpace(in.APIKey)
	if in.ExpectedRevision < 0 || in.DailyLimit < 0 || in.DailyLimit > 1000 || in.MonthlyLimit < 0 || in.MonthlyLimit > 1000 || len(in.APIKey) > 512 || strings.IndexFunc(in.APIKey, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0 || (in.ClearKey && in.APIKey != "") {
		return NetworkSettings{}, domain.Err(422, "INVALID_ARGUMENT", "每日及每月搜索额度需在 0–1000 之间，Key 不能含空白或同时清除与替换。")
	}
	var v NetworkSettings
	e := s.Tx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		old, e := networkSettings(ctx, tx, fallback)
		if e != nil {
			return e
		}
		if old.Revision != in.ExpectedRevision {
			return domain.Err(409, "REVISION_CONFLICT", "联网配置已被其他页面修改，请重新读取后再保存。")
		}
		ref := old.KeyRef
		key := in.APIKey
		if old.Revision == 0 && !in.ClearKey && key == "" {
			key = fallback.TavilyAPIKey
		}
		if in.ClearKey {
			ref = ""
		}
		if key != "" {
			ref = "tavily:" + domain.ID()
			if e = s.putCredential(ctx, tx, ref, key); e != nil {
				return e
			}
		}
		if in.SearchEnabled && (!in.ReferenceReadEnabled || ref == "") {
			return domain.Err(422, "INVALID_ARGUMENT", "启用搜索前，请打开网页读取并填写 Tavily API Key。")
		}
		v = NetworkSettings{Revision: old.Revision + 1, ReferenceReadEnabled: in.ReferenceReadEnabled, SearchEnabled: in.SearchEnabled, DailyLimit: in.DailyLimit, MonthlyLimit: in.MonthlyLimit, KeyConfigured: ref != "", KeyRef: ref}
		_, e = tx.Exec(ctx, `INSERT INTO network_settings(singleton,revision,reference_read_enabled,search_enabled,daily_limit,monthly_limit,key_ref) VALUES(true,$1,$2,$3,$4,$5,$6)
            ON CONFLICT(singleton) DO UPDATE SET revision=EXCLUDED.revision,reference_read_enabled=EXCLUDED.reference_read_enabled,search_enabled=EXCLUDED.search_enabled,daily_limit=EXCLUDED.daily_limit,monthly_limit=EXCLUDED.monthly_limit,key_ref=EXCLUDED.key_ref,updated_at=now()`, v.Revision, v.ReferenceReadEnabled, v.SearchEnabled, v.DailyLimit, v.MonthlyLimit, v.KeyRef)
		// No runs retain this credential identity; an admitted call has its own
		// in-memory key. Remove superseded keys without touching model credentials.
		if e == nil && old.KeyRef != "" && old.KeyRef != ref {
			_, e = tx.Exec(ctx, "DELETE FROM credentials WHERE name=$1", old.KeyRef)
		}
		return e
	})
	return v, e
}
