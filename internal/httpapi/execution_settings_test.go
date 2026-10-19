package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"vidlens/internal/store"
)

func TestExecutionSettingsOwnerWriteIsLiveAndRevisionProtected(t *testing.T) {
	srv, a := apiTest(t)
	call := func(method, path, body string, owner bool) (int, []byte) {
		t.Helper()
		r, _ := http.NewRequest(method, srv.URL+path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", a.Config.Origin)
		if owner {
			r.Header.Set("Authorization", "Bearer "+a.Config.AdminToken)
		}
		resp, e := srv.Client().Do(r)
		if e != nil {
			t.Fatal(e)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, b
	}
	path := "/api/v1/admin/execution-settings"
	body := `{"max_video_running":10,"expected_revision":0}`
	for _, method := range []string{"GET", "PUT"} {
		if code, _ := call(method, path, body, false); code != 401 {
			t.Fatalf("public %s accepted: %d", method, code)
		}
	}
	code, b := call("PUT", path, body, true)
	var v store.ExecutionSettings
	if e := json.Unmarshal(b, &v); e != nil || code != 200 || v.Revision != 1 || v.MaxVideoRunning != 10 {
		t.Fatalf("save %d: %s", code, b)
	}
	code, b = call("GET", "/api/v1/site/queue", "", false)
	var q store.QueueStatus
	if e := json.Unmarshal(b, &q); e != nil || code != 200 || q.MaxConcurrent != 10 {
		t.Fatalf("public setting stale: %s", b)
	}
	if code, _ := call("PUT", path, body, true); code != 409 {
		t.Fatal("stale revision accepted")
	}
	if code, _ := call("PUT", path, `{"max_video_running":1.5,"expected_revision":1}`, true); code != 400 {
		t.Fatalf("fraction accepted: %d", code)
	}
	if code, _ := call("PUT", path, `{"max_video_running":65,"expected_revision":1}`, true); code != 422 {
		t.Fatalf("invalid limit accepted: %d", code)
	}
}
