package usertags

import (
	"strings"
	"testing"
)

func TestNormalizeUnicodeAndTechnicalPunctuation(t *testing.T) {
	for _, test := range []struct{ input, want string }{{"  Ｃ＋＋  ", "c++"}, {"C#", "c#"}, {".NET", ".net"}, {"  DATA\u00a0  Base ", "data base"}, {"Straße", "strasse"}} {
		_, key, err := Normalize(test.input, 80)
		if err != nil || key != test.want {
			t.Fatalf("normalize %q=%q,%v", test.input, key, err)
		}
	}
	for _, name := range []string{"", " \n\t", strings.Repeat("字", 81), "a\x00b"} {
		if _, _, err := Normalize(name, 80); err == nil {
			t.Fatalf("accepted invalid %q", name)
		}
	}
	if _, _, err := Normalize(strings.Repeat("字", 80), 80); err != nil {
		t.Fatal(err)
	}
}
