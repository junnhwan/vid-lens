package remoteurl

import (
	"context"
	"net"
	"net/url"
	"strings"
	"testing"
)

type testResolver map[string][]net.IP

func (r testResolver) LookupIP(context.Context, string) ([]net.IP, error) {
	return r["example.com"], nil
}

func TestBilibiliPartRetainedAndValidated(t *testing.T) {
	policy := NewPolicy(nil, testResolver{"example.com": {net.ParseIP("8.8.8.8")}})
	for _, test := range []struct{ raw, want string }{
		{"https://www.bilibili.com/video/BV1xx411c7mD?p=2&share_source=copy#tracking", "https://www.bilibili.com/video/BV1xx411c7mD?p=2"},
		{"https://www.bilibili.com/video/BV1xx411c7mD?p=02&p=2", "https://www.bilibili.com/video/BV1xx411c7mD?p=2"},
		{"https://www.bilibili.com/video/BV1xx411c7mD?share_medium=web", "https://www.bilibili.com/video/BV1xx411c7mD"},
		{"https://b23.tv/short?p=3&tracking=x", "https://b23.tv/short?p=3"},
		{"https://www.youtube.com/watch?v=video&p=2&utm_source=x", "https://www.youtube.com/watch?v=video"},
	} {
		checked, err := policy.Validate(context.Background(), test.raw)
		if err != nil || checked.Sanitized != test.want {
			t.Fatalf("%s => %+v %v", test.raw, checked, err)
		}
	}
	for _, query := range []string{"p=0", "p=-1", "p=1.5", "p=", "p=1&p=2", "p=9999999999999999999999999", "p=+2", "p=%ZZ", "p=1;bad=x"} {
		raw := "https://www.bilibili.com/video/BV1xx411c7mD?" + query
		if _, err := policy.Validate(context.Background(), raw); err == nil {
			t.Fatalf("accepted invalid query %s", query)
		}
		parsed, _ := url.Parse(raw)
		if clean := Sanitize(*parsed); clean != "" {
			t.Fatalf("invalid sanitization produced %s", clean)
		}
	}
}

func TestPolicyRejectsPrivateResolutionAndSanitizesAllowedURL(t *testing.T) {
	policy := NewPolicy([]string{"example.com"}, testResolver{"example.com": {net.ParseIP("8.8.8.8")}})
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

func TestUnsafeIPRejectsNonPublicSpecialRanges(t *testing.T) {
	for _, raw := range []string{"100.100.100.200", "0.1.2.3", "198.18.0.1", "192.0.2.1", "255.255.255.255", "2001:db8::1", "::ffff:127.0.0.1"} {
		if !UnsafeIP(net.ParseIP(raw)) {
			t.Fatalf("non-public address accepted: %s", raw)
		}
	}
	for _, raw := range []string{"8.8.8.8", "1.1.1.1", "2606:4700:4700::1111"} {
		if UnsafeIP(net.ParseIP(raw)) {
			t.Fatalf("public address rejected: %s", raw)
		}
	}
}
