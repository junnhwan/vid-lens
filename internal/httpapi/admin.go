package httpapi

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"vidlens/internal/domain"
	"vidlens/internal/outbound"
	"vidlens/internal/store"
)

const adminCookie = "vidlens_owner"

func (a *Server) ownerLogin(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Origin") != a.Config.Origin {
		a.problem(w, r, domain.Err(403, "CSRF_INVALID", "来源校验失败。"))
		return
	}
	var in struct {
		Password string `json:"password"`
	}
	if !a.body(w, r, &in) {
		return
	}
	if len(in.Password) > 512 {
		a.problem(w, r, domain.Err(401, "SESSION_REQUIRED", "无法登录，请检查密码或服务器设置。"))
		return
	}
	source, _, _ := net.SplitHostPort(r.RemoteAddr)
	source = hashToken("owner-login:" + source)
	if e := a.Store.OwnerLoginAllowed(r.Context(), source); e != nil {
		a.problem(w, r, e)
		return
	}
	select {
	case a.loginQueue <- struct{}{}:
		defer func() { <-a.loginQueue }()
	default:
		a.problem(w, r, domain.Err(429, "RATE_LIMITED", "登录繁忙，请稍后重试。"))
		return
	}
	select {
	case a.loginWorkers <- struct{}{}:
		defer func() { <-a.loginWorkers }()
	case <-r.Context().Done():
		return
	}
	h, version, e := a.Store.BeginOwnerLogin(r.Context(), source)
	if e != nil || !a.Store.VerifyOwnerPassword(in.Password, h) {
		a.problem(w, r, domain.Err(401, "SESSION_REQUIRED", "无法登录，请检查密码或服务器设置。"))
		return
	}
	b := make([]byte, 32)
	if _, e = rand.Read(b); e != nil {
		a.problem(w, r, e)
		return
	}
	token := base64.RawURLEncoding.EncodeToString(b)
	if e = a.Store.NewAdminSession(r.Context(), source, hashToken(token), version); e != nil {
		a.problem(w, r, e)
		return
	}
	if old, e := r.Cookie(adminCookie); e == nil {
		_ = a.Store.LogoutAdmin(r.Context(), hashToken(old.Value))
	}
	http.SetCookie(w, &http.Cookie{Name: adminCookie, Value: token, Path: "/", HttpOnly: true, Secure: a.Config.SecureCookie, SameSite: http.SameSiteStrictMode, MaxAge: 8 * 3600})
	a.json(w, 200, map[string]any{"authenticated": true, "csrf_token": a.csrf("owner:" + token)})
}
func (a *Server) ownerSession(w http.ResponseWriter, r *http.Request) {
	if !a.admin(w, r) {
		return
	}
	c, e := r.Cookie(adminCookie)
	if e != nil {
		a.json(w, 200, map[string]any{"authenticated": true, "authentication": "bearer"})
		return
	}
	if r.Method == "DELETE" {
		if e = a.Store.LogoutAdmin(r.Context(), hashToken(c.Value)); e != nil {
			a.problem(w, r, e)
			return
		}
		http.SetCookie(w, &http.Cookie{Name: adminCookie, Value: "", Path: "/", HttpOnly: true, Secure: a.Config.SecureCookie, SameSite: http.SameSiteStrictMode, MaxAge: -1})
		a.json(w, 200, map[string]bool{"authenticated": false})
		return
	}
	a.json(w, 200, map[string]any{"authenticated": true, "csrf_token": a.csrf("owner:" + c.Value)})
}
func (a *Server) providers(w http.ResponseWriter, r *http.Request) {
	if !a.admin(w, r) {
		return
	}
	if r.Method == "GET" {
		v, e := a.Store.Providers(r.Context())
		if e != nil {
			a.problem(w, r, e)
			return
		}
		a.json(w, 200, map[string]any{"items": v})
		return
	}
	var in store.ProviderInput
	if !a.body(w, r, &in) {
		return
	}
	if _, e := outbound.Validate(strings.TrimRight(strings.TrimSpace(in.BaseURL), "/"), a.Config.ModelAllowedHosts, true); e != nil {
		a.problem(w, r, domain.Err(422, "INVALID_ARGUMENT", "端点须为公开 HTTPS 地址；自托管目标需由服务器明确允许。"))
		return
	}
	v, e := a.Store.SaveProvider(r.Context(), r.PathValue("provider"), in)
	if e != nil {
		a.problem(w, r, e)
		return
	}
	a.json(w, 200, v)
}
func (a *Server) catalogModels(w http.ResponseWriter, r *http.Request) {
	if !a.admin(w, r) {
		return
	}
	if r.Method == "GET" {
		v, e := a.Store.Models(r.Context())
		if e != nil {
			a.problem(w, r, e)
			return
		}
		catalog, e := a.Store.ModelCatalog(r.Context())
		if e != nil {
			a.problem(w, r, e)
			return
		}
		a.json(w, 200, map[string]any{"items": v, "settings": catalog})
		return
	}
	var in store.ModelInput
	if !a.body(w, r, &in) {
		return
	}
	v, e := a.Store.SaveModel(r.Context(), r.PathValue("model"), in)
	if e != nil {
		a.problem(w, r, e)
		return
	}
	a.json(w, 200, v)
}

func (a *Server) modelRecommendation(w http.ResponseWriter, r *http.Request) {
	if !a.admin(w, r) {
		return
	}
	var in struct {
		Recommended      bool  `json:"recommended"`
		ExpectedRevision int64 `json:"expected_revision"`
	}
	if !a.body(w, r, &in) {
		return
	}
	v, e := a.Store.SetModelRecommended(r.Context(), r.PathValue("model"), in.Recommended, in.ExpectedRevision)
	if e != nil {
		a.problem(w, r, e)
		return
	}
	a.json(w, 200, v)
}

func (a *Server) modelRouteOrder(w http.ResponseWriter, r *http.Request) {
	if !a.admin(w, r) {
		return
	}
	var in struct {
		Items []store.ModelRouteOrderEntry `json:"items"`
	}
	if !a.body(w, r, &in) {
		return
	}
	items, err := a.Store.ReorderModelRoutes(r.Context(), in.Items)
	if err != nil {
		a.problem(w, r, err)
		return
	}
	a.json(w, 200, map[string]any{"items": items})
}

func (a *Server) modelSettings(w http.ResponseWriter, r *http.Request) {
	if !a.admin(w, r) {
		return
	}
	var in struct {
		DefaultModelID   string `json:"default_model_id"`
		ExpectedRevision int64  `json:"expected_revision"`
	}
	if !a.body(w, r, &in) {
		return
	}
	if e := a.Store.SetDefaultModel(r.Context(), in.DefaultModelID, in.ExpectedRevision); e != nil {
		a.problem(w, r, e)
		return
	}
	v, e := a.Store.ModelCatalog(r.Context())
	if e != nil {
		a.problem(w, r, e)
		return
	}
	a.json(w, 200, v)
}
func (a *Server) discoverModels(w http.ResponseWriter, r *http.Request) {
	if !a.admin(w, r) {
		return
	}
	if !a.body(w, r, &struct{}{}) {
		return
	}
	p, key, e := a.Store.ProviderCredential(r.Context(), r.PathValue("provider"))
	if e != nil {
		a.problem(w, r, e)
		return
	}
	if !p.Enabled {
		a.problem(w, r, domain.Err(409, "MODEL_UNAVAILABLE", "供应商已停用。"))
		return
	}
	u, e := outbound.Validate(p.BaseURL, a.Config.ModelAllowedHosts, true)
	if e != nil {
		a.problem(w, r, domain.Err(422, "INVALID_ARGUMENT", "端点不符合出站策略。"))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	client, e := outbound.CredentialClient(ctx, p.BaseURL, a.Config.ProxyURL, p.ProxyPolicy, a.Config.ModelAllowedHosts, 15*time.Second)
	if e != nil {
		a.problem(w, r, domain.Err(422, "PROVIDER_UNAVAILABLE", "模型列表端点或代理不可用。"))
		return
	}
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	defer client.CloseIdleConnections()
	req, _ := http.NewRequestWithContext(ctx, "GET", strings.TrimRight(u.String(), "/")+"/models", nil)
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Accept", "application/json")
	resp, e := client.Do(req)
	if e != nil {
		a.problem(w, r, domain.Err(422, "PROVIDER_UNAVAILABLE", "无法读取模型列表；可手动添加。"))
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		a.problem(w, r, domain.Err(422, "PROVIDER_UNAVAILABLE", "供应商未返回模型列表；可手动添加。"))
		return
	}
	b, e := io.ReadAll(io.LimitReader(resp.Body, (2<<20)+1))
	if e != nil || len(b) > 2<<20 {
		a.problem(w, r, domain.Err(422, "RESOURCE_LIMIT_EXCEEDED", "模型列表超过读取限制。"))
		return
	}
	var result struct {
		Data []struct {
			ID          string `json:"id"`
			DisplayName string `json:"display_name"`
			Name        string `json:"name"`
		} `json:"data"`
	}
	if json.Unmarshal(b, &result) != nil {
		a.problem(w, r, domain.Err(422, "PROVIDER_UNAVAILABLE", "模型列表格式不兼容。"))
		return
	}
	items := []map[string]string{}
	for _, m := range result.Data {
		if m.ID != "" && len(m.ID) <= 256 {
			name := strings.TrimSpace(m.DisplayName)
			if name == "" {
				name = strings.TrimSpace(m.Name)
			}
			if name == "" {
				name = m.ID
			}
			items = append(items, map[string]string{"id": m.ID, "display_name": name})
		}
		if len(items) == 1000 {
			break
		}
	}
	a.json(w, 200, map[string]any{"items": items, "discovery_only": true, "truncated": len(result.Data) > 1000})
}
func (a *Server) probes(w http.ResponseWriter, r *http.Request) {
	if !a.admin(w, r) {
		return
	}
	id := r.PathValue("model")
	if r.Method == "GET" {
		v, e := a.Store.Probes(r.Context(), id)
		if e != nil {
			a.problem(w, r, e)
			return
		}
		a.json(w, 200, map[string]any{"items": v})
		return
	}
	key, ok := a.key(w, r)
	if !ok {
		return
	}
	var in struct {
		RevisionID string `json:"revision_id"`
		Mode       string `json:"mode"`
	}
	if !a.body(w, r, &in) {
		return
	}
	x, e := a.Store.CreateProbe(r.Context(), id, in.RevisionID, in.Mode, key)
	if e != nil {
		a.problem(w, r, e)
		return
	}
	w.Header().Set("Idempotency-Replayed", fmtBool(x.Replayed))
	w.Header().Set("Idempotency-Expires-At", x.Expires.Format(time.RFC3339))
	a.json(w, 202, map[string]any{"id": x.ID, "status_url": "/api/v1/admin/models/" + id + "/probes"})
}
func fmtBool(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
func diagnosticsPage(r *http.Request) (int, int, error) {
	limit, offset := 20, 0
	if q := r.URL.Query().Get("limit"); q != "" {
		n, e := strconv.Atoi(q)
		if e != nil || n < 1 || n > 100 {
			return 0, 0, domain.Err(422, "INVALID_ARGUMENT", "每页条数需在 1 到 100 之间。")
		}
		limit = n
	}
	if q := r.URL.Query().Get("offset"); q != "" {
		n, e := strconv.Atoi(q)
		if e != nil || n < 0 || n > 1_000_000 {
			return 0, 0, domain.Err(422, "INVALID_ARGUMENT", "分页起点无效。")
		}
		offset = n
	}
	return limit, offset, nil
}
func (a *Server) diagnostics(w http.ResponseWriter, r *http.Request) {
	if !a.admin(w, r) {
		return
	}
	limit, offset, e := diagnosticsPage(r)
	if e != nil {
		a.problem(w, r, e)
		return
	}
	v, e := a.Store.ModelReporting(r.Context(), limit, offset)
	if e != nil {
		a.problem(w, r, e)
		return
	}
	a.json(w, 200, v)
}
func (a *Server) stats(w http.ResponseWriter, r *http.Request) {
	v, e := a.Store.SiteStats(r.Context())
	if e != nil {
		a.problem(w, r, e)
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=30")
	a.json(w, 200, v)
}
func (a *Server) like(w http.ResponseWriter, r *http.Request) {
	s, ok := a.auth(w, r)
	if !ok {
		return
	}
	if r.Method == "PUT" && !a.body(w, r, &struct{}{}) {
		return
	}
	v, e := a.Store.SiteLike(r.Context(), s.ID, r.Method == "PUT")
	if e != nil {
		a.problem(w, r, e)
		return
	}
	a.json(w, 200, v)
}
func (a *Server) publicModels(w http.ResponseWriter, r *http.Request) {
	v, e := a.Store.ModelCatalog(r.Context())
	if e != nil {
		a.problem(w, r, e)
		return
	}
	a.json(w, 200, v)
}
func (a *Server) importCurrent(w http.ResponseWriter, r *http.Request) {
	if !a.admin(w, r) {
		return
	}
	if !a.body(w, r, &struct{}{}) {
		return
	}
	m, e := a.Store.ImportCurrentModel(r.Context())
	if e != nil {
		a.problem(w, r, e)
		return
	}
	a.json(w, 200, m)
}
func (a *Server) adminAuthenticated(w http.ResponseWriter, r *http.Request) bool {
	if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, "Bearer ") && len(a.Config.AdminToken) >= 32 && subtle.ConstantTimeCompare([]byte(strings.TrimPrefix(auth, "Bearer ")), []byte(a.Config.AdminToken)) == 1 {
		return true
	}
	c, e := r.Cookie(adminCookie)
	if e == nil {
		_, e = a.Store.AdminSession(r.Context(), hashToken(c.Value))
	}
	if e != nil {
		a.problem(w, r, domain.Err(401, "SESSION_REQUIRED", "请使用站长密码登录。"))
		return false
	}
	if r.Method != "GET" && (r.Header.Get("Origin") != a.Config.Origin || subtle.ConstantTimeCompare([]byte(r.Header.Get("X-CSRF-Token")), []byte(a.csrf("owner:"+c.Value))) != 1) {
		a.problem(w, r, domain.Err(403, "CSRF_INVALID", "管理会话校验失败，请重新登录。"))
		return false
	}
	return true
}
