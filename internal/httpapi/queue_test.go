package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"vidlens/internal/domain"
	"vidlens/internal/store"
)

func TestPublicQueueCountsGlobalWorkWithoutDisclosingRequests(t *testing.T) {
	server, a := apiTest(t)
	a.Store.VideoConcurrency = 2
	ctx := context.Background()
	session, err := a.Store.NewSession(ctx, domain.ID(), a.Config.SessionTTL)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, url := range []string{"local://private-one.mp4", "local://private-two.mp4", "local://private-three.mp4", "local://private-four.mp4"} {
		v, err := a.Store.CreateRequest(ctx, session.ID, domain.ID(), url, "", true)
		if err != nil {
			t.Fatal(err)
		}
		r, _ := a.Store.Request(ctx, session.ID, v.ID)
		ids = append(ids, *r.ActiveRunID)
	}
	// A current worker, a queued job, a retry, and an expired worker lease.
	if _, err := a.Store.Pool.Exec(ctx, "UPDATE job_runs SET status='running',lease_expires_at=now()+interval '1 minute' WHERE id=$1", ids[0]); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Store.Pool.Exec(ctx, "UPDATE job_runs SET status='retry_wait' WHERE id=$1", ids[2]); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Store.Pool.Exec(ctx, "UPDATE job_runs SET status='running',lease_expires_at=now()-interval '1 minute' WHERE id=$1", ids[3]); err != nil {
		t.Fatal(err)
	}
	response, err := http.Get(server.URL + "/api/v1/site/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || response.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("status/cache: %d %v", response.StatusCode, response.Header)
	}
	var body map[string]json.RawMessage
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if len(body) != 5 {
		t.Fatalf("unexpected public fields: %v", body)
	}
	raw, _ := json.Marshal(body)
	var status store.QueueStatus
	if err := json.Unmarshal(raw, &status); err != nil {
		t.Fatal(err)
	}
	if status.MaxConcurrent != 2 || status.Processing != 1 || status.Waiting != 3 || status.AsOf.IsZero() {
		t.Fatalf("wrong aggregate: %+v", status)
	}
	// Question answering uses an independent queue; terminal/expired work must
	// also disappear instead of leaving a phantom occupied slot on the homepage.
	if _, err := a.Store.Pool.Exec(ctx, "UPDATE job_runs SET kind='qa.answer' WHERE id=$1;", ids[1]); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Store.Pool.Exec(ctx, "UPDATE job_runs SET deadline_at=now()-interval '1 second' WHERE id=$1", ids[2]); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Store.Pool.Exec(ctx, "UPDATE job_runs SET status='failed' WHERE id=$1", ids[3]); err != nil {
		t.Fatal(err)
	}
	v, err := a.Store.QueueStatus(ctx)
	if err != nil || v.Processing != 1 || v.Waiting != 0 {
		t.Fatalf("question/terminal work leaked: %+v %v", v, err)
	}
}
