package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
	"vidlens/internal/domain"
	"vidlens/internal/store"
)

func (a *Server) exports(w http.ResponseWriter, r *http.Request) {
	s, ok := a.auth(w, r)
	if !ok {
		return
	}
	request, id := r.PathValue("request"), r.PathValue("export")
	if r.Method == "POST" {
		key, ok := a.key(w, r)
		if !ok {
			return
		}
		var in struct {
			Format string `json:"format"`
		}
		if !a.body(w, r, &in) {
			return
		}
		var disk syscall.Statfs_t
		if e := syscall.Statfs(a.Config.DataDir, &disk); e != nil {
			a.problem(w, r, e)
			return
		}
		x, reused, e := a.Store.CreateExport(r.Context(), s.ID, request, in.Format, key, store.DiskSpace{Available: int64(disk.Bavail) * int64(disk.Bsize), Total: int64(disk.Blocks) * int64(disk.Bsize), StopPercent: a.Config.DiskStopPercent})
		if e != nil {
			a.problem(w, r, e)
			return
		}
		v, e := a.Store.Export(r.Context(), s.ID, request, x.ID)
		if e != nil {
			a.problem(w, r, e)
			return
		}
		w.Header().Set("Export-Reused", fmtBool(reused))
		status := 201
		if x.Replayed || reused {
			status = 200
		}
		w.Header().Set("Idempotency-Replayed", fmtBool(x.Replayed))
		w.Header().Set("Idempotency-Expires-At", x.Expires.Format(time.RFC3339))
		w.Header().Set("Location", "/api/v1/requests/"+request+"/exports/"+x.ID)
		a.json(w, status, store.ExportView(&v))
		return
	}
	if id == "" {
		v, e := a.Store.LatestExport(r.Context(), s.ID, request, r.URL.Query().Get("format"))
		if e != nil {
			a.problem(w, r, e)
			return
		}
		a.json(w, 200, store.ExportView(v))
		return
	}
	v, e := a.Store.Export(r.Context(), s.ID, request, id)
	if e != nil {
		a.problem(w, r, e)
		return
	}
	a.json(w, 200, store.ExportView(&v))
}
func (a *Server) exportContent(w http.ResponseWriter, r *http.Request) {
	s, ok := a.auth(w, r)
	if !ok {
		return
	}
	v, e := a.Store.Export(r.Context(), s.ID, r.PathValue("request"), r.PathValue("export"))
	if e != nil {
		a.problem(w, r, e)
		return
	}
	if v.Status == "expired" {
		a.problem(w, r, domain.Err(410, "EXPORT_EXPIRED", "导出已过期，请重新创建。"))
		return
	}
	if v.Status == "unavailable" {
		a.problem(w, r, domain.Err(410, "EXPORT_FILE_UNAVAILABLE", "导出副本暂不可读，请重新创建。"))
		return
	}
	if v.Status != "ready" || v.StorageKey == nil {
		a.problem(w, r, domain.Err(409, "EXPORT_NOT_READY", "导出还未准备完成。"))
		return
	}
	lease, e := a.Store.ExportDownload(r.Context(), v)
	if e != nil {
		a.problem(w, r, e)
		return
	}
	defer func() {
		c, cc := context.WithTimeout(context.Background(), 2*time.Second)
		defer cc()
		_ = a.Store.ReleaseDownload(c, lease)
	}()
	path := filepath.Join(a.Config.DataDir, filepath.Clean(*v.StorageKey))
	if !strings.HasPrefix(path, filepath.Join(a.Config.DataDir, "exports")+string(os.PathSeparator)) {
		a.problem(w, r, domain.Err(503, "SERVICE_UNAVAILABLE", "导出文件不可读。"))
		return
	}
	f, e := os.Open(path)
	valid := e == nil
	if valid {
		defer f.Close()
		stat, se := f.Stat()
		valid = se == nil && v.Bytes != nil && stat.Size() == *v.Bytes && stat.Size() <= store.ExportMaxBytes
	}
	if valid {
		h := sha256.New()
		_, e = io.Copy(h, io.LimitReader(f, store.ExportMaxBytes+1))
		valid = e == nil && v.SHA256 != nil && hex.EncodeToString(h.Sum(nil)) == *v.SHA256
		_, _ = f.Seek(0, io.SeekStart)
	}
	if !valid {
		_ = a.Store.ExportUnavailable(r.Context(), v.ID)
		a.problem(w, r, domain.Err(410, "EXPORT_FILE_UNAVAILABLE", "导出副本丢失或未通过校验，请重新创建。"))
		return
	}
	_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(5 * time.Minute))
	mime, ext := "text/markdown; charset=utf-8", ".md"
	if v.Format == "markdown_zip" {
		mime, ext = "application/zip", ".zip"
	}
	w.Header().Set("Content-Type", mime)
	w.Header().Set("Content-Disposition", `attachment; filename="vidlens-`+v.ID+ext+`"`)
	w.Header().Set("Content-Length", strconv.FormatInt(*v.Bytes, 10))
	w.Header().Set("Accept-Ranges", "none")
	_, _ = io.Copy(w, f)
}
