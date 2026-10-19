package httpapi

import (
	"net/http"
	"strings"
	"syscall"
	"unicode/utf8"

	"vidlens/internal/domain"
	"vidlens/internal/media"
	"vidlens/internal/store"
)

func (a *Server) storageSettings(w http.ResponseWriter, r *http.Request) {
	if !a.admin(w, r) {
		return
	}
	_, profile, e := a.Store.CurrentConfig(r.Context())
	if e != nil {
		a.problem(w, r, e)
		return
	}
	var disk syscall.Statfs_t
	if e = syscall.Statfs(a.Config.DataDir, &disk); e != nil {
		a.problem(w, r, domain.Err(503, "SERVICE_UNAVAILABLE", "主机磁盘状态暂时读不到。"))
		return
	}
	if disk.Blocks == 0 || disk.Bsize <= 0 {
		a.problem(w, r, domain.Err(503, "SERVICE_UNAVAILABLE", "主机磁盘状态暂时读不到。"))
		return
	}
	if r.Method == "PUT" {
		var in store.StorageInput
		if !a.body(w, r, &in) {
			return
		}
		if e = a.Store.SaveStorageSettings(r.Context(), in, profile.WorkspaceBytes); e != nil {
			a.problem(w, r, e)
			return
		}
		_, profile, e = a.Store.CurrentConfig(r.Context())
		if e != nil {
			a.problem(w, r, e)
			return
		}
	}
	available := int64(disk.Bavail) * int64(disk.Bsize)
	total := int64(disk.Blocks) * int64(disk.Bsize)
	v, e := a.Store.StorageOverviewWithAdmission(r.Context(), available, total, profile.WorkspaceBytes)
	if e != nil {
		a.problem(w, r, e)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
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
