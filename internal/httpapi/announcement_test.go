package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
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
	public := func() (int, struct {
		Announcement *struct {
			Version string `json:"version"`
			Title   string `json:"title"`
			Body    string `json:"body"`
		} `json:"announcement"`
		Timeline []struct {
			Version string `json:"version"`
			Title   string `json:"title"`
		} `json:"timeline"`
	}) {
		t.Helper()
		code, b := call("GET", "/api/v1/site/announcement", "", nil, "")
		var result struct {
			Announcement *struct {
				Version string `json:"version"`
				Title   string `json:"title"`
				Body    string `json:"body"`
			} `json:"announcement"`
			Timeline []struct {
				Version string `json:"version"`
				Title   string `json:"title"`
			} `json:"timeline"`
		}
		if e := json.Unmarshal(b, &result); e != nil || code != 200 {
			t.Fatalf("public notice %d: %s", code, b)
		}
		return code, result
	}
	if _, result := public(); result.Announcement != nil || len(result.Timeline) != 0 {
		t.Fatalf("empty site exposed a notice: %+v", result)
	}
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
		t.Fatal("owner could not read announcements")
	}
	if code, _ := call("PUT", path, `{"expected_revision":0,"action":"publish","title":" ","body":"正文"}`, owner, session.CSRF); code != 422 {
		t.Fatalf("empty publication accepted: %d", code)
	}
	code, b := call("PUT", path, `{"expected_revision":0,"action":"publish","title":"欢迎","body":"第一行\n第二行"}`, owner, session.CSRF)
	var saved struct {
		Revision int64 `json:"revision"`
		Items    []struct {
			ID      string `json:"id"`
			Status  string `json:"status"`
			Version string `json:"version"`
			Title   string `json:"title"`
			Body    string `json:"body"`
		} `json:"items"`
	}
	if e := json.Unmarshal(b, &saved); e != nil || code != 200 || saved.Revision != 1 || len(saved.Items) != 1 || saved.Items[0].Status != "current" || saved.Items[0].Version == "" {
		t.Fatalf("save %d: %s", code, b)
	}
	_, visible := public()
	if visible.Announcement == nil || visible.Announcement.Title != "欢迎" || visible.Announcement.Version != saved.Items[0].Version || len(visible.Timeline) != 1 {
		t.Fatal("public notice did not match publication")
	}
	code, b = call("PUT", path, `{"expected_revision":1,"action":"correct","id":"`+saved.Items[0].ID+`","title":"欢迎","body":"改正错字"}`, owner, session.CSRF)
	var corrected struct {
		Revision int64 `json:"revision"`
		Items    []struct {
			Version string `json:"version"`
			Body    string `json:"body"`
		} `json:"items"`
	}
	if e := json.Unmarshal(b, &corrected); e != nil || code != 200 || corrected.Items[0].Version != saved.Items[0].Version || corrected.Items[0].Body != "改正错字" {
		t.Fatalf("correction %d: %s", code, b)
	}
	_, visible = public()
	if visible.Announcement == nil || visible.Announcement.Version != saved.Items[0].Version || visible.Announcement.Body != "改正错字" {
		t.Fatal("a typo correction asked visitors to read the notice again")
	}
	if code, _ := call("PUT", path, `{"expected_revision":1,"action":"archive","id":"`+saved.Items[0].ID+`"}`, owner, session.CSRF); code != 409 {
		t.Fatalf("stale write accepted: %d", code)
	}
	if code, _ := call("PUT", path, `{"expected_revision":2,"action":"archive","id":"`+saved.Items[0].ID+`"}`, owner, session.CSRF); code != 200 {
		t.Fatal("ending the popup failed")
	}
	_, visible = public()
	if visible.Announcement != nil || len(visible.Timeline) != 1 || visible.Timeline[0].Title != "欢迎" {
		t.Fatalf("ending the popup removed the timeline: %+v", visible)
	}
	if code, _ := call("PUT", path, `{"expected_revision":3,"action":"withdraw","id":"`+saved.Items[0].ID+`"}`, owner, session.CSRF); code != 200 {
		t.Fatal("withdraw failed")
	}
	_, visible = public()
	if visible.Announcement != nil || len(visible.Timeline) != 0 {
		t.Fatal("withdrawn notice stayed visible")
	}
}
