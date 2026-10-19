package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHostConcurrencyRetainsSingleSlotAndLoadsExplicitLimit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	base := "database_url: postgres://unused\ndata_dir: /tmp/vidlens-config-test-data\nadmin_token: " + strings.Repeat("a", 32) + "\nsecret_key: " + strings.Repeat("s", 32) + "\n"
	if err := os.WriteFile(path, []byte(base), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("VIDLENS_VIDEO_CONCURRENCY", "")
	c, err := LoadFile(path)
	if err != nil || c.VideoSlots() != 1 {
		t.Fatalf("legacy config: %+v %v", c, err)
	}
	t.Setenv("VIDLENS_VIDEO_CONCURRENCY", "10")
	c, err = LoadFile(path)
	if err != nil || c.VideoSlots() != 10 {
		t.Fatalf("ten slots: %+v %v", c, err)
	}
	for _, value := range []string{"0", "-1", "65", "many"} {
		t.Setenv("VIDLENS_VIDEO_CONCURRENCY", value)
		if _, err := LoadFile(path); err == nil {
			t.Fatalf("accepted invalid concurrency %q", value)
		}
	}
}
