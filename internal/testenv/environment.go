// Package testenv makes integration dependencies explicit without importing the
// application packages that use the test helpers.
package testenv

import (
	"os"
	"testing"
)

// Require excludes integration work in short mode. Full verification sets
// VIDLENS_TEST_STRICT so a missing dependency fails instead of silently skipping.
func Require(t testing.TB, name string) string {
	t.Helper()
	if testing.Short() {
		t.Skip("integration: excluded by -short")
	}
	value := os.Getenv(name)
	if value == "" {
		if os.Getenv("VIDLENS_TEST_STRICT") == "1" {
			t.Fatalf("integration dependency is required: %s", name)
		}
		t.Skipf("integration: set %s to an isolated test dependency", name)
	}
	return value
}
