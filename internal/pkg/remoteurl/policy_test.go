package remoteurl

import (
	"context"
	"net"
	"strings"
	"testing"
)

type testResolver map[string][]net.IP

func (r testResolver) LookupIP(context.Context, string) ([]net.IP, error) {
	return r["example.com"], nil
}

func TestPolicyRejectsPrivateResolutionAndSanitizesAllowedURL(t *testing.T) {
	policy := NewPolicy([]string{"example.com"}, testResolver{"example.com": {net.ParseIP("192.0.2.10")}})
	checked, err := policy.Validate(context.Background(), "https://example.com/video?token=secret#fragment")
	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if checked.Host != "example.com" || strings.Contains(checked.Sanitized, "token") || strings.Contains(checked.Sanitized, "fragment") {
		t.Fatalf("checked = %+v, want sanitized allowed URL", checked)
	}

	policy = NewPolicy([]string{"example.com"}, testResolver{"example.com": {net.ParseIP("10.0.0.1")}})
	if _, err := policy.Validate(context.Background(), "https://example.com/video"); err == nil || !strings.Contains(err.Error(), "内网") {
		t.Fatalf("Validate() error = %v, want private-address rejection", err)
	}
}
