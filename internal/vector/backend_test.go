package vector

import "testing"

func TestNormalizeBackendName(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "empty uses pgvector application default", input: "", want: "pgvector"},
		{name: "trims and lowercases", input: "  PGVECTOR ", want: "pgvector"},
		{name: "keeps unknown names for validation", input: "elastic-vector", want: "elastic-vector"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NormalizeBackendName(tt.input); got != tt.want {
				t.Fatalf("NormalizeBackendName(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
