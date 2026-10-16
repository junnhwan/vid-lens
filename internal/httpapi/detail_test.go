package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestDetailHTTPTraceIsCorrelatedAndPolicyPayloadStaysPrivate(t *testing.T) {
	server, a := apiTest(t)
	post := func(path, body, key, csrf string, cookie *http.Cookie) *http.Response {
		t.Helper()
		r, _ := http.NewRequest("POST", server.URL+path, strings.NewReader(body))
		r.Header.Set("Origin", a.Config.Origin)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-CSRF-Token", csrf)
		r.Header.Set("Idempotency-Key", key)
		if cookie != nil {
			r.AddCookie(cookie)
		}
		v, err := server.Client().Do(r)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { v.Body.Close() })
		return v
	}
	session := post("/api/v1/session", "{}", "", "", nil)
	var ses struct {
		CSRF string `json:"csrf_token"`
	}
	if err := json.NewDecoder(session.Body).Decode(&ses); err != nil {
		t.Fatal(err)
	}
	cookie := session.Cookies()[0]
	invalid := post("/api/v1/requests", `{"video_url":"https://youtube.com/watch?v=abcdefghijk","detail_level":""}`, "bad", ses.CSRF, cookie)
	var problem map[string]any
	_ = json.NewDecoder(invalid.Body).Decode(&problem)
	if invalid.StatusCode != 422 || problem["trace_id"] != invalid.Header.Get("X-Request-ID") || problem["meta"] == nil {
		t.Fatalf("unsafe or uncorrelated argument error: %d %+v", invalid.StatusCode, problem)
	}
	accepted := post("/api/v1/requests", `{"video_url":"https://youtube.com/watch?v=abcdefghijk","detail_level":"detailed"}`, "detailed", ses.CSRF, cookie)
	var view struct {
		Request struct {
			ID     string `json:"id"`
			Detail string `json:"detail_level"`
			Policy any    `json:"detail_policy"`
		} `json:"request"`
	}
	if err := json.NewDecoder(accepted.Body).Decode(&view); err != nil {
		t.Fatal(err)
	}
	if accepted.StatusCode != 201 || view.Request.Detail != "detailed" || view.Request.Policy != nil {
		t.Fatalf("detail contract lost: %d %+v", accepted.StatusCode, view)
	}
	var trace string
	if err := a.Store.Pool.QueryRow(context.Background(), "SELECT http_trace_id FROM generation_requests WHERE id=$1", view.Request.ID).Scan(&trace); err != nil || trace != accepted.Header.Get("X-Request-ID") {
		t.Fatal("creation trace not durable", err)
	}
	r, err := a.Store.Request(context.Background(), "foreign-session", view.Request.ID)
	_ = r
	if err == nil {
		t.Fatal("detail request lost anonymous isolation")
	}
}
