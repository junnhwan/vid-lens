package media

import (
	"context"
	"errors"
	"fmt"

	"vidlens/internal/domain"
)

// Reload is an execution-only operation. HTTP image reads never call it.
func (s *Service) Reload(ctx context.Context, c domain.Claim, id, key string) (domain.Asset, []byte, error) {
	a, b, e := s.ReadImage(ctx, c, id)
	if e == nil {
		return a, b, nil
	}
	if !errors.Is(e, ErrMediaMissing) {
		return a, nil, e
	}
	old, e := s.Store.InternalFrame(ctx, c, id)
	if e != nil {
		return old, nil, e
	}
	if _, e = s.Store.Video(ctx, *c.Run.SnapshotID); e != nil {
		return old, nil, ErrMediaMissing
	}
	snap, e := s.Store.Snapshot(ctx, *c.Run.SnapshotID)
	if e != nil {
		return old, nil, e
	}
	if old.RequestedTimeMS == nil {
		return old, nil, ErrMediaMissing
	}
	t := *old.RequestedTimeMS
	w := Window{StartMS: max(int64(0), t-1), EndMS: min(snap.DurationMS, t+1), Count: 1, Mode: "overview"}
	if old.Width > 1280 || old.Height > 1280 {
		w.Mode = "detail"
	}
	if old.Crop != nil {
		parent, _, e := s.Reload(ctx, c, old.Crop.ParentFrameID, key+":parent")
		if e != nil {
			return old, nil, e
		}
		crop := *old.Crop
		crop.ParentFrameID = parent.ID
		w.Crop = &crop
	}
	result, e := s.Inspect(ctx, c, "remat:"+key+":"+id, "恢复已引用的同一来源帧", w, nil)
	if e != nil {
		return old, nil, e
	}
	if len(result.FrameIDs) != 1 {
		return old, nil, fmt.Errorf("invalid rematerialization")
	}
	fresh := result.FrameIDs[0]
	if e = s.Store.MarkReplacement(ctx, c, fresh, id); e != nil {
		return old, nil, e
	}
	a, b, e = s.ReadImage(ctx, c, fresh)
	return a, b, e
}
