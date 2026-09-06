package model

import "testing"

func TestSanitizeVideoTitle(t *testing.T) {
	longRunes := make([]rune, 80)
	for i := range longRunes {
		longRunes[i] = '题'
	}
	long := string(longRunes)
	wantLong := string(longRunes[:60])
	cases := []struct {
		in, want string
	}{
		{"  正确标题  ", "正确标题"},
		{"\"带引号\"", "带引号"},
		{"'单引号'", "单引号"},
		{"第一行\n第二行", "第一行 第二行"},
		{"   ", ""},
		{long, wantLong},
	}
	for _, tc := range cases {
		if got := SanitizeVideoTitle(tc.in); got != tc.want {
			t.Fatalf("SanitizeVideoTitle(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
