package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"vidlens/internal/store"
)

func TestCapacityPrivacyAndStorageAdministration(t *testing.T) {
	server, a := apiTest(t)
	res, e := server.Client().Get(server.URL + "/api/v1/site/capacity")
	if e != nil {
		t.Fatal(e)
	}
	defer res.Body.Close()
	var capacity map[string]any
	if e = json.NewDecoder(res.Body).Decode(&capacity); e != nil {
		t.Fatal(e)
	}
	if res.StatusCode != 200 || capacity["scope"] != "video_workspace" || capacity["total_bytes"] != float64(store.DefaultVideoQuota) || capacity["used_bytes"] != float64(0) {
		t.Fatalf("public quota: %d %+v", res.StatusCode, capacity)
	}
	if res.Header.Get("Cache-Control") != "no-store" {
		t.Fatal("capacity cached across owner changes")
	}
	allowed := map[string]bool{"scope": true, "total_bytes": true, "available_bytes": true, "used_bytes": true, "reserved_bytes": true, "workspace_bytes": true, "ceiling_bytes": true, "accepting": true, "level": true}
	for k := range capacity {
		if !allowed[k] {
			t.Fatalf("unexpected host field: %s", k)
		}
	}
	do := func(method, path, body string, auth bool) *http.Response {
		t.Helper()
		req, _ := http.NewRequest(method, server.URL+path, strings.NewReader(body))
		if auth {
			req.Header.Set("Authorization", "Bearer "+a.Config.AdminToken)
		}
		r, e := server.Client().Do(req)
		if e != nil {
			t.Fatal(e)
		}
		t.Cleanup(func() { r.Body.Close() })
		return r
	}
	if r := do("DELETE", "/api/v1/admin/videos/unknown/media", `{"asset_ids":["one"]}`, false); r.StatusCode != 401 {
		t.Fatal("anonymous deletion accepted", r.StatusCode)
	}
	r := do("GET", "/api/v1/admin/videos?limit=20&offset=0", "", true)
	b, _ := io.ReadAll(r.Body)
	if r.StatusCode != 200 || !strings.Contains(string(b), `"items":[]`) || !strings.Contains(string(b), `"counts":{"ready":0,"held":0,"gone":0}`) {
		t.Fatalf("video list: %d %s", r.StatusCode, b)
	}
	r = do("GET", "/api/v1/admin/videos?cleanup=nope", "", true)
	if r.StatusCode != 422 {
		t.Fatalf("unknown cleanup accepted: %d", r.StatusCode)
	}
	r = do("PUT", "/api/v1/admin/storage", `{"expected_revision":1,"video_quota_bytes":12884901888,"image_quota_bytes":5368709120}`, true)
	if r.StatusCode != 200 {
		b, _ := io.ReadAll(r.Body)
		t.Fatalf("quota save: %d %s", r.StatusCode, b)
	}
	r = do("GET", "/api/v1/site/capacity", "", false)
	var updated store.DiskCapacity
	if e = json.NewDecoder(r.Body).Decode(&updated); e != nil || updated.TotalBytes != 12<<30 {
		t.Fatalf("public quota not updated: %+v %v", updated, e)
	}
}
