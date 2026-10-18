package store

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"vidlens/internal/domain"
)

func TestPreparedSourceAdmissionIncludesNewArticleWork(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	owner := session(t, s)
	prepared(t, s, owner)
	var last Accepted
	for i := 0; i < maxPendingVideoTasks-1; i++ {
		var err error
		last, err = s.CreateRequestWithInput(ctx, owner, domain.ID(), domain.RequestInput{VideoURL: "https://youtube.com/watch?v=abcdefghijk", Purpose: fmt.Sprintf("bounded article %d", i)}, "", false)
		if err != nil {
			t.Fatalf("request %d: %v", i, err)
		}
	}
	var before int
	if err := s.Pool.QueryRow(ctx, "SELECT count(*) FROM generation_requests").Scan(&before); err != nil {
		t.Fatal(err)
	}
	_, err := s.CreateRequestWithInput(ctx, owner, domain.ID(), domain.RequestInput{VideoURL: "https://youtube.com/watch?v=abcdefghijk", Purpose: "one more independent article"}, "", false)
	var problem *domain.Problem
	if !errors.As(err, &problem) || problem.Code != "ADMISSION_LIMITED" {
		t.Fatalf("new article exceeded admission bound: %v", err)
	}
	var pending, after int
	if err := s.Pool.QueryRow(ctx, "SELECT count(*) FROM job_runs WHERE kind<>'qa.answer' AND status IN ('queued','running','retry_wait','waiting_dependency','cancel_requested')").Scan(&pending); err != nil {
		t.Fatal(err)
	}
	if err := s.Pool.QueryRow(ctx, "SELECT count(*) FROM generation_requests").Scan(&after); err != nil {
		t.Fatal(err)
	}
	if pending != maxPendingVideoTasks || after != before {
		t.Fatalf("rejected creation left state: pending=%d requests=%d->%d", pending, before, after)
	}
	// Joining the exact already admitted work consumes no additional capacity.
	joined, err := s.CreateRequestWithInput(ctx, owner, domain.ID(), domain.RequestInput{VideoURL: "https://youtube.com/watch?v=abcdefghijk", Purpose: fmt.Sprintf("bounded article %d", maxPendingVideoTasks-2)}, "", false)
	if err != nil {
		t.Fatal("existing work was rejected at capacity", err)
	}
	a, _ := s.Request(ctx, owner, last.ID)
	b, _ := s.Request(ctx, owner, joined.ID)
	if a.ActiveRunID == nil || b.ActiveRunID == nil || *a.ActiveRunID != *b.ActiveRunID {
		t.Fatal("admission created duplicate shared work")
	}
}

func TestPreparingSourceReservesDistinctArticleAdmissions(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	owner := session(t, s)
	var first Accepted
	for i := 0; i < maxPendingVideoTasks; i++ {
		accepted, err := s.CreateRequestWithInput(ctx, owner, domain.ID(), domain.RequestInput{VideoURL: "https://youtube.com/watch?v=abcdefghijk", Purpose: fmt.Sprintf("future article %d", i)}, "", false)
		if err != nil {
			t.Fatalf("request %d: %v", i, err)
		}
		if i == 0 {
			first = accepted
		}
	}
	_, err := s.CreateRequestWithInput(ctx, owner, domain.ID(), domain.RequestInput{VideoURL: "https://youtube.com/watch?v=abcdefghijk", Purpose: "excess future article"}, "", false)
	var problem *domain.Problem
	if !errors.As(err, &problem) || problem.Code != "ADMISSION_LIMITED" {
		t.Fatalf("source subscribers bypassed future article admission: %v", err)
	}
	if _, err = s.CreateRequestWithInput(ctx, owner, domain.ID(), domain.RequestInput{VideoURL: "https://youtube.com/watch?v=abcdefghijk", Purpose: "future article 0"}, "", false); err != nil {
		t.Fatal("same future article must remain joinable", err)
	}
	request, err := s.Request(ctx, owner, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	claim, err := s.Claim(ctx, *request.ActiveRunID, 1, "shared-source")
	if err != nil || claim == nil {
		t.Fatal(err)
	}
	video := domain.Asset{ID: domain.ID(), SourceID: claim.Run.SourceID, RunID: claim.Run.ID, Kind: "video", StorageKey: domain.ID() + ".mp4", SHA256: "fixture", MIME: "video/mp4", Bytes: 42}
	if err := s.AddAsset(ctx, *claim, video); err != nil {
		t.Fatal(err)
	}
	snapshot := domain.Snapshot{ID: domain.ID(), SourceID: claim.Run.SourceID, VideoHash: "fixture", TextHash: "text", DurationMS: 1000, Metadata: []byte(`{}`)}
	if _, err := s.FinishSource(ctx, *claim, snapshot, []domain.Segment{{Ordinal: 0, StartMS: 0, EndMS: 1000, Text: "source", Origin: "platform"}}, video.ID); err != nil {
		t.Fatal("admitted subscribers could not continue at the limit", err)
	}
	var articles int
	if err := s.Pool.QueryRow(ctx, "SELECT count(*) FROM job_runs WHERE kind='article.generate'").Scan(&articles); err != nil || articles != maxPendingVideoTasks {
		t.Fatalf("source fan-out exceeded or lost reservations: %d %v", articles, err)
	}
}
