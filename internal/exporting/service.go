package exporting

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"syscall"
	"time"
	"vidlens/internal/config"
	"vidlens/internal/domain"
	"vidlens/internal/store"
)

type Service struct {
	Store  *store.Store
	Config config.Config
}
type payload struct {
	Path   string `json:"path"`
	Bytes  int64  `json:"bytes"`
	SHA256 string `json:"sha256"`
}
type boundedWriter struct {
	w   io.Writer
	n   int64
	ctx context.Context
}

func (w *boundedWriter) Write(b []byte) (int, error) {
	if e := w.ctx.Err(); e != nil {
		return 0, e
	}
	if w.n+int64(len(b)) > store.ExportMaxBytes {
		return 0, domain.Err(422, "EXPORT_LIMIT_EXCEEDED", "导出文件超过 64 MiB。")
	}
	n, e := w.w.Write(b)
	w.n += int64(n)
	return n, e
}
func hash(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func (s *Service) Run(ctx context.Context, v store.Export) error {
	start := time.Now()
	ctx, cancel := context.WithDeadline(ctx, minTime(v.DeadlineAt, start.Add(time.Duration(120000-v.ActiveMS)*time.Millisecond)))
	defer cancel()
	key, sha := "", ""
	var size int64
	err := func() error {
		var disk syscall.Statfs_t
		if e := syscall.Statfs(s.Config.DataDir, &disk); e != nil {
			return e
		}
		available, total := int64(disk.Bavail)*int64(disk.Bsize), int64(disk.Blocks)*int64(disk.Bsize)
		allowed, e := s.Store.DiskSpaceAllowed(ctx, available, total, store.ExportMaxBytes)
		if e != nil {
			return e
		}
		if !allowed {
			return domain.Err(422, "EXPORT_LIMIT_EXCEEDED", "当前磁盘空间不足以准备导出。")
		}
		r, e := s.Store.Request(ctx, v.SessionID, v.RequestID)
		if e != nil {
			return e
		}
		a, e := s.Store.PublishedArticle(ctx, v.ArticleID)
		if e != nil {
			return e
		}
		model := domain.GenerationModel{}
		purpose := ""
		if r.GenerationSpec != nil {
			model = r.GenerationSpec.Model
			purpose = r.GenerationSpec.Purpose
		}
		dir := filepath.Join(s.Config.DataDir, "exports")
		if e = os.MkdirAll(dir, 0700); e != nil {
			return e
		}
		f, e := os.CreateTemp(dir, ".export-*")
		if e != nil {
			return e
		}
		tmp := f.Name()
		defer os.Remove(tmp)
		defer f.Close()
		digest := sha256.New()
		w := &boundedWriter{w: io.MultiWriter(f, digest), ctx: ctx}
		var imagePaths map[string]string
		ids := store.ContentFrameIDs(a.Sections, a.Citations)
		if v.Format == "markdown_zip" {
			imagePaths = map[string]string{}
			for i, id := range ids {
				asset, e := s.Store.Asset(ctx, id)
				if e != nil {
					return e
				}
				ext := ""
				switch asset.MIME {
				case "image/jpeg":
					ext = ".jpg"
				case "image/png":
					ext = ".png"
				case "image/webp":
					ext = ".webp"
				default:
					return domain.Err(409, "EXPORT_ASSET_UNAVAILABLE", "配图格式不支持离线导出。")
				}
				imagePaths[id] = fmt.Sprintf("images/frame-%03d%s", i+1, ext)
			}
		}
		md, e := markdownVersion(a, model, purpose, imagePaths, v.RendererVersion)
		if e != nil {
			return e
		}
		if v.Format == "markdown" {
			if _, e = w.Write(md); e != nil {
				return e
			}
		} else {
			z := zip.NewWriter(w)
			entries := []payload{}
			var totalPayload int64
			write := func(name string, b []byte) error {
				if len(entries)+1 >= store.ExportMaxEntries || totalPayload+int64(len(b)) > store.ExportMaxBytes {
					return domain.Err(422, "EXPORT_LIMIT_EXCEEDED", "图文载荷超过限制。")
				}
				if filepath.IsAbs(name) || strings.Contains(name, "..") || strings.Contains(name, "\\") {
					return errors.New("unsafe ZIP path")
				}
				h := &zip.FileHeader{Name: name, Method: zip.Store}
				h.SetMode(0600)
				h.Modified = time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
				entry, e := z.CreateHeader(h)
				if e != nil {
					return e
				}
				if _, e = entry.Write(b); e != nil {
					return e
				}
				totalPayload += int64(len(b))
				entries = append(entries, payload{name, int64(len(b)), hash(b)})
				return nil
			}
			if e = write("article.md", md); e != nil {
				return e
			}
			frameMeta := []map[string]any{}
			for _, id := range ids {
				if e = ctx.Err(); e != nil {
					return e
				}
				asset, e := s.Store.Asset(ctx, id)
				if e != nil {
					return e
				}
				if asset.State != "ready" || asset.Bytes > 2<<20 {
					return domain.Err(409, "EXPORT_ASSET_UNAVAILABLE", "正式配图暂不可用。")
				}
				path := filepath.Join(s.Config.DataDir, filepath.Clean(asset.StorageKey))
				if !strings.HasPrefix(path, s.Config.DataDir+string(os.PathSeparator)) {
					return errors.New("invalid image storage key")
				}
				image, e := os.Open(path)
				if e != nil {
					return domain.Err(409, "EXPORT_ASSET_UNAVAILABLE", "缺少正式配图。")
				}
				b, e := io.ReadAll(io.LimitReader(image, (2<<20)+1))
				image.Close()
				if e != nil {
					return e
				}
				if int64(len(b)) != asset.Bytes || hash(b) != asset.SHA256 {
					return domain.Err(409, "EXPORT_ASSET_UNAVAILABLE", "配图校验未通过。")
				}
				if e = write(imagePaths[id], b); e != nil {
					return e
				}
				frameMeta = append(frameMeta, map[string]any{"file": imagePaths[id], "time_ms": asset.TimeMS, "requested_time_ms": asset.RequestedTimeMS, "crop": asset.Crop, "sha256": asset.SHA256})
			}
			sources, e := json.MarshalIndent(map[string]any{"source": a.Source, "citations": a.Citations, "frames": frameMeta, "generation_model": model, "published_at": a.PublishedAt}, "", "  ")
			if e != nil {
				return e
			}
			if e = write("sources.json", sources); e != nil {
				return e
			}
			manifest, e := json.MarshalIndent(map[string]any{"format_version": 1, "renderer_version": v.RendererVersion, "article_content_sha256": hash(domain.JSON(a)), "files": entries}, "", "  ")
			if e != nil {
				return e
			}
			entry, e := z.CreateHeader(&zip.FileHeader{Name: "manifest.json", Method: zip.Store})
			if e != nil {
				return e
			}
			if totalPayload+int64(len(manifest)) > store.ExportMaxBytes {
				return domain.Err(422, "EXPORT_LIMIT_EXCEEDED", "图文载荷超过限制。")
			}
			if _, e = entry.Write(manifest); e != nil {
				return e
			}
			if e = z.Close(); e != nil {
				return e
			}
		}
		if e = f.Sync(); e != nil {
			return e
		}
		if e = f.Close(); e != nil {
			return e
		}
		ext := ".md"
		if v.Format == "markdown_zip" {
			ext = ".zip"
		}
		key = filepath.Join("exports", v.ID+"-"+fmt.Sprint(v.Epoch)+ext)
		if e = os.Rename(tmp, filepath.Join(s.Config.DataDir, key)); e != nil {
			return e
		}
		size = w.n
		sha = hex.EncodeToString(digest.Sum(nil))
		return nil
	}()
	var failure *domain.Failure
	retry := false
	if err != nil {
		code, message := "EXPORT_FILE_UNAVAILABLE", "导出文件暂时无法准备。"
		var p *domain.Problem
		if errors.As(err, &p) {
			code, message = p.Code, p.Detail
		} else {
			retry = true
		}
		failure = &domain.Failure{Code: code, Message: message, CanRetry: true}
	}
	c, cc := context.WithTimeout(context.Background(), 3*time.Second)
	defer cc()
	saved := s.Store.FinishExport(c, v, key, sha, size, failure, retry, time.Since(start))
	if saved != nil && key != "" {
		_ = os.Remove(filepath.Join(s.Config.DataDir, key))
	}
	return saved
}
func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}
func (s *Service) Cleanup(ctx context.Context) error {
	files, e := s.Store.ExpiredExportFiles(ctx)
	if e != nil {
		return e
	}
	for _, v := range files {
		if v.StorageKey == nil {
			continue
		}
		path := filepath.Join(s.Config.DataDir, filepath.Clean(*v.StorageKey))
		if !strings.HasPrefix(path, filepath.Join(s.Config.DataDir, "exports")+string(os.PathSeparator)) {
			return errors.New("invalid export storage key")
		}
		e = os.Remove(path)
		if e == nil || errors.Is(e, os.ErrNotExist) {
			if e = s.Store.ExportFileDeleted(ctx, v.ID); e != nil {
				return e
			}
		}
	}
	// Crash-created unpublished temporary and epoch files are reclaimed after
	// the absolute job deadline. Only validated filenames in this directory.
	dir := filepath.Join(s.Config.DataDir, "exports")
	entries, e := os.ReadDir(dir)
	if os.IsNotExist(e) {
		return nil
	}
	if e != nil {
		return e
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		st, e := entry.Info()
		if e == nil && time.Since(st.ModTime()) > 20*time.Minute {
			if strings.HasPrefix(entry.Name(), ".export-") {
				_ = os.Remove(filepath.Join(dir, entry.Name()))
				continue
			}
			match := regexp.MustCompile(`^([a-f0-9]{32})-[0-9]+\.(md|zip)$`).FindStringSubmatch(entry.Name())
			if match == nil {
				continue
			}
			protected, err := s.Store.ExportFileProtected(ctx, filepath.Join("exports", entry.Name()), match[1])
			if err != nil {
				return err
			}
			if !protected {
				_ = os.Remove(filepath.Join(dir, entry.Name()))
			}
		}
	}
	return nil
}
