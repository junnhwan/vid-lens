package investigator

import (
	"context"
	"slices"
	"strings"

	"vidlens/internal/domain"
	"vidlens/internal/store"
)

// The reviewer and publication guard must resolve the same exact candidate and
// original source identities. Recomputing this reads metadata, never pixels or a
// provider; a resumed publication cannot silently approve missing review work.
func (s *session) contentReviewIdentity(ctx context.Context, candidate any, citations map[string]domain.Citation) (string, map[string]domain.Citation, []domain.Asset, map[string]any, error) {
	catalog, err := s.citationCatalog(ctx)
	if err != nil {
		return "", nil, nil, nil, err
	}
	for id, c := range citations {
		catalog[id] = c // Already checked against the owned original source.
	}
	frames := []domain.Asset{}
	seen := map[string]bool{}
	for _, c := range citations {
		if c.Kind != "video_frame" || seen[c.FrameID] {
			continue
		}
		seen[c.FrameID] = true
		a, e := s.engine.Store.InternalFrame(ctx, s.claim, c.FrameID)
		if e != nil {
			return "", nil, nil, nil, e
		}
		frames = append(frames, a)
	}
	slices.SortFunc(frames, func(a, b domain.Asset) int {
		at, bt := int64(-1), int64(-1)
		if a.TimeMS != nil {
			at = *a.TimeMS
		}
		if b.TimeMS != nil {
			bt = *b.TimeMS
		}
		if at < bt {
			return -1
		}
		if at > bt {
			return 1
		}
		return strings.Compare(a.ID, b.ID)
	})
	identities := []any{}
	for _, a := range frames {
		identities = append(identities, map[string]any{"id": a.ID, "sha256": a.SHA256, "time_ms": a.TimeMS})
	}
	reviewProtocol := evidenceReviewProtocol
	if s.usesBoundedReview() {
		reviewProtocol = s.boundedReviewProtocol()
	}
	identity := map[string]any{"review_protocol": reviewProtocol, "candidate": candidate, "citations": citations, "snapshot_id": s.snapshot.ID, "text_hash": s.snapshot.TextHash, "video_hash": s.snapshot.VideoHash, "frames": identities}
	if s.usesModalityReview() {
		scope, err := s.reviewExecutionContext(ctx)
		if err != nil {
			return "", nil, nil, nil, err
		}
		identity["execution_context"] = scope
	}
	hash := store.Hash(identity)
	hints := map[string]any{}
	for id, c := range catalog {
		if c.Kind == "video_text" {
			for i, t := range s.transcript {
				if slices.Contains(c.SegmentIDs, t.ID) {
					hints[id] = map[string]any{"transcript_index": i, "range": c.Range, "text_origin": t.Origin}
					break
				}
			}
		} else {
			hints[id] = c
		}
	}
	return hash, catalog, frames, hints, nil
}
