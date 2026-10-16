package outbound

import (
	"context"
	"net/netip"
	"testing"
)

func TestRejectsSSRFAndPrivateAddressEncodings(t *testing.T) {
	for _, url := range []string{"http://127.0.0.1/", "http://[::1]/", "http://[::ffff:127.0.0.1]/", "http://169.254.169.254/latest/meta-data/", "http://10.0.0.1/", "http://100.64.1.2/", "https://localhost/", "https://internal.local/", "https://example.com:8080/", "https://user:pass@example.com/", "file:///etc/passwd", "http://2130706433/", "http://0x7f000001/"} {
		if _, e := Validate(url, nil, false); e == nil {
			t.Fatalf("unsafe URL accepted: %s", url)
		}
	}
	for _, ip := range []string{"127.0.0.1", "::1", "::ffff:10.0.0.1", "169.254.169.254", "100.64.0.1", "192.168.0.1", "2001:db8::1", "64:ff9b::7f00:1"} {
		if publicIP(netip.MustParseAddr(ip)) {
			t.Fatalf("private/reserved IP accepted: %s", ip)
		}
	}
	if _, e := Validate("https://example.com/read?q=video", nil, false); e != nil {
		t.Fatal(e)
	}
	// Even a syntactically accepted public hostname is checked at actual dial.
	tr := Transport(nil)
	if c, e := tr.DialContext(context.Background(), "tcp", "127.0.0.1:80"); e == nil {
		c.Close()
		t.Fatal("dialer reached loopback")
	}
}
