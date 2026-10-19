package httpapi

import (
	"net/http"
	"strings"
	"unicode/utf8"

	"vidlens/internal/domain"
	"vidlens/internal/media"
	"vidlens/internal/store"
)

func (a *Server) storageSettings(w http.ResponseWriter, r *http.Request) {
	if !a.admin(w, r) {
		return
	}
	if r.Method == "PUT" {
		var in store.StorageInput
		if !a.body(w, r, &in) {
			return
		}
		_, p, e := a.Store.CurrentConfig(r.Context())
		if e != nil {
			a.problem(w, r, e)
			return
		}
		if e = a.Store.SaveStorageSettings(r.Context(), in, p.WorkspaceBytes); e != nil {
			a.problem(w, r, e)
			return
		}
	}
	v, e := a.Store.StorageOverview(r.Context())
	if e != nil {
		a.problem(w, r, e)
		return
	}
	a.json(w, 200, v)
}

func (a *Server) managedVideos(w http.ResponseWriter, r *http.Request) {
	if !a.admin(w, r) {
		return
	}
	limit, offset, e := diagnosticsPage(r)
	if e != nil {
		a.problem(w, r, e)
		return
	}
	search := strings.TrimSpace(r.URL.Query().Get("q"))
	if utf8.RuneCountInString(search) > 200 {
		a.problem(w, r, domain.Err(422, "INVALID_ARGUMENT", "搜索内容最多 200 字。"))
		return
	}
	cleanup := strings.TrimSpace(r.URL.Query().Get("cleanup"))
	v, total, counts, e := a.Store.ManagedVideos(r.Context(), search, cleanup, limit, offset)
	if e != nil {
		a.problem(w, r, e)
		return
	}
	a.json(w, 200, map[string]any{"items": v, "total": total, "limit": limit, "offset": offset, "counts": counts})
}

func (a *Server) deleteVideo(w http.ResponseWriter, r *http.Request) {
	if !a.admin(w, r) {
		return
	}
	var in struct {
		AssetIDs []string `json:"asset_ids"`
	}
	if !a.body(w, r, &in) {
		return
	}
	s := &media.Service{Store: a.Store, Config: a.Config}
	v, e := s.DeleteVideo(r.Context(), r.PathValue("source"), in.AssetIDs)
	if e != nil {
		a.problem(w, r, e)
		return
	}
	status := 200
	if v.PendingFiles > 0 {
		status = 202
	}
	a.json(w, status, v)
}
