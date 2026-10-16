package store

import (
	"context"
	"strings"
	"testing"
)

func TestOwnerPasswordLengthBounds(t *testing.T) {
	for _, tc := range []struct {
		name   string
		length int
		valid  bool
	}{
		{"empty", 0, false},
		{"seven_bytes", 7, false},
		{"eight_bytes", 8, true},
		{"maximum", 512, true},
		{"over_maximum", 513, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := testStore(t)
			ctx := context.Background()
			password := strings.Repeat("p", tc.length)
			err := s.SetOwnerPassword(ctx, password)
			if !tc.valid {
				if err == nil {
					t.Fatal("out-of-bounds password accepted")
				}
				var count int
				if err := s.Pool.QueryRow(ctx, "SELECT count(*) FROM owner_auth").Scan(&count); err != nil || count != 0 {
					t.Fatalf("rejected password changed owner credentials: count=%d error=%v", count, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			var hash string
			if err := s.Pool.QueryRow(ctx, "SELECT password_hash FROM owner_auth").Scan(&hash); err != nil {
				t.Fatal(err)
			}
			if !s.VerifyOwnerPassword(password, hash) {
				t.Fatal("accepted password cannot authenticate")
			}
		})
	}
}
