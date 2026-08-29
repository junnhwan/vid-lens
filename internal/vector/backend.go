package vector

import "strings"

const DefaultBackend = "pgvector"

// NormalizeBackendName is the single boundary for selecting a vector backend.
// Empty configuration follows the PostgreSQL + pgvector application default;
// the temporary Milvus rollback path must always be selected explicitly.
func NormalizeBackendName(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return DefaultBackend
	}
	return name
}
