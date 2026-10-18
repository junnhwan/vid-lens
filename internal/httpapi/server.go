package httpapi

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"vidlens/internal/config"
	"vidlens/internal/domain"
	"vidlens/internal/outbound"
	"vidlens/internal/store"
)

const cookieName = "vidlens_session"

type Server struct {
	Store        *store.Store
	Config       config.Config
	hub          *Hub
	CheckModel   func(context.Context, config.Profile) (any, error)
	loginWorkers chan struct{}
	loginQueue   chan struct{}
}

func New(ctx context.Context, s *store.Store, c config.Config) *Server {
	a := &Server{Store: s, Config: c, loginWorkers: make(chan struct{}, 2), loginQueue: make(chan struct{}, 6)}
	a.hub = NewHub(ctx, a)
	return a
}
func (a *Server) Handler() http.Handler {
	m := http.NewServeMux()
	m.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if e := a.Store.Ping(ctx); e != nil {
			a.problem(w, r, domain.Err(503, "SERVICE_UNAVAILABLE", "数据库暂不可用。"))
			return
		}
		a.json(w, 200, map[string]any{"schema_version": 1, "status": "ok"})
	})
	m.HandleFunc("POST /api/v1/session", a.session)
	m.HandleFunc("GET /api/v1/site/stats", a.stats)
	m.HandleFunc("GET /api/v1/site/announcement", a.publicAnnouncement)
	m.HandleFunc("GET /api/v1/examples", a.publicExamples)
	m.HandleFunc("GET /api/v1/examples/{article}/frames/{frame}/content", a.publicExampleFrame)
	m.HandleFunc("GET /api/v1/examples/{article}", a.publicExample)
	m.HandleFunc("GET /api/v1/site/capacity", a.capacity)
	m.HandleFunc("GET /api/v1/site/like", a.like)
	m.HandleFunc("PUT /api/v1/site/like", a.like)
	m.HandleFunc("GET /api/v1/models", a.publicModels)
	m.HandleFunc("POST /api/v1/admin/login", a.ownerLogin)
	m.HandleFunc("GET /api/v1/admin/session", a.ownerSession)
	m.HandleFunc("DELETE /api/v1/admin/session", a.ownerSession)
	m.HandleFunc("GET /api/v1/admin/providers", a.providers)
	m.HandleFunc("POST /api/v1/admin/providers", a.providers)
	m.HandleFunc("PUT /api/v1/admin/providers/{provider}", a.providers)
	m.HandleFunc("POST /api/v1/admin/providers/{provider}/discover", a.discoverModels)
	m.HandleFunc("GET /api/v1/admin/models", a.catalogModels)
	m.HandleFunc("POST /api/v1/admin/models", a.catalogModels)
	m.HandleFunc("POST /api/v1/admin/models/import-current", a.importCurrent)
	m.HandleFunc("PUT /api/v1/admin/models/{model}", a.catalogModels)
	m.HandleFunc("PUT /api/v1/admin/models/{model}/recommendation", a.modelRecommendation)
	m.HandleFunc("POST /api/v1/admin/model-routes/order", a.modelRouteOrder)
	m.HandleFunc("PUT /api/v1/admin/model-settings", a.modelSettings)
	m.HandleFunc("POST /api/v1/admin/models/{model}/probes", a.probes)
	m.HandleFunc("GET /api/v1/admin/models/{model}/probes", a.probes)
	m.HandleFunc("GET /api/v1/admin/diagnostics", a.diagnostics)
	m.HandleFunc("GET /api/v1/admin/runs/{run}/diagnostics", a.runDiagnostics)
	m.HandleFunc("GET /api/v1/admin/network-settings", a.networkSettings)
	m.HandleFunc("PUT /api/v1/admin/network-settings", a.networkSettings)
	m.HandleFunc("GET /api/v1/admin/announcement", a.announcementSettings)
	m.HandleFunc("PUT /api/v1/admin/announcement", a.announcementSettings)
	m.HandleFunc("GET /api/v1/admin/storage", a.storageSettings)
	m.HandleFunc("PUT /api/v1/admin/storage", a.storageSettings)
	m.HandleFunc("GET /api/v1/admin/videos", a.managedVideos)
	m.HandleFunc("DELETE /api/v1/admin/videos/{source}/media", a.deleteVideo)
	m.HandleFunc("GET /api/v1/admin/examples", a.adminExamples)
	m.HandleFunc("PUT /api/v1/admin/examples", a.saveExamples)
	m.HandleFunc("GET /api/v1/admin/config", a.adminConfig)
	m.HandleFunc("PUT /api/v1/admin/config", a.adminConfig)
	m.HandleFunc("POST /api/v1/admin/config/test", a.adminTest)
	m.HandleFunc("GET /api/v1/admin/status", a.adminStatus)
	m.HandleFunc("POST /api/v1/requests", a.createRequest)
	m.HandleFunc("GET /api/v1/requests", a.requestHistory)
	m.HandleFunc("GET /api/v1/requests/{request}", a.getRequest)
	m.HandleFunc("GET /api/v1/requests/{request}/share", a.requestShare)
	m.HandleFunc("PUT /api/v1/requests/{request}/share", a.requestShare)
	m.HandleFunc("DELETE /api/v1/requests/{request}/share", a.requestShare)
	m.HandleFunc("GET /api/v1/shares/{token}", a.sharedRecord)
	m.HandleFunc("GET /api/v1/shares/{token}/frames/{frame}/content", a.sharedFrame)
	m.HandleFunc("POST /api/v1/requests/{request}/cancel", a.cancelRequest)
	m.HandleFunc("POST /api/v1/requests/{request}/retry", a.retryRequest)
	m.HandleFunc("GET /api/v1/requests/{request}/article", a.article)
	m.HandleFunc("POST /api/v1/requests/{request}/exports", a.exports)
	m.HandleFunc("GET /api/v1/requests/{request}/exports/latest", a.exports)
	m.HandleFunc("GET /api/v1/requests/{request}/exports/{export}", a.exports)
	m.HandleFunc("GET /api/v1/requests/{request}/exports/{export}/content", a.exportContent)
	m.HandleFunc("GET /api/v1/requests/{request}/activity", a.activity)
	m.HandleFunc("GET /api/v1/requests/{request}/events", a.events)
	m.HandleFunc("GET /api/v1/requests/{request}/frames/{frame}", a.frame)
	m.HandleFunc("GET /api/v1/requests/{request}/frames/{frame}/content", a.frameContent)
	m.HandleFunc("GET /api/v1/requests/{request}/references/{reference}", a.reference)
	m.HandleFunc("POST /api/v1/requests/{request}/conversation", a.conversation)
	m.HandleFunc("GET /api/v1/conversations/{conversation}/turns", a.turns)
	m.HandleFunc("POST /api/v1/conversations/{conversation}/turns", a.createTurn)
	m.HandleFunc("GET /api/v1/conversations/{conversation}/turns/{turn}", a.getTurn)
	m.HandleFunc("POST /api/v1/conversations/{conversation}/turns/{turn}/cancel", a.cancelTurn)
	m.HandleFunc("POST /api/v1/conversations/{conversation}/turns/{turn}/retry", a.retryTurn)
	m.HandleFunc("GET /api/v1/conversations/{conversation}/turns/{turn}/events", a.events)
	m.HandleFunc("GET /api/v1/conversations/{conversation}/turns/{turn}/activity", a.activity)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r = r.WithContext(domain.WithTrace(r.Context(), domain.ID()))
		w.Header().Set("X-Request-ID", domain.TraceID(r.Context()))
		w.Header().Set("Cache-Control", "private, no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		defer func() {
			if v := recover(); v != nil {
				slog.Error("http handler panic", "path", r.URL.Path)
				a.problem(w, r, domain.Err(503, "SERVICE_UNAVAILABLE", "服务暂不可用。"))
			}
		}()
		if strings.HasPrefix(r.URL.Path, "/api/v1/admin/") {
			ownerAudit(m).ServeHTTP(w, r)
		} else {
			m.ServeHTTP(w, r)
		}
	})
}
func hashToken(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }
func (a *Server) csrf(token string) string {
	h := hmac.New(sha256.New, []byte(a.Config.SecretKey))
	h.Write([]byte("csrf:" + token))
	return base64.RawURLEncoding.EncodeToString(h.Sum(nil))
}
func (a *Server) auth(w http.ResponseWriter, r *http.Request) (store.Session, bool) {
	var v store.Session
	c, e := r.Cookie(cookieName)
	if e == nil {
		v, e = a.Store.Session(r.Context(), hashToken(c.Value))
	}
	if e != nil {
		if errors.Is(e, http.ErrNoCookie) || errors.Is(e, pgx.ErrNoRows) {
			a.problem(w, r, domain.Err(401, "SESSION_REQUIRED", "匿名会话已失效，请重新开始。"))
		} else {
			a.problem(w, r, domain.Err(503, "SERVICE_UNAVAILABLE", "会话服务暂不可用，请稍后重试。"))
		}
		return v, false
	}
	if r.Method != "GET" {
		if r.Header.Get("Origin") != a.Config.Origin || !hmac.Equal([]byte(r.Header.Get("X-CSRF-Token")), []byte(a.csrf(c.Value))) {
			a.problem(w, r, domain.Err(403, "CSRF_INVALID", "会话校验失败，请重新获取会话信息。"))
			return v, false
		}
	}
	return v, true
}
func (a *Server) session(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Origin") != a.Config.Origin {
		a.problem(w, r, domain.Err(403, "CSRF_INVALID", "来源校验失败。"))
		return
	}
	if !a.body(w, r, &struct{}{}) {
		return
	}
	var v store.Session
	var token string
	code := 200
	c, e := r.Cookie(cookieName)
	if e == nil {
		token = c.Value
		v, e = a.Store.Session(r.Context(), hashToken(token))
		if e != nil && !errors.Is(e, pgx.ErrNoRows) {
			a.problem(w, r, domain.Err(503, "SERVICE_UNAVAILABLE", "会话服务暂不可用，请稍后重试。"))
			return
		}
	}
	if e != nil || v.ID == "" {
		code = 201
		b := make([]byte, 32)
		if _, e = rand.Read(b); e != nil {
			a.problem(w, r, e)
			return
		}
		token = base64.RawURLEncoding.EncodeToString(b)
		v, e = a.Store.NewSession(r.Context(), hashToken(token), a.Config.SessionTTL)
		if e != nil {
			a.problem(w, r, domain.Err(503, "SERVICE_UNAVAILABLE", "会话服务暂不可用，请稍后重试。"))
			return
		}
		http.SetCookie(w, &http.Cookie{Name: cookieName, Value: token, Path: "/", HttpOnly: true, Secure: a.Config.SecureCookie, SameSite: http.SameSiteLaxMode, Expires: v.ExpiresAt, MaxAge: int(a.Config.SessionTTL.Seconds())})
	}
	_, p, e := a.Store.CurrentConfig(r.Context())
	if e != nil {
		a.problem(w, r, domain.Err(503, "SERVICE_UNAVAILABLE", "服务配置暂不可读取，请稍后重试。"))
		return
	}
	catalog, e := a.Store.ModelCatalog(r.Context())
	if e != nil {
		a.problem(w, r, e)
		return
	}
	network, e := a.Store.NetworkSettings(r.Context(), a.Config)
	if e != nil {
		a.problem(w, r, e)
		return
	}
	models, _ := catalog["models"].([]domain.GenerationModel)
	a.json(w, code, map[string]any{"schema_version": 1, "session": v, "csrf_token": a.csrf(token), "capabilities": map[string]any{"supported_platforms": p.Platforms, "accepting_requests": len(models) > 0, "question_chars_max": p.QuestionCharsMax, "selection_chars_max": p.SelectionCharsMax, "idempotency_window_seconds": p.IdempotencySeconds, "detail_levels": []string{"concise", "standard", "detailed"}, "default_detail_level": "standard", "purpose_input": true, "max_purpose_chars": domain.PurposeCharsMax, "model_selection": true, "article_export": map[string]any{"formats": []string{"markdown", "markdown_zip"}}, "external_sources": map[string]bool{"enabled": network.ReferenceReadEnabled, "search_enabled": network.ReferenceReadEnabled && network.SearchEnabled && network.KeyConfigured}}})
}
func (a *Server) body(w http.ResponseWriter, r *http.Request, out any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if e := d.Decode(out); e != nil {
		a.problem(w, r, domain.Err(400, "INVALID_ARGUMENT", "请求 JSON 无效。"))
		return false
	}
	if e := d.Decode(&struct{}{}); e != io.EOF {
		a.problem(w, r, domain.Err(400, "INVALID_ARGUMENT", "只允许一个 JSON 对象。"))
		return false
	}
	return true
}
func (a *Server) key(w http.ResponseWriter, r *http.Request) (string, bool) {
	k := r.Header.Get("Idempotency-Key")
	if len(k) < 1 || len(k) > 200 || strings.ContainsAny(k, "\r\n") {
		a.problem(w, r, domain.Err(400, "INVALID_ARGUMENT", "缺少有效的 Idempotency-Key。"))
		return "", false
	}
	return k, true
}
func (a *Server) json(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func (a *Server) problem(w http.ResponseWriter, r *http.Request, err error) {
	p := &domain.Problem{Status: 503, Code: "SERVICE_UNAVAILABLE", Detail: "服务暂不可用，请稍后读取资源状态。"}
	var given *domain.Problem
	if errors.As(err, &given) {
		p = given
	} else {
		slog.Error("request failed", "path", r.URL.Path, "class", "internal", "http_trace_id", domain.TraceID(r.Context()))
	}
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(p.Status)
	_ = json.NewEncoder(w).Encode(map[string]any{"schema_version": 1, "type": "about:blank", "title": http.StatusText(p.Status), "status": p.Status, "code": p.Code, "detail": p.Detail, "instance": r.URL.Path, "trace_id": domain.TraceID(r.Context()), "meta": p.Meta})
}
func (a *Server) accepted(w http.ResponseWriter, r *http.Request, x store.Accepted, v any, location string) {
	status := 201
	if x.Replayed {
		status = 200
		w.Header().Set("Idempotency-Replayed", "true")
	}
	w.Header().Set("Idempotency-Expires-At", x.Expires.UTC().Format(time.RFC3339))
	w.Header().Set("Location", location)
	a.json(w, status, v)
}
func (a *Server) createRequest(w http.ResponseWriter, r *http.Request) {
	ses, ok := a.auth(w, r)
	if !ok {
		return
	}
	key, ok := a.key(w, r)
	if !ok {
		return
	}
	var in domain.RequestInput
	if !a.body(w, r, &in) {
		return
	}
	x, e := a.Store.CreateRequestWithInput(r.Context(), ses.ID, key, in, "", a.Config.DevMediaDir != "")
	if e != nil {
		a.problem(w, r, e)
		return
	}
	v, _, e := a.Store.RequestView(r.Context(), ses.ID, x.ID)
	if e != nil {
		a.problem(w, r, e)
		return
	}
	a.accepted(w, r, x, v, "/api/v1/requests/"+x.ID)
}
func (a *Server) requestHistory(w http.ResponseWriter, r *http.Request) {
	ses, ok := a.auth(w, r)
	if !ok {
		return
	}
	limit, offset := 20, 0
	if value := r.URL.Query().Get("limit"); value != "" {
		n, err := strconv.Atoi(value)
		if err != nil || n < 1 || n > 50 {
			a.problem(w, r, domain.Err(400, "INVALID_ARGUMENT", "limit 必须在 1 到 50 之间。"))
			return
		}
		limit = n
	}
	if value := r.URL.Query().Get("offset"); value != "" {
		n, err := strconv.Atoi(value)
		if err != nil || n < 0 || n > 1_000_000 {
			a.problem(w, r, domain.Err(400, "INVALID_ARGUMENT", "offset 无效。"))
			return
		}
		offset = n
	}
	page, err := a.Store.RequestHistory(r.Context(), ses.ID, limit, offset)
	if err != nil {
		a.problem(w, r, err)
		return
	}
	a.json(w, 200, page)
}
func (a *Server) getRequest(w http.ResponseWriter, r *http.Request) {
	ses, ok := a.auth(w, r)
	if !ok {
		return
	}
	v, _, e := a.Store.RequestView(r.Context(), ses.ID, r.PathValue("request"))
	if e != nil {
		a.problem(w, r, e)
		return
	}
	a.json(w, 200, v)
}
func (a *Server) cancelRequest(w http.ResponseWriter, r *http.Request) {
	ses, ok := a.auth(w, r)
	if !ok {
		return
	}
	if !a.body(w, r, &struct{}{}) {
		return
	}
	if e := a.Store.CancelRequest(r.Context(), ses.ID, r.PathValue("request")); e != nil {
		a.problem(w, r, e)
		return
	}
	a.getRequest(w, r)
}
func (a *Server) retryRequest(w http.ResponseWriter, r *http.Request) {
	ses, ok := a.auth(w, r)
	if !ok {
		return
	}
	key, ok := a.key(w, r)
	if !ok {
		return
	}
	if !a.body(w, r, &struct{}{}) {
		return
	}
	x, e := a.Store.CreateRequest(r.Context(), ses.ID, key, "", r.PathValue("request"), a.Config.DevMediaDir != "")
	if e != nil {
		a.problem(w, r, e)
		return
	}
	v, _, e := a.Store.RequestView(r.Context(), ses.ID, x.ID)
	if e != nil {
		a.problem(w, r, e)
		return
	}
	a.accepted(w, r, x, v, "/api/v1/requests/"+x.ID)
}
func (a *Server) article(w http.ResponseWriter, r *http.Request) {
	ses, ok := a.auth(w, r)
	if !ok {
		return
	}
	v, e := a.Store.Article(r.Context(), ses.ID, r.PathValue("request"))
	if e != nil {
		a.problem(w, r, e)
		return
	}
	a.json(w, 200, v)
}
func (a *Server) frame(w http.ResponseWriter, r *http.Request) {
	ses, ok := a.auth(w, r)
	if !ok {
		return
	}
	f, _, e := a.Store.Frame(r.Context(), ses.ID, r.PathValue("request"), r.PathValue("frame"))
	if e != nil {
		a.problem(w, r, e)
		return
	}
	a.json(w, 200, map[string]any{"schema_version": 1, "frame": f})
}
func (a *Server) frameContent(w http.ResponseWriter, r *http.Request) {
	ses, ok := a.auth(w, r)
	if !ok {
		return
	}
	variant := r.URL.Query().Get("variant")
	if variant != "" && variant != "full" && variant != "thumbnail" {
		a.problem(w, r, domain.Err(400, "INVALID_ARGUMENT", "图片 variant 无效。"))
		return
	}
	f, asset, e := a.Store.Frame(r.Context(), ses.ID, r.PathValue("request"), r.PathValue("frame"))
	if e != nil {
		a.problem(w, r, e)
		return
	}
	if f.Availability == "expired" {
		a.problem(w, r, domain.Err(410, "FRAME_EXPIRED", "这张临时图片已过期。"))
		return
	}
	if f.Availability != "ready" {
		a.problem(w, r, domain.Err(503, "SERVICE_UNAVAILABLE", "图片暂不可读。"))
		return
	}
	a.serveStoredAsset(w, r, asset)
}
func (a *Server) conversation(w http.ResponseWriter, r *http.Request) {
	ses, ok := a.auth(w, r)
	if !ok {
		return
	}
	if !a.body(w, r, &struct{}{}) {
		return
	}
	c, new, e := a.Store.EnsureConversation(r.Context(), ses.ID, r.PathValue("request"))
	if e != nil {
		a.problem(w, r, e)
		return
	}
	c.SessionID = ""
	c.NextTurnNo = 0
	status := 200
	if new {
		status = 201
	}
	a.json(w, status, map[string]any{"schema_version": 1, "conversation": c})
}
func (a *Server) createTurn(w http.ResponseWriter, r *http.Request) {
	ses, ok := a.auth(w, r)
	if !ok {
		return
	}
	key, ok := a.key(w, r)
	if !ok {
		return
	}
	var in domain.QuestionInput
	if !a.body(w, r, &in) {
		return
	}
	if in.AllowExternalSources && contractVersion(r) < 2 {
		a.problem(w, r, domain.Err(409, "CLIENT_UPDATE_REQUIRED", "网络补充需要新版页面。"))
		return
	}
	network, e := a.Store.NetworkSettings(r.Context(), a.Config)
	if e != nil {
		a.problem(w, r, e)
		return
	}
	if in.AllowExternalSources && !network.ReferenceReadEnabled {
		a.problem(w, r, domain.Err(503, "EXTERNAL_SOURCES_DISABLED", "站长尚未启用网络补充；可继续围绕视频提问。"))
		return
	}
	x, e := a.Store.CreateTurn(r.Context(), ses.ID, r.PathValue("conversation"), key, in, "")
	if e != nil {
		a.problem(w, r, e)
		return
	}
	v, _, e := a.Store.TurnView(r.Context(), ses.ID, r.PathValue("conversation"), x.ID)
	if e != nil {
		a.problem(w, r, e)
		return
	}
	a.accepted(w, r, x, withContract(r, v), "/api/v1/conversations/"+r.PathValue("conversation")+"/turns/"+x.ID)
}
func (a *Server) getTurn(w http.ResponseWriter, r *http.Request) {
	ses, ok := a.auth(w, r)
	if !ok {
		return
	}
	v, _, e := a.Store.TurnView(r.Context(), ses.ID, r.PathValue("conversation"), r.PathValue("turn"))
	if e != nil {
		a.problem(w, r, e)
		return
	}
	if !a.checkContract(w, r, v) {
		return
	}
	a.json(w, 200, withContract(r, v))
}
func (a *Server) cancelTurn(w http.ResponseWriter, r *http.Request) {
	ses, ok := a.auth(w, r)
	if !ok {
		return
	}
	if !a.body(w, r, &struct{}{}) {
		return
	}
	if e := a.Store.CancelTurn(r.Context(), ses.ID, r.PathValue("conversation"), r.PathValue("turn")); e != nil {
		a.problem(w, r, e)
		return
	}
	a.getTurn(w, r)
}
func (a *Server) retryTurn(w http.ResponseWriter, r *http.Request) {
	ses, ok := a.auth(w, r)
	if !ok {
		return
	}
	key, ok := a.key(w, r)
	if !ok {
		return
	}
	var in struct {
		Last *string `json:"expected_last_turn_id"`
	}
	if !a.body(w, r, &in) {
		return
	}
	old, e := a.Store.Turn(r.Context(), ses.ID, r.PathValue("conversation"), r.PathValue("turn"))
	if e != nil {
		a.problem(w, r, e)
		return
	}
	if old.AllowExternalSources && contractVersion(r) < 2 {
		a.problem(w, r, domain.Err(409, "CLIENT_UPDATE_REQUIRED", "本轮网络补充重试需要新版页面。"))
		return
	}
	x, e := a.Store.CreateTurn(r.Context(), ses.ID, r.PathValue("conversation"), key, domain.QuestionInput{ExpectedLastTurnID: in.Last}, r.PathValue("turn"))
	if e != nil {
		a.problem(w, r, e)
		return
	}
	v, _, e := a.Store.TurnView(r.Context(), ses.ID, r.PathValue("conversation"), x.ID)
	if e != nil {
		a.problem(w, r, e)
		return
	}
	a.accepted(w, r, x, withContract(r, v), "/api/v1/conversations/"+r.PathValue("conversation")+"/turns/"+x.ID)
}
func page(r *http.Request) (int, error) {
	v := r.URL.Query().Get("limit")
	if v == "" {
		return 20, nil
	}
	n, e := strconv.Atoi(v)
	if e != nil || n < 1 || n > 100 {
		return 0, domain.Err(400, "INVALID_ARGUMENT", "limit 必须在 1 到 100 之间。")
	}
	return n, nil
}
func (a *Server) turns(w http.ResponseWriter, r *http.Request) {
	ses, ok := a.auth(w, r)
	if !ok {
		return
	}
	n, e := page(r)
	if e != nil {
		a.problem(w, r, e)
		return
	}
	var before int64
	if v := r.URL.Query().Get("before_turn_no"); v != "" {
		before, e = strconv.ParseInt(v, 10, 64)
		if e != nil || before < 1 {
			a.problem(w, r, domain.Err(400, "INVALID_ARGUMENT", "before_turn_no 无效。"))
			return
		}
	}
	v, e := a.Store.TurnPage(r.Context(), ses.ID, r.PathValue("conversation"), before, n)
	if e != nil {
		a.problem(w, r, e)
		return
	}
	if !a.checkContract(w, r, v) {
		return
	}
	a.json(w, 200, v)
}
func (a *Server) activity(w http.ResponseWriter, r *http.Request) {
	ses, ok := a.auth(w, r)
	if !ok {
		return
	}
	n, e := page(r)
	if e != nil {
		a.problem(w, r, e)
		return
	}
	typ, id := "request", r.PathValue("request")
	if r.PathValue("turn") != "" {
		typ, id = "turn", r.PathValue("turn")
	}
	v, e := a.Store.ActivityPage(r.Context(), ses.ID, typ, id, r.PathValue("conversation"), r.URL.Query().Get("before"), n)
	if e != nil {
		a.problem(w, r, e)
		return
	}
	a.json(w, 200, v)
}
func (a *Server) admin(w http.ResponseWriter, r *http.Request) bool {
	return a.adminAuthenticated(w, r)
}
func (a *Server) adminConfig(w http.ResponseWriter, r *http.Request) {
	if !a.admin(w, r) {
		return
	}
	if r.Method == "PUT" {
		var in struct {
			Profile  config.Profile `json:"profile"`
			ModelKey string         `json:"model_api_key"`
			ASRKey   string         `json:"asr_api_key"`
		}
		if !a.body(w, r, &in) {
			return
		}
		if e := in.Profile.Validate(); e != nil {
			a.problem(w, r, domain.Err(422, "INVALID_ARGUMENT", e.Error()))
			return
		}
		if _, e := outbound.Validate(in.Profile.ModelBaseURL, a.Config.ModelAllowedHosts, true); e != nil {
			a.problem(w, r, domain.Err(422, "INVALID_ARGUMENT", "模型端点不符合服务器出站策略。"))
			return
		}
		id, e := a.Store.SaveConfig(r.Context(), in.Profile, in.ModelKey, in.ASRKey)
		if e != nil {
			a.problem(w, r, e)
			return
		}
		stored, e := a.Store.Profile(r.Context(), id)
		if e != nil {
			a.problem(w, r, e)
			return
		}
		a.json(w, 200, map[string]any{"schema_version": 1, "config_revision_id": id, "profile": stored})
		return
	}
	id, p, e := a.Store.CurrentConfig(r.Context())
	if e != nil {
		a.problem(w, r, e)
		return
	}
	mk, e := a.Store.Credential(r.Context(), p.ModelKeyRef)
	if e != nil {
		a.problem(w, r, e)
		return
	}
	ak, e := a.Store.Credential(r.Context(), p.ASRKeyRef)
	if e != nil {
		a.problem(w, r, e)
		return
	}
	a.json(w, 200, map[string]any{"schema_version": 1, "config_revision_id": id, "profile": p, "model_key_configured": mk != "", "asr_key_configured": ak != ""})
}
func (a *Server) adminTest(w http.ResponseWriter, r *http.Request) {
	if !a.admin(w, r) {
		return
	}
	if !a.body(w, r, &struct{}{}) {
		return
	}
	key, ok := a.key(w, r)
	if !ok {
		return
	}
	m, e := a.Store.ImportCurrentModel(r.Context())
	if e != nil {
		a.problem(w, r, e)
		return
	}
	v, e := a.Store.CreateProbe(r.Context(), m.ID, m.RevisionID, "capabilities", key)
	if e != nil {
		a.problem(w, r, e)
		return
	}
	w.Header().Set("Idempotency-Replayed", fmtBool(v.Replayed))
	a.json(w, 202, map[string]any{"schema_version": 1, "id": v.ID, "status_url": "/api/v1/admin/models/" + m.ID + "/probes"})
}
func (a *Server) adminStatus(w http.ResponseWriter, r *http.Request) {
	if !a.admin(w, r) {
		return
	}
	rows, e := a.Store.Pool.Query(r.Context(), "SELECT status,count(*) FROM job_runs GROUP BY status")
	if e != nil {
		a.problem(w, r, e)
		return
	}
	defer rows.Close()
	counts := map[string]int64{}
	for rows.Next() {
		var k string
		var n int64
		if e = rows.Scan(&k, &n); e != nil {
			a.problem(w, r, e)
			return
		}
		counts[k] = n
	}
	var temp, images int64
	e = a.Store.Pool.QueryRow(r.Context(), "SELECT COALESCE(sum(bytes) FILTER(WHERE kind IN ('video','audio')),0),COALESCE(sum(bytes) FILTER(WHERE kind='frame'),0) FROM media_assets WHERE state IN ('ready','deleting')").Scan(&temp, &images)
	if e != nil {
		a.problem(w, r, e)
		return
	}
	a.json(w, 200, map[string]any{"schema_version": 1, "runs": counts, "storage": map[string]any{"media_bytes": temp, "frame_bytes": images}})
}
