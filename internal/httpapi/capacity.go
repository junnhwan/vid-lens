package httpapi

import (
	"net/http"
	"syscall"

	"vidlens/internal/domain"
)

func (a *Server) capacity(w http.ResponseWriter, r *http.Request) {
	var disk syscall.Statfs_t
	if e := syscall.Statfs(a.Config.DataDir, &disk); e != nil {
		a.problem(w, r, domain.Err(503, "SERVICE_UNAVAILABLE", "磁盘状态暂时读不到。"))
		return
	}
	available := int64(disk.Bavail) * int64(disk.Bsize)
	total := int64(disk.Blocks) * int64(disk.Bsize)
	_, p, e := a.Store.CurrentConfig(r.Context())
	if e != nil {
		a.problem(w, r, e)
		return
	}
	v, e := a.Store.DiskCapacity(r.Context(), available, total, a.Config.DiskStopPercent, p.WorkspaceBytes)
	if e != nil {
		a.problem(w, r, e)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	a.json(w, 200, v)
}
