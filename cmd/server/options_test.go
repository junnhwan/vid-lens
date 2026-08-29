package main

import "testing"

func TestParseServerOptionsUsesExplicitConfigPath(t *testing.T) {
	opts, err := parseServerOptions([]string{"--config", ".logs/config.pgvector.yaml"})
	if err != nil {
		t.Fatalf("parseServerOptions returned error: %v", err)
	}
	if got, want := opts.configPath, ".logs/config.pgvector.yaml"; got != want {
		t.Fatalf("config path = %q, want %q", got, want)
	}
}

func TestParseServerOptionsDefaultsToConfigYAML(t *testing.T) {
	opts, err := parseServerOptions(nil)
	if err != nil {
		t.Fatalf("parseServerOptions returned error: %v", err)
	}
	if got, want := opts.configPath, "config.yaml"; got != want {
		t.Fatalf("config path = %q, want %q", got, want)
	}
}
