package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestLoadServerConfigRejectsInvalidConfigurationBeforeStartup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("server:\n  port: 70000\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := loadServerConfig(path)
	if err == nil || !strings.Contains(err.Error(), "server.port") {
		t.Fatalf("loadServerConfig() error = %v, want server.port validation", err)
	}
}

func TestProjectServerConfigLoadsAndValidates(t *testing.T) {
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller() could not locate test file")
	}

	configPath := filepath.Clean(filepath.Join(filepath.Dir(currentFile), "..", "..", "config.yaml"))
	if _, err := loadServerConfig(configPath); err != nil {
		t.Fatalf("loadServerConfig(%q) error = %v", configPath, err)
	}
}
