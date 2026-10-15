package media

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

func (s *Service) Cleanup(ctx context.Context) error {
	release, ok, e := s.Store.DeleteLease(ctx)
	if e != nil || !ok {
		return e
	}
	defer release()
	var st syscall.Statfs_t
	if e = syscall.Statfs(s.Config.DataDir, &st); e != nil {
		return e
	}
	pressure := int64(st.Blocks-st.Bavail)*100/int64(st.Blocks) >= 80
	assets, e := s.Store.CleanupAssets(ctx, pressure)
	if e != nil {
		return e
	}
	deleteFailed := false
	for _, a := range assets {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		path, e := s.Path(a.StorageKey)
		if e != nil {
			return e
		}
		e = os.Remove(path)
		success := e == nil || errors.Is(e, os.ErrNotExist)
		if !success {
			deleteFailed = true
		}
		if e = s.Store.Deleted(ctx, a.ID, success); e != nil {
			return e
		}
	}
	if e = s.Store.CleanupMetadata(ctx); e != nil {
		return e
	}
	known, e := s.Store.KnownStorageKeys(ctx)
	if e != nil {
		return e
	}
	count := len(assets)
	for _, root := range []string{"work", "assets"} {
		e = filepath.WalkDir(filepath.Join(s.Config.DataDir, root), func(path string, d os.DirEntry, e error) error {
			if e != nil {
				return e
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if count >= 100 {
				return filepath.SkipAll
			}
			if d.IsDir() {
				return nil
			}
			relative, _ := filepath.Rel(s.Config.DataDir, path)
			if known[relative] {
				return nil
			}
			st, e := d.Info()
			if e != nil {
				return e
			}
			if time.Since(st.ModTime()) < 2*time.Hour {
				return nil
			}
			if root == "work" {
				parts := strings.Split(relative, string(os.PathSeparator))
				if len(parts) > 1 {
					protected, e := s.Store.ProtectedRun(ctx, parts[1])
					if e != nil {
						return e
					}
					if protected {
						return nil
					}
				}
			}
			if e = os.Remove(path); e != nil && !errors.Is(e, os.ErrNotExist) {
				return e
			}
			count++
			return nil
		})
		if e != nil {
			return e
		}
	}
	if deleteFailed {
		return errors.New("asset deletion will retry")
	}
	return nil
}
