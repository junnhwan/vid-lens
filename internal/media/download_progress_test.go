package media

import (
	"context"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
	"vidlens/internal/domain"
	"vidlens/internal/testsupport"
)

func TestDownloadProgressOnlyReportsNewBytesAndBoundsWrites(t *testing.T) {
	var reports []int64
	ctx := context.WithValue(context.Background(), downloadProgressKey{}, downloadProgress(func(_ context.Context, key string, bytes int64) error {
		if key != "track" {
			t.Fatal(key)
		}
		reports = append(reports, bytes)
		return nil
	}))
	reader := newProgressReader(ctx, strings.NewReader("abcdef"), "track")
	buf := make([]byte, 2)
	for i := 0; i < 2; i++ {
		if _, err := reader.Read(buf); err != nil {
			t.Fatal(err)
		}
	}
	if !reflect.DeepEqual(reports, []int64{2}) {
		t.Fatal("every chunk wrote to the database", reports)
	}
	reader.reportedAt = time.Now().Add(-31 * time.Second)
	if _, err := reader.Read(buf); err != nil {
		t.Fatal(err)
	}
	reader.reportedAt = time.Now().Add(-31 * time.Second)
	if _, err := reader.Read(buf); !errors.Is(err, io.EOF) {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(reports, []int64{2, 6}) {
		t.Fatal("empty reads refreshed progress or cumulative byte count was lost", reports)
	}
}

func TestActualDownloadBytesReachDurableSourceProgress(t *testing.T) {
	for _, route := range []string{"youtube", "bilibili"} {
		t.Run(route, func(t *testing.T) {
			db, _, cfg := testsupport.Services(t)
			ctx := context.Background()
			owner, err := db.NewSession(ctx, domain.ID(), time.Hour)
			if err != nil {
				t.Fatal(err)
			}
			a, err := db.CreateRequest(ctx, owner.ID, domain.ID(), "local://transfer.mp4", "", true)
			if err != nil {
				t.Fatal(err)
			}
			r, err := db.Request(ctx, owner.ID, a.ID)
			if err != nil {
				t.Fatal(err)
			}
			c, err := db.Claim(ctx, *r.ActiveRunID, 1, "downloader")
			if err != nil || c == nil {
				t.Fatal(err)
			}
			if _, err := db.Pool.Exec(ctx, "UPDATE job_runs SET updated_at=now()-interval '6 minutes' WHERE id=$1", c.Run.ID); err != nil {
				t.Fatal(err)
			}
			svc := &Service{Store: db, Config: cfg}
			dctx := svc.sourceDownloadContext(ctx, *c)
			dst := filepath.Join(t.TempDir(), "track")
			if route == "youtube" {
				fakeExtractor(t, "printf 'complete media'\n")
				err = svc.downloadStream(dctx, "https://youtube.com/watch?v=abcdefghijk", "best", dst, 1024)
			} else {
				client := &http.Client{Transport: bilibiliRoundTrip(func(*http.Request) (*http.Response, error) {
					return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("complete media")), ContentLength: 14, Header: http.Header{}}, nil
				})}
				err = (&bilibiliClient{http: client}).download(dctx, bilibiliTrack{BaseURL: "https://cdn.example.test/video"}, dst, 1024)
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := db.Repair(ctx, c.Run.ID, 1, "active", nil); err != nil {
				t.Fatal(err)
			}
			run, err := db.Run(ctx, c.Run.ID)
			if err != nil || run.Status != "running" {
				t.Fatalf("download byte progress was invisible to recovery: %s %v", run.Status, err)
			}
			var reports int
			if err := db.Pool.QueryRow(ctx, "SELECT count(*) FROM run_operations WHERE run_id=$1 AND kind='source_download_progress'", c.Run.ID).Scan(&reports); err != nil || reports != 1 {
				t.Fatal("download did not persist progress", reports, err)
			}
		})
	}
}
