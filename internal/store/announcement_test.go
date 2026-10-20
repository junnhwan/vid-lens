package store

import (
	"context"
	"strings"
	"testing"
)

func TestAnnouncementTimelinePublishesCorrectsAndWithdraws(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	list, e := s.Announcements(ctx)
	if e != nil || list.Revision != 0 || len(list.Items) != 0 {
		t.Fatalf("default: %+v %v", list, e)
	}
	feed, e := s.PublicAnnouncements(ctx)
	if e != nil || feed.Announcement != nil || len(feed.Timeline) != 0 {
		t.Fatalf("public default: %+v %v", feed, e)
	}
	publish := func(revision int64, title, body string) AnnouncementList {
		t.Helper()
		next, e := s.ApplyAnnouncement(ctx, AnnouncementCommand{ExpectedRevision: revision, Action: "publish", Title: title, Body: body})
		if e != nil {
			t.Fatal(e)
		}
		return next
	}
	first := publish(0, "  服务公告  ", " 第一段\r\n第二段 ")
	if first.Revision != 1 || len(first.Items) != 1 || first.Items[0].Status != "current" || first.Items[0].Title != "服务公告" || first.Items[0].Body != "第一段\n第二段" || first.Items[0].Version == "" || first.Items[0].PublishedAt == nil {
		t.Fatalf("publication: %+v", first)
	}
	if same := publish(first.Revision, "服务公告", "第一段\n第二段"); same.Revision != first.Revision || len(same.Items) != 1 {
		t.Fatal("identical publish appended another notice")
	}
	corrected, e := s.ApplyAnnouncement(ctx, AnnouncementCommand{ExpectedRevision: first.Revision, Action: "correct", ID: first.Items[0].ID, Title: first.Items[0].Title, Body: "更新后的正文"})
	if e != nil || corrected.Revision != 2 || corrected.Items[0].Version != first.Items[0].Version || corrected.Items[0].Body != "更新后的正文" {
		t.Fatalf("correction: %+v %v", corrected, e)
	}
	feed, e = s.PublicAnnouncements(ctx)
	if e != nil || feed.Announcement == nil || feed.Announcement.Version != first.Items[0].Version || feed.Announcement.Body != "更新后的正文" || len(feed.Timeline) != 1 {
		t.Fatalf("public correction: %+v %v", feed, e)
	}
	second := publish(corrected.Revision, "另一件事", "新的正文")
	if second.Revision != 3 || len(second.Items) != 2 || second.Items[0].Status != "current" || second.Items[0].Version == first.Items[0].Version || second.Items[1].Status != "published" || second.Items[1].Version != first.Items[0].Version {
		t.Fatalf("second publication: %+v", second)
	}
	archived, e := s.ApplyAnnouncement(ctx, AnnouncementCommand{ExpectedRevision: second.Revision, Action: "archive", ID: second.Items[0].ID})
	if e != nil || archived.Revision != 4 {
		t.Fatalf("archive: %+v %v", archived, e)
	}
	feed, e = s.PublicAnnouncements(ctx)
	if e != nil || feed.Announcement != nil || len(feed.Timeline) != 2 {
		t.Fatalf("archived notice left the timeline or kept the popup: %+v %v", feed, e)
	}
	hidden, e := s.ApplyAnnouncement(ctx, AnnouncementCommand{ExpectedRevision: archived.Revision, Action: "withdraw", ID: second.Items[0].ID})
	if e != nil || hidden.Revision != 5 {
		t.Fatalf("withdraw: %+v %v", hidden, e)
	}
	feed, e = s.PublicAnnouncements(ctx)
	if e != nil || feed.Announcement != nil || len(feed.Timeline) != 1 || feed.Timeline[0].Version != first.Items[0].Version {
		t.Fatalf("withdrawn notice stayed public: %+v %v", feed, e)
	}
	if _, e = s.ApplyAnnouncement(ctx, AnnouncementCommand{ExpectedRevision: archived.Revision, Action: "archive", ID: first.Items[0].ID}); e == nil {
		t.Fatal("stale editor changed the timeline")
	}
	if got, e := s.Announcements(ctx); e != nil || got.Revision != hidden.Revision || len(got.Items) != 2 {
		t.Fatalf("stale write changed the timeline: %+v %v", got, e)
	}
}

func TestAnnouncementRejectsInvalidInput(t *testing.T) {
	var s Store
	for _, in := range []AnnouncementCommand{
		{ExpectedRevision: -1, Action: "publish", Title: "标题", Body: "正文"},
		{Action: "publish", Title: " ", Body: "正文"},
		{Action: "publish", Title: "标题", Body: "\r\n "},
		{Action: "publish", Title: strings.Repeat("字", 121), Body: "正文"},
		{Action: "publish", Title: "标题", Body: strings.Repeat("字", 8001)},
		{Action: "publish", Title: "标题", Body: "before\x00after"},
		{Action: "correct", Title: "标题", Body: "正文"},
		{Action: "forget"},
	} {
		if _, e := s.ApplyAnnouncement(context.Background(), in); e == nil {
			t.Fatalf("accepted invalid input: %+v", in)
		}
	}
}

func TestAnnouncementMigrationAppendsAfterPublishedRecommendation(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	if _, e := s.Pool.Exec(ctx, "ALTER TABLE model_probes DROP COLUMN started_at"); e != nil {
		t.Fatal(e)
	}
	if _, e := s.Pool.Exec(ctx, "DROP TABLE IF EXISTS site_announcement_entries; DROP TABLE IF EXISTS site_announcement_state; DROP TABLE IF EXISTS site_announcement; DROP TABLE IF EXISTS execution_settings; ALTER TABLE example_showcases DROP COLUMN IF EXISTS process, DROP COLUMN IF EXISTS process_frozen; DROP INDEX IF EXISTS example_question_once; ALTER TABLE storage_settings DROP COLUMN IF EXISTS host_min_free_bytes, DROP COLUMN IF EXISTS source_download_limit_bytes, DROP COLUMN IF EXISTS disk_stop_percent; DELETE FROM schema_migrations WHERE version>18"); e != nil {
		t.Fatal(e)
	}
	if e := s.Migrate(ctx); e != nil {
		t.Fatalf("upgrade from the published recommendation migration: %v", e)
	}
	if e := s.Migrate(ctx); e != nil {
		t.Fatalf("repeat startup: %v", e)
	}
	if list, e := s.Announcements(ctx); e != nil || list.Revision != 0 || len(list.Items) != 0 {
		t.Fatalf("upgraded default announcement: %+v %v", list, e)
	}
	var exists bool
	if e := s.Pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version=19)").Scan(&exists); e != nil || !exists {
		t.Fatalf("announcement did not append at version 19: %v", e)
	}
}

func TestAnnouncementHistoryKeepsThePublishedVersionAndHidesAStoppedNotice(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	if _, e := s.Pool.Exec(ctx, `
		DROP TABLE IF EXISTS site_announcement_entries;
		DROP TABLE IF EXISTS site_announcement_state;
		DELETE FROM schema_migrations WHERE version=22;
		CREATE TABLE site_announcement (
			singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
			revision bigint NOT NULL CHECK (revision > 0),
			enabled boolean NOT NULL DEFAULT false,
			title text NOT NULL DEFAULT '' CHECK (char_length(title) <= 120),
			body text NOT NULL DEFAULT '' CHECK (char_length(body) <= 8000),
			version text NOT NULL DEFAULT '',
			updated_at timestamptz NOT NULL DEFAULT now(),
			CHECK (NOT enabled OR (btrim(title) <> '' AND btrim(body) <> '' AND version <> ''))
		);
		INSERT INTO site_announcement(singleton, revision, enabled, title, body, version, updated_at)
		VALUES (true, 4, false, '旧公告', '已停用的正文', 'bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb', '2026-10-02T00:00:00Z')
	`); e != nil {
		t.Fatal(e)
	}
	if e := s.Migrate(ctx); e != nil {
		t.Fatal(e)
	}
	list, e := s.Announcements(ctx)
	if e != nil || list.Revision != 4 || len(list.Items) != 1 || list.Items[0].Status != "withdrawn" || list.Items[0].Version != "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" || list.Items[0].Title != "旧公告" {
		t.Fatalf("stopped notice: %+v %v", list, e)
	}
	feed, e := s.PublicAnnouncements(ctx)
	if e != nil || feed.Announcement != nil || len(feed.Timeline) != 0 {
		t.Fatalf("stopped notice became visible: %+v %v", feed, e)
	}

	s = testStore(t)
	if _, e = s.Pool.Exec(ctx, `
		DROP TABLE IF EXISTS site_announcement_entries;
		DROP TABLE IF EXISTS site_announcement_state;
		DELETE FROM schema_migrations WHERE version=22;
		CREATE TABLE site_announcement (
			singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
			revision bigint NOT NULL CHECK (revision > 0),
			enabled boolean NOT NULL DEFAULT false,
			title text NOT NULL DEFAULT '' CHECK (char_length(title) <= 120),
			body text NOT NULL DEFAULT '' CHECK (char_length(body) <= 8000),
			version text NOT NULL DEFAULT '',
			updated_at timestamptz NOT NULL DEFAULT now()
		);
		INSERT INTO site_announcement(singleton, revision, enabled, title, body, version, updated_at)
		VALUES (true, 6, true, '欢迎', '仍在展示', 'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa', '2026-10-01T00:00:00Z')
	`); e != nil {
		t.Fatal(e)
	}
	if e = s.Migrate(ctx); e != nil {
		t.Fatal(e)
	}
	list, e = s.Announcements(ctx)
	if e != nil || list.Revision != 6 || len(list.Items) != 1 || list.Items[0].Status != "current" || list.Items[0].ID != "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" || list.Items[0].Version != "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" {
		t.Fatalf("published notice: %+v %v", list, e)
	}
	feed, e = s.PublicAnnouncements(ctx)
	if e != nil || feed.Announcement == nil || feed.Announcement.Version != "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" || len(feed.Timeline) != 1 {
		t.Fatalf("published notice was not kept: %+v %v", feed, e)
	}
}
