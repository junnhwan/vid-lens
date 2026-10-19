package store

import (
	"context"
	"strings"
	"testing"
)

func TestAnnouncementPublicationLifecycle(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	v, e := s.Announcement(ctx)
	if e != nil || v != (Announcement{}) {
		t.Fatalf("default: %+v %v", v, e)
	}
	save := func(enabled bool, title, body string) Announcement {
		t.Helper()
		next, e := s.SaveAnnouncement(ctx, AnnouncementInput{ExpectedRevision: v.Revision, Enabled: enabled, Title: title, Body: body})
		if e != nil {
			t.Fatal(e)
		}
		v = next
		return next
	}
	draft := save(false, "  服务公告  ", " 第一段\r\n第二段 ")
	if draft.Revision != 1 || draft.Version != "" || draft.Title != "服务公告" || draft.Body != "第一段\n第二段" {
		t.Fatalf("draft: %+v", draft)
	}
	published := save(true, v.Title, v.Body)
	if published.Version == "" || published.Revision != 2 {
		t.Fatalf("publication: %+v", published)
	}
	other := &Store{Pool: s.Pool}
	if got, e := other.Announcement(ctx); e != nil || got != published {
		t.Fatalf("not persisted: %+v %v", got, e)
	}
	if same := save(true, "  "+v.Title, v.Body+" \n"); same != published {
		t.Fatal("identical save republished the notice")
	}
	edited := save(true, v.Title, "更新后的正文")
	if edited.Version == published.Version {
		t.Fatal("published edit did not notify readers again")
	}
	titleEdit := save(true, "新的标题", v.Body)
	if titleEdit.Version == edited.Version {
		t.Fatal("title edit reused an old dismissal")
	}
	stopped := save(false, v.Title, v.Body)
	if stopped.Enabled || stopped.Version != titleEdit.Version {
		t.Fatal("stop did not retain the saved notice")
	}
	draft = save(false, v.Title, "下一次发布的正文")
	if draft.Version != stopped.Version {
		t.Fatal("unpublished edit minted a publication")
	}
	republished := save(true, v.Title, v.Body)
	if republished.Version == stopped.Version {
		t.Fatal("re-enabling did not create a fresh publication")
	}
	if _, e := s.SaveAnnouncement(ctx, AnnouncementInput{ExpectedRevision: stopped.Revision, Enabled: false}); e == nil {
		t.Fatal("stale editor overwrote the current announcement")
	}
	if got, _ := s.Announcement(ctx); got != republished {
		t.Fatal("stale write changed the persisted announcement")
	}
}

func TestAnnouncementRejectsInvalidInput(t *testing.T) {
	var s Store
	for _, in := range []AnnouncementInput{
		{ExpectedRevision: -1},
		{Enabled: true, Title: " ", Body: "正文"},
		{Enabled: true, Title: "标题", Body: "\r\n "},
		{Title: strings.Repeat("字", 121)},
		{Body: strings.Repeat("字", 8001)},
		{Body: "before\x00after"},
	} {
		if _, e := s.SaveAnnouncement(context.Background(), in); e == nil {
			t.Fatalf("accepted invalid input: %+v", in)
		}
	}
}

func TestAnnouncementMigrationAppendsAfterPublishedRecommendation(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	// Reproduce the published schema: recommendation was already migration 18.
	// Pending work must append after it rather than shift its recorded version.
	if _, e := s.Pool.Exec(ctx, "DROP TABLE site_announcement; DROP TABLE IF EXISTS execution_settings; ALTER TABLE example_showcases DROP COLUMN IF EXISTS process, DROP COLUMN IF EXISTS process_frozen; DROP INDEX IF EXISTS example_question_once; DELETE FROM schema_migrations WHERE version>18"); e != nil {
		t.Fatal(e)
	}
	if e := s.Migrate(ctx); e != nil {
		t.Fatalf("upgrade from the published recommendation migration: %v", e)
	}
	if e := s.Migrate(ctx); e != nil {
		t.Fatalf("repeat startup: %v", e)
	}
	if v, e := s.Announcement(ctx); e != nil || v != (Announcement{}) {
		t.Fatalf("upgraded default announcement: %+v %v", v, e)
	}
	var exists bool
	if e := s.Pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version=19)").Scan(&exists); e != nil || !exists {
		t.Fatalf("announcement did not append at version 19: %v", e)
	}
}
