package media

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"vidlens/internal/config"
	"vidlens/internal/domain"
	"vidlens/internal/resource"
	"vidlens/internal/store"
)

type Service struct {
	Store     *store.Store
	Resources *resource.Manager
	Config    config.Config
}

func New(s *store.Store, r *resource.Manager, c config.Config) (*Service, error) {
	if e := os.MkdirAll(filepath.Join(c.DataDir, "assets"), 0700); e != nil {
		return nil, e
	}
	if e := os.MkdirAll(filepath.Join(c.DataDir, "work"), 0700); e != nil {
		return nil, e
	}
	return &Service{Store: s, Resources: r, Config: c}, nil
}
func newHash() hash.Hash                       { return sha256.New() }
func hashString(h hash.Hash) string            { return hex.EncodeToString(h.Sum(nil)) }
func stringInt(i int) string                   { return strconv.Itoa(i) }
func fmtSscan(s string, a ...any) (int, error) { return fmt.Sscan(s, a...) }

func fingerprintFile(ctx context.Context, path string) (string, error) {
	f, e := os.Open(path)
	if e != nil {
		return "", e
	}
	defer f.Close()
	h := newHash()
	if _, e = io.Copy(h, &contextReader{ctx: ctx, r: f}); e != nil {
		return "", e
	}
	return hashString(h), nil
}
func (s *Service) Path(key string) (string, error) {
	p := filepath.Join(s.Config.DataDir, filepath.Clean(key))
	if !strings.HasPrefix(p, s.Config.DataDir+string(os.PathSeparator)) {
		return "", errors.New("invalid storage key")
	}
	return p, nil
}
func (s *Service) work(c domain.Claim) (string, error) {
	p := filepath.Join(s.Config.DataDir, "work", c.Run.ID, strconv.FormatInt(c.Epoch, 10))
	return p, os.MkdirAll(p, 0700)
}
func (s *Service) own(ctx context.Context, c domain.Claim, path, kind, mime string, snapshot *string, expiry time.Duration) (domain.Asset, error) {
	id := domain.ID()
	ext := filepath.Ext(path)
	key := filepath.Join("assets", id+ext)
	dst, _ := s.Path(key)
	f, e := os.Open(path)
	if e != nil {
		return domain.Asset{}, e
	}
	h := sha256.New()
	n, e := io.Copy(h, &contextReader{ctx: ctx, r: f})
	f.Close()
	if e != nil {
		return domain.Asset{}, e
	}
	if e = os.Rename(path, dst); e != nil {
		return domain.Asset{}, e
	}
	now := time.Now().UTC()
	a := domain.Asset{ID: id, SnapshotID: snapshot, SourceID: c.Run.SourceID, RunID: c.Run.ID, Kind: kind, StorageKey: key, SHA256: hex.EncodeToString(h.Sum(nil)), MIME: mime, Bytes: n, State: "ready", CreatedAt: now, ExpiresAt: domain.Ptr(now.Add(expiry))}
	return a, nil
}
func (s *Service) ReadImage(ctx context.Context, c domain.Claim, id string) (domain.Asset, []byte, error) {
	a, e := s.Store.InternalFrame(ctx, c, id)
	if e != nil {
		return a, nil, e
	}
	if a.State != "ready" {
		return a, nil, ErrMediaMissing
	}
	if a.ExpiresAt != nil && !a.ExpiresAt.After(time.Now()) {
		pinned, e := s.Store.HasProtection(ctx, a.ID)
		if e != nil {
			return a, nil, e
		}
		if !pinned {
			return a, nil, ErrMediaMissing
		}
	}
	if e = s.Store.Pin(ctx, c, id); e != nil {
		return a, nil, e
	}
	p, e := s.Path(a.StorageKey)
	if e != nil {
		return a, nil, e
	}
	f, e := os.Open(p)
	if e != nil {
		return a, nil, ErrMediaMissing
	}
	defer f.Close()
	b, e := io.ReadAll(io.LimitReader(f, (2<<20)+1))
	if e != nil {
		return a, nil, e
	}
	if len(b) > 2<<20 {
		return a, nil, domain.Err(422, "RESOURCE_LIMIT_EXCEEDED", "图片载荷超过限制。")
	}
	h := sha256.Sum256(b)
	if hex.EncodeToString(h[:]) != a.SHA256 {
		return a, nil, errors.New("image checksum mismatch")
	}
	return a, b, nil
}

var ErrMediaMissing = errors.New("original media is unavailable")
