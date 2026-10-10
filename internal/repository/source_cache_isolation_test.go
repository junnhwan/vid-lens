package repository

import (
	"context"
	"testing"

	"vid-lens/internal/model"
)

func cacheTask(t *testing.T, repos *Repositories, user int64, fingerprint, source, intent string) model.VideoTask {
	t.Helper()
	row := model.VideoTask{UserID: user, FileMD5: fingerprint, Filename: "lesson.mp4", Status: model.TaskStatusCompleted, ActiveTextSourceID: source, ProcessingIntentJSON: intent}
	if err := repos.Task.Create(&row); err != nil {
		t.Fatal(err)
	}
	return row
}

func TestSourceCacheIsolationPreservesDirectAndLegacyResults(t *testing.T) {
	repos := NewRepositories(newDedupTestDB(t))
	fingerprint := "private-and-legacy"
	private := cacheTask(t, repos, 8, fingerprint, "source-a", "{}")
	pending := cacheTask(t, repos, 7, fingerprint, "", "{}")
	sourceOnly := cacheTask(t, repos, 7, fingerprint, "source-b", "")
	legacy := cacheTask(t, repos, 7, fingerprint, "", "")
	old := cacheTask(t, repos, 9, fingerprint, "", "")
	if err := repos.Transcription.Create(&model.VideoTranscription{TaskID: private.ID, FileMD5: fingerprint, Content: "private text", SourceID: "source-a", SourceKind: "subtitle", SourceDigest: "digest-a"}); err != nil {
		t.Fatal(err)
	}
	if err := repos.Summary.Create(&model.AISummary{TaskID: private.ID, FileMD5: fingerprint, Content: "private summary", SourceID: "source-a", DocumentJSON: "{}", SchemaVersion: "summary-v2"}); err != nil {
		t.Fatal(err)
	}
	// Private source exists first: legacy cache lookup must not find it.
	if got, err := repos.Transcription.FindByMD5(fingerprint); err != nil || got != nil {
		t.Fatalf("private transcription escaped: %+v %v", got, err)
	}
	if got, err := repos.Summary.FindByMD5(fingerprint); err != nil || got != nil {
		t.Fatalf("private summary escaped: %+v %v", got, err)
	}
	assertPresence := func(wantLegacy bool) {
		t.Helper()
		texts, summaries, err := repos.Task.ResultPresenceByTaskIDs([]model.VideoTask{private, pending, sourceOnly, legacy})
		if err != nil {
			t.Fatal(err)
		}
		if !texts[private.ID] || !summaries[private.ID] || texts[pending.ID] || summaries[pending.ID] || texts[sourceOnly.ID] || summaries[sourceOnly.ID] || texts[legacy.ID] != wantLegacy || summaries[legacy.ID] != wantLegacy {
			t.Fatalf("text=%v summary=%v", texts, summaries)
		}
	}
	assertPresence(false)
	if err := repos.Transcription.Create(&model.VideoTranscription{TaskID: old.ID, FileMD5: fingerprint, Content: "legacy text"}); err != nil {
		t.Fatal(err)
	}
	if err := repos.Summary.Create(&model.AISummary{TaskID: old.ID, FileMD5: fingerprint, Content: "legacy summary"}); err != nil {
		t.Fatal(err)
	}
	assertPresence(true)
	if got, err := repos.Transcription.FindByMD5(fingerprint); err != nil || got == nil || got.TaskID != old.ID {
		t.Fatalf("legacy transcription=%+v %v", got, err)
	}
	if got, err := repos.Summary.FindByMD5(fingerprint); err != nil || got == nil || got.TaskID != old.ID {
		t.Fatalf("legacy summary=%+v %v", got, err)
	}
	nav, err := repos.Summary.ListNavigationSummaries(context.Background(), 7, []int64{pending.ID, sourceOnly.ID, legacy.ID, private.ID})
	if err != nil {
		t.Fatal(err)
	}
	if nav[legacy.ID] != "legacy summary" || nav[pending.ID] != "" || nav[sourceOnly.ID] != "" || nav[private.ID] != "" {
		t.Fatalf("navigation=%v", nav)
	}
	own, err := repos.Summary.ListNavigationSummaries(context.Background(), 8, []int64{private.ID})
	if err != nil || own[private.ID] != "private summary" {
		t.Fatalf("owner navigation=%v %v", own, err)
	}
	ready, total, err := repos.Task.ListByUserID(7, 1, 10, "", "ready")
	if err != nil || total != 1 || len(ready) != 1 || ready[0].ID != legacy.ID {
		t.Fatalf("ready=%+v total=%d err=%v", ready, total, err)
	}
	rows, _, err := repos.Task.ListByUserID(7, 1, 10, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if row.ID == sourceOnly.ID && (row.ActiveTextSourceID != "source-b" || LegacyResultReuseAllowed(&row)) {
			t.Fatalf("active source lost from projection: %+v", row)
		}
		if row.ID == pending.ID && (row.ProcessingIntentJSON != "{}" || LegacyResultReuseAllowed(&row)) {
			t.Fatalf("intent lost from projection: %+v", row)
		}
	}
}

func TestSourceCacheIsolationNeverRehomesPrivateSummary(t *testing.T) {
	repos := NewRepositories(newDedupTestDB(t))
	for _, kind := range []string{"source", "document", "generation"} {
		origin := cacheTask(t, repos, 8, kind, "source-a", "{}")
		successor := cacheTask(t, repos, 7, kind, "", "")
		row := model.AISummary{TaskID: origin.ID, FileMD5: kind, Content: "private"}
		switch kind {
		case "source":
			row.SourceID = "source-a"
		case "document":
			row.DocumentJSON = "{}"
		case "generation":
			row.GenerationID = "generation-a"
		}
		if err := repos.Summary.Create(&row); err != nil {
			t.Fatal(err)
		}
		if err := repos.Summary.RehomeOrDeleteByTaskID(origin.ID); err != nil {
			t.Fatal(err)
		}
		if got, err := repos.Summary.FindByTaskID(successor.ID); err != nil || got != nil {
			t.Fatalf("rehome %s escaped: %+v %v", kind, got, err)
		}
		if got, err := repos.Summary.FindByTaskID(origin.ID); err != nil || got != nil {
			t.Fatalf("private %s retained: %+v %v", kind, got, err)
		}
	}
	legacy := cacheTask(t, repos, 8, "old", "", "")
	scoped := cacheTask(t, repos, 7, "old", "", "{}")
	successor := cacheTask(t, repos, 9, "old", "", "")
	if err := repos.Summary.Create(&model.AISummary{TaskID: legacy.ID, FileMD5: "old", Content: "shared"}); err != nil {
		t.Fatal(err)
	}
	if err := repos.Summary.RehomeOrDeleteByTaskID(legacy.ID); err != nil {
		t.Fatal(err)
	}
	if got, err := repos.Summary.FindByTaskID(scoped.ID); err != nil || got != nil {
		t.Fatalf("legacy rehomed into scoped task: %+v %v", got, err)
	}
	if got, err := repos.Summary.FindByTaskID(successor.ID); err != nil || got == nil || got.Content != "shared" {
		t.Fatalf("legacy successor=%+v %v", got, err)
	}
}
