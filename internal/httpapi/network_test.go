package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestNetworkSettingsApplyImmediatelyAndNeverReturnKey(t *testing.T) {
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
	body := `{"reference_read_enabled":true,"search_enabled":true,"daily_limit":20,"monthly_limit":1000,"api_key":"tvly-private-fixture","expected_revision":0}`
	if code, _ := call("PUT", "/api/v1/admin/network-settings", body, false); code != 401 {
		t.Fatal("public write accepted")
	}
	code, b := call("PUT", "/api/v1/admin/network-settings", body, true)
	if code != 200 {
		t.Fatalf("save %d: %s", code, b)
	}
	if strings.Contains(string(b), "tvly-private-fixture") || strings.Contains(string(b), "key_ref") {
		t.Fatal("key exposed on save")
	}
	var result struct {
		Revision      int64 `json:"revision"`
		KeyConfigured bool  `json:"key_configured"`
		DailyLimit    int   `json:"daily_limit"`
	}
	if e := json.Unmarshal(b, &result); e != nil || !result.KeyConfigured || result.DailyLimit != 20 || result.Revision != 1 {
		t.Fatalf("bad response: %s", b)
	}
	code, b = call("GET", "/api/v1/admin/network-settings", "", true)
	if code != 200 || strings.Contains(string(b), "tvly-private-fixture") {
		t.Fatal("GET exposed key or failed")
	}
	code, b = call("POST", "/api/v1/session", "{}", false)
	var session struct {
		Capabilities struct {
			External map[string]bool `json:"external_sources"`
		} `json:"capabilities"`
	}
	if e := json.Unmarshal(b, &session); e != nil || !session.Capabilities.External["search_enabled"] {
		t.Fatal("visitor did not receive live configuration")
	}
	if code, _ = call("PUT", "/api/v1/admin/network-settings", body, true); code != 409 {
		t.Fatal("stale overwrite accepted")
	}
	body = `{"reference_read_enabled":false,"search_enabled":false,"daily_limit":20,"monthly_limit":1000,"clear_key":true,"expected_revision":1}`
	if code, _ = call("PUT", "/api/v1/admin/network-settings", body, true); code != 200 {
		t.Fatal("cannot disable/clear")
	}
	_, b = call("POST", "/api/v1/session", "{}", false)
	_ = json.Unmarshal(b, &session)
	if session.Capabilities.External["enabled"] || session.Capabilities.External["search_enabled"] {
		t.Fatal("disable required restart")
	}
}
