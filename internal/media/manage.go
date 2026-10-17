package media

import (
	"context"
	"errors"
	"os"

	"vidlens/internal/domain"
)

type DeleteResult struct {
	DeletedBytes int64 `json:"deleted_bytes"`
	DeletedFiles int   `json:"deleted_files"`
	PendingFiles int   `json:"pending_files"`
}

// Mark-before-unlink is shared with the janitor. A failed unlink remains charged
// and retryable; successful deletion never changes articles or their frame refs.
func (s *Service) DeleteVideo(ctx context.Context, source string, expected []string) (DeleteResult, error) {
	var result DeleteResult
	release, ok, e := s.Store.DeleteLease(ctx)
	if e != nil {
		return result, e
	}
	if !ok {
		return result, domain.Err(409, "STORAGE_BUSY", "素材正在整理，请稍后重新读取列表。")
	}
	defer release()
	assets, e := s.Store.MarkVideoDeleting(ctx, source, expected)
	if e != nil {
		return result, e
	}
	for _, a := range assets {
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		path, e := s.Path(a.StorageKey)
		if e != nil {
			return result, e
		}
		e = os.Remove(path)
		success := e == nil || errors.Is(e, os.ErrNotExist)
		if e = s.Store.Deleted(ctx, a.ID, success); e != nil {
			return result, e
		}
		if success {
			result.DeletedBytes += a.Bytes
			result.DeletedFiles++
		} else {
			result.PendingFiles++
		}
	}
	return result, nil
}
