package httpapi

import (
	"bytes"
	"net/http"
	"net/url"
	"vidlens/internal/domain"
)

func contractVersion(r *http.Request) int {
	if r.Header.Get("X-VidLens-Contract") == "2" || r.URL.Query().Get("contract") == "2" {
		return 2
	}
	return 1
}
func (a *Server) checkContract(w http.ResponseWriter, r *http.Request, v any) bool {
	if contractVersion(r) < 2 && bytes.Contains(domain.JSON(v), []byte(`"kind":"external_reference"`)) {
		a.problem(w, r, domain.Err(409, "CLIENT_UPDATE_REQUIRED", "此问答包含新版网页引用，请刷新或升级页面。"))
		return false
	}
	return true
}
func withContract(r *http.Request, v map[string]any) map[string]any {
	if contractVersion(r) < 2 {
		return v
	}
	out := map[string]any{}
	for k, x := range v {
		out[k] = x
	}
	if stream, ok := v["stream"].(map[string]any); ok {
		copy := map[string]any{}
		for k, x := range stream {
			copy[k] = x
		}
		if raw, ok := copy["url"].(string); ok {
			u, e := url.Parse(raw)
			if e == nil {
				q := u.Query()
				q.Set("contract", "2")
				u.RawQuery = q.Encode()
				copy["url"] = u.String()
			}
		}
		out["stream"] = copy
	}
	return out
}
func (a *Server) reference(w http.ResponseWriter, r *http.Request) {
	ses, ok := a.auth(w, r)
	if !ok {
		return
	}
	if contractVersion(r) < 2 {
		a.problem(w, r, domain.Err(409, "CLIENT_UPDATE_REQUIRED", "网页引用需要新版客户端。"))
		return
	}
	v, e := a.Store.PublicReference(r.Context(), ses.ID, r.PathValue("request"), r.PathValue("reference"))
	if e != nil {
		a.problem(w, r, e)
		return
	}
	a.json(w, 200, v.Evidence())
}
