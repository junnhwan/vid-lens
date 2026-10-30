package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"vidlens/internal/domain"
	"vidlens/internal/testsupport"
)

func TestHTTPVideoConversationsAreIdempotentAndPrivate(t *testing.T) {
	server, a := apiTest(t)
	ctx := context.Background()
	turn, _ := testsupport.Question(t, a.Store)
	var owner string
	if err := a.Store.Pool.QueryRow(ctx, "SELECT session_id FROM qa_conversations WHERE id=$1", turn.ConversationID).Scan(&owner); err != nil {
		t.Fatal(err)
	}
	// Give the isolated fixture owner a known cookie. No live visitor is used.
	token := "fixture-conversation-owner-cookie"
	if _, err := a.Store.Pool.Exec(ctx, "UPDATE anonymous_sessions SET token_hash=$2 WHERE id=$1", owner, hashToken(token)); err != nil {
		t.Fatal(err)
	}
	foreign := "fixture-foreign-cookie"
	if _, err := a.Store.NewSession(ctx, hashToken(foreign), time.Hour); err != nil {
		t.Fatal(err)
	}
	do := func(method, path, key, cookie string, csrf bool) (int, http.Header, map[string]json.RawMessage) {
		t.Helper()
		req, _ := http.NewRequest(method, server.URL+path, strings.NewReader("{}"))
		req.AddCookie(&http.Cookie{Name: cookieName, Value: cookie})
		req.Header.Set("Origin", a.Config.Origin)
		req.Header.Set("Content-Type", "application/json")
		if csrf {
			req.Header.Set("X-CSRF-Token", a.csrf(cookie))
		}
		if key != "" {
			req.Header.Set("Idempotency-Key", key)
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var body map[string]json.RawMessage
		if err = json.NewDecoder(res.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		return res.StatusCode, res.Header, body
	}
	path := "/api/v1/requests/" + turn.RequestID + "/conversations"
	status, _, _ := do("POST", path, "missing-csrf", token, false)
	if status != 403 {
		t.Fatal("creation skipped CSRF", status)
	}
	status, headers, body := do("POST", path, "new", token, true)
	if status != 201 || headers.Get("Location") == "" {
		t.Fatal(status, body)
	}
	var conversation domain.Conversation
	if err := json.Unmarshal(body["conversation"], &conversation); err != nil || conversation.ID == turn.ConversationID || conversation.SessionID != "" || conversation.NextTurnNo != 0 {
		t.Fatal("private or incorrect response", conversation, err)
	}
	status, headers, body = do("POST", path, "new", token, true)
	if status != 200 || headers.Get("Idempotency-Replayed") != "true" {
		t.Fatal("retry created a second thread", status, body)
	}
	status, _, body = do("GET", path, "", token, false)
	var items []json.RawMessage
	_ = json.Unmarshal(body["items"], &items)
	if status != 200 || len(items) != 2 {
		t.Fatal("history missing", status, body)
	}
	for _, target := range []string{path, "/api/v1/conversations/" + conversation.ID, "/api/v1/conversations/" + conversation.ID + "/turns"} {
		status, _, body = do("GET", target, "", foreign, false)
		if status != 404 {
			t.Fatal("foreign history visible", target, status, body)
		}
	}
	status, _, body = do("POST", path, "foreign-create", foreign, true)
	if status != 404 {
		t.Fatal("foreign creation allowed", status, body)
	}
}
