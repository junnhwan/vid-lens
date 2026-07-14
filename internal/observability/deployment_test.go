package observability

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestDeploymentHasThreeDistinctProvisionedDashboardsWithoutOCR(t *testing.T) {
	root := filepath.Join("..", "..")
	files := []string{"task-overview.json", "ai-usage.json", "rag-multimodal.json"}
	uids := map[string]bool{}
	for _, name := range files {
		path := filepath.Join(root, "deploy", "grafana", "dashboards", name)
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		var dashboard struct {
			UID    string            `json:"uid"`
			Title  string            `json:"title"`
			Panels []json.RawMessage `json:"panels"`
		}
		if err := json.Unmarshal(raw, &dashboard); err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		if dashboard.UID == "" || dashboard.Title == "" || len(dashboard.Panels) == 0 {
			t.Fatalf("incomplete dashboard %s: %+v", name, dashboard)
		}
		if uids[dashboard.UID] {
			t.Fatalf("duplicate uid %q", dashboard.UID)
		}
		uids[dashboard.UID] = true
		if name == "rag-multimodal.json" && strings.Contains(string(raw), "vidlens_ocr_") {
			t.Fatalf("Phase 2 dashboard contains premature OCR metrics")
		}
	}

	for _, path := range []string{
		filepath.Join(root, "deploy", "prometheus", "prometheus.yml"),
		filepath.Join(root, "deploy", "grafana", "provisioning", "datasources", "prometheus.yml"),
		filepath.Join(root, "deploy", "grafana", "provisioning", "dashboards", "dashboards.yml"),
	} {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		var doc any
		if err := yaml.Unmarshal(raw, &doc); err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
	}
	compose, err := os.ReadFile(filepath.Join(root, "docker-compose.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(compose), "observability") || !strings.Contains(string(compose), "prometheus") || !strings.Contains(string(compose), "grafana") {
		t.Fatal("compose does not include observability profile")
	}
}
