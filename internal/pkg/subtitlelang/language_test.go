package subtitlelang

import "testing"

func TestDeclaredProviderLanguageAliases(t *testing.T) {
	for _, test := range []struct {
		actual, wanted string
		match          bool
	}{
		{"ai-zh", "zh-CN", true}, {"ai-en", "en-GB", true},
		{"auto-zh-Hans", "zh_CN", true}, {"AUTO-en-US", "en", true},
		{"ai-en", "zh-CN", false}, {"ai-zh", "en", false},
		{"ai-unknown", "zh", false}, {"auto-und", "zh", false},
		{"ai-ja", "ja", false}, {"ai-", "zh", false},
		{"ai-zh-?", "zh", false}, {"unknown", "zh", false},
		{"zh-Hant", "zh-CN", true}, {"", "zh", false},
	} {
		if got := Matches(test.actual, test.wanted); got != test.match {
			t.Errorf("%q against %q = %t, want %t", test.actual, test.wanted, got, test.match)
		}
	}
}
