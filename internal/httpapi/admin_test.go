package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOwnerCookieIsIndependentAndAllAdminWritesRequireCSRF(t *testing.T) {
	server, a := apiTest(t)
	if e := a.Store.SetOwnerPassword(context.Background(), "isolated-fixture-password"); e != nil {
		t.Fatal(e)
	}
	do := func(method, path, body, csrf string, cookie *http.Cookie) *http.Response {
		t.Helper()
		r, _ := http.NewRequest(method, server.URL+path, strings.NewReader(body))
		r.Header.Set("Origin", a.Config.Origin)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-CSRF-Token", csrf)
		if cookie != nil {
			r.AddCookie(cookie)
		}
		v, e := server.Client().Do(r)
		if e != nil {
			t.Fatal(e)
		}
		t.Cleanup(func() { v.Body.Close() })
		return v
	}
	visitor := do("POST", "/api/v1/session", "{}", "", nil).Cookies()[0]
	for _, path := range []string{"/api/v1/admin/config", "/api/v1/admin/models", "/api/v1/admin/providers", "/api/v1/admin/status", "/api/v1/admin/diagnostics"} {
		if v := do("GET", path, "", "", visitor); v.StatusCode != 401 {
			t.Fatalf("visitor accessed %s: %d", path, v.StatusCode)
		}
	}
	login := do("POST", "/api/v1/admin/login", `{"password":"isolated-fixture-password"}`, "", nil)
	if login.StatusCode != 200 {
		b, _ := io.ReadAll(login.Body)
		t.Fatalf("login %d %s", login.StatusCode, b)
	}
	owner := login.Cookies()[0]
	if owner.Name != adminCookie || !owner.HttpOnly || owner.SameSite != http.SameSiteStrictMode {
		t.Fatal("owner cookie is not independent/HttpOnly/Strict")
	}
	var result struct {
		CSRF string `json:"csrf_token"`
	}
	if e := json.NewDecoder(login.Body).Decode(&result); e != nil {
		t.Fatal(e)
	}
	if v := do("PUT", "/api/v1/admin/model-settings", `{}`, "", owner); v.StatusCode != 403 {
		t.Fatal("owner write bypassed CSRF")
	}
	if v := do("GET", "/api/v1/admin/models", "", "", owner); v.StatusCode != 200 {
		t.Fatal("owner session inaccessible")
	}
	if v := do("DELETE", "/api/v1/admin/session", "", result.CSRF, owner); v.StatusCode != 200 {
		t.Fatalf("logout %d", v.StatusCode)
	}
	if v := do("GET", "/api/v1/admin/models", "", "", owner); v.StatusCode != 401 {
		t.Fatal("logout did not revoke session")
	}
	if v := do("GET", "/api/v1/site/like", "", "", visitor); v.StatusCode != 200 {
		t.Fatal("owner logout destroyed visitor session")
	}
}
func TestLegacyClientRejectsExternalCitationAndStreamCarriesContract(t *testing.T) {
	_, a := apiTest(t)
	old, _ := http.NewRequest("GET", "https://vidlens.test/api/v1/test", nil)
	value := map[string]any{"answer": map[string]any{"citations": map[string]any{"web": map[string]any{"kind": "external_reference"}}}}
	w := httptest.NewRecorder()
	if a.checkContract(w, old, value) || w.Code != 409 {
		t.Fatal("old client silently received an unknown citation type")
	}
	old.Header.Set("X-VidLens-Contract", "2")
	stream := map[string]any{"url": "/events?cursor=one"}
	v := withContract(old, map[string]any{"stream": stream})
	if !strings.Contains(v["stream"].(map[string]any)["url"].(string), "contract=2") || stream["url"] != "/events?cursor=one" {
		t.Fatal("contract propagation mutated old projection")
	}
}
