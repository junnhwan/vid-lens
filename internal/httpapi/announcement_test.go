package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"vidlens/internal/store"
)

func TestAnnouncementOwnerConfigurationAndPublicVisibility(t *testing.T) {
	srv, a := apiTest(t)
	call := func(method, path, body string, cookie *http.Cookie, csrf string) (int, []byte) {
		t.Helper()
		r, _ := http.NewRequest(method, srv.URL+path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", a.Config.Origin)
		r.Header.Set("X-CSRF-Token", csrf)
		if cookie != nil {
			r.AddCookie(cookie)
		}
		resp, e := srv.Client().Do(r)
		if e != nil {
			t.Fatal(e)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		if path == "/api/v1/site/announcement" && !strings.Contains(resp.Header.Get("Cache-Control"), "no-store") {
			t.Fatal("announcement visibility could be cached after stopping")
		}
		return resp.StatusCode, b
	}
	public := func(want bool) map[string]string {
		t.Helper()
		code, b := call("GET", "/api/v1/site/announcement", "", nil, "")
		var result struct {
			Announcement map[string]string `json:"announcement"`
		}
		if e := json.Unmarshal(b, &result); e != nil || code != 200 || (result.Announcement != nil) != want {
			t.Fatalf("public notice %d: %s", code, b)
		}
		if want && (len(result.Announcement) != 3 || result.Announcement["version"] == "") {
			t.Fatalf("unexpected public fields: %s", b)
		}
		return result.Announcement
	}
	public(false)
	path := "/api/v1/admin/announcement"
	for _, method := range []string{"GET", "PUT"} {
		if code, _ := call(method, path, "{}", nil, ""); code != 401 {
			t.Fatalf("public %s accepted: %d", method, code)
		}
	}
	if e := a.Store.SetOwnerPassword(context.Background(), "announcement-test-password"); e != nil {
		t.Fatal(e)
	}
	login, _ := http.NewRequest("POST", srv.URL+"/api/v1/admin/login", strings.NewReader(`{"password":"announcement-test-password"}`))
	login.Header.Set("Origin", a.Config.Origin)
	login.Header.Set("Content-Type", "application/json")
	resp, e := srv.Client().Do(login)
	if e != nil {
		t.Fatal(e)
	}
	defer resp.Body.Close()
	var session struct {
		CSRF string `json:"csrf_token"`
	}
	if e := json.NewDecoder(resp.Body).Decode(&session); e != nil || resp.StatusCode != 200 || len(resp.Cookies()) == 0 {
		t.Fatalf("owner login failed: %d %v", resp.StatusCode, e)
	}
	owner := resp.Cookies()[0]
	if code, _ := call("PUT", path, "{}", owner, ""); code != 403 {
		t.Fatalf("cookie write bypassed CSRF: %d", code)
	}
	if code, _ := call("GET", path, "", owner, ""); code != 200 {
		t.Fatal("owner could not read announcement")
	}
	if code, _ := call("PUT", path, `{"expected_revision":0,"enabled":true,"title":" ","body":"正文"}`, owner, session.CSRF); code != 422 {
		t.Fatalf("empty publication accepted: %d", code)
	}
	if code, _ := call("PUT", path, `{"expected_revision":0,"enabled":false,"title":"内部草稿","body":"尚未发布"}`, owner, session.CSRF); code != 200 {
		t.Fatal("draft save failed")
	}
	public(false)
	code, b := call("PUT", path, `{"expected_revision":1,"enabled":true,"title":"欢迎","body":"第一行\n第二行"}`, owner, session.CSRF)
	var saved store.Announcement
	if e := json.Unmarshal(b, &saved); e != nil || code != 200 || !saved.Enabled || saved.Revision != 2 {
		t.Fatalf("save %d: %s", code, b)
	}
	visible := public(true)
	if visible["title"] != saved.Title || visible["body"] != saved.Body || visible["version"] != saved.Version {
		t.Fatal("public notice did not match publication")
	}
	if code, _ := call("PUT", path, `{"expected_revision":1,"enabled":false}`, owner, session.CSRF); code != 409 {
		t.Fatalf("stale write accepted: %d", code)
	}
	if code, _ := call("PUT", path, `{"expected_revision":2,"enabled":false,"title":"欢迎","body":"第一行\n第二行"}`, owner, session.CSRF); code != 200 {
		t.Fatal("stop failed")
	}
	public(false)
	code, b = call("GET", path, "", owner, "")
	if e := json.Unmarshal(b, &saved); e != nil || code != 200 || saved.Enabled || saved.Body != "第一行\n第二行" {
		t.Fatal("stop discarded the owner's saved content")
	}
}
