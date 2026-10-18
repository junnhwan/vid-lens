package httpapi

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"vidlens/internal/store"
)

func TestShareHTTPRequiresOwnerForWritesAndAllowsCookielessRead(t *testing.T) {
	server, _ := apiTest(t)
	do := func(method, path, body, csrf string, cookie *http.Cookie) *http.Response {
		t.Helper()
		req, _ := http.NewRequest(method, server.URL+path, bytes.NewBufferString(body))
		req.Header.Set("Origin", "https://vidlens.test")
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Idempotency-Key", "share-test-request")
		req.Header.Set("X-CSRF-Token", csrf)
		if cookie != nil {
			req.AddCookie(cookie)
		}
		response, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { response.Body.Close() })
		return response
	}
	session := do("POST", "/api/v1/session", "{}", "", nil)
	var sessionBody struct {
		CSRF string `json:"csrf_token"`
	}
	if err := json.NewDecoder(session.Body).Decode(&sessionBody); err != nil {
		t.Fatal(err)
	}
	cookie := session.Cookies()[0]
	created := do("POST", "/api/v1/requests", `{"video_url":"https://youtube.com/watch?v=abcdefghijk"}`, sessionBody.CSRF, cookie)
	if created.StatusCode != http.StatusCreated {
		raw, _ := io.ReadAll(created.Body)
		t.Fatalf("create: %d %s", created.StatusCode, raw)
	}
	path := created.Header.Get("Location") + "/share"
	for _, method := range []string{"GET", "PUT", "DELETE"} {
		if response := do(method, path, "{}", "", nil); response.StatusCode != 401 {
			t.Fatalf("unauthenticated %s: %d", method, response.StatusCode)
		}
	}
	for _, method := range []string{"PUT", "DELETE"} {
		if response := do(method, path, "{}", "wrong", cookie); response.StatusCode != 403 {
			t.Fatalf("missing CSRF %s: %d", method, response.StatusCode)
		}
	}
	other := do("POST", "/api/v1/session", "{}", "", nil)
	var otherBody struct {
		CSRF string `json:"csrf_token"`
	}
	_ = json.NewDecoder(other.Body).Decode(&otherBody)
	for _, method := range []string{"GET", "PUT", "DELETE"} {
		if response := do(method, path, "{}", otherBody.CSRF, other.Cookies()[0]); response.StatusCode != 404 {
			t.Fatalf("cross-browser %s: %d", method, response.StatusCode)
		}
	}
	response := do("PUT", path, "{}", sessionBody.CSRF, cookie)
	var share store.RequestShare
	if response.StatusCode != 200 || json.NewDecoder(response.Body).Decode(&share) != nil || share.URL == nil {
		t.Fatal("share not created")
	}
	publicPath := "/api/v1/shares/" + strings.TrimPrefix(*share.URL, "/s/")
	if head := do("HEAD", path, "", sessionBody.CSRF, cookie); head.StatusCode != 200 {
		t.Fatalf("owner HEAD: %d", head.StatusCode)
	}
	public := do("GET", publicPath, "", "", nil)
	raw, _ := io.ReadAll(public.Body)
	if public.StatusCode != 200 || !strings.Contains(string(raw), "waiting_source") || strings.Contains(string(raw), "csrf_token") || strings.Contains(string(raw), "session_id") {
		t.Fatalf("cookieless read: %d %s", public.StatusCode, raw)
	}
	if public.Header.Get("Set-Cookie") != "" || !strings.Contains(public.Header.Get("Cache-Control"), "no-store") || public.Header.Get("Referrer-Policy") != "no-referrer" {
		t.Fatal("share establishes ownership or allows cached access")
	}
	if response := do("DELETE", path, "", sessionBody.CSRF, cookie); response.StatusCode != 200 {
		t.Fatal("revoke failed")
	}
	if response := do("GET", publicPath, "", "", nil); response.StatusCode != 404 {
		t.Fatalf("revoked share: %d", response.StatusCode)
	}
	if response := do("GET", publicPath+"/frames/not-public/content", "", "", nil); response.StatusCode != 404 {
		t.Fatalf("revoked image: %d", response.StatusCode)
	}
}
