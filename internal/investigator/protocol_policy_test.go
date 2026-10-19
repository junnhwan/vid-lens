package investigator

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"testing"
	"vidlens/internal/domain"
)

func toolProtocolHash(t *testing.T, rev int, question bool) string {
	p, _ := domain.InterpretationPolicy("standard")
	s := &session{spec: domain.GenerationSpec{DetailPolicy: &p}, cp: checkpoint{HarnessRevision: rev, LayoutVersion: draftLayout}}
	if question {
		s.turn = &domain.Turn{}
	}
	tools, err := s.makeTools()
	if err != nil {
		t.Fatal(err)
	}
	values := map[string]any{}
	for _, tool := range tools {
		info, err := tool.Info(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		params, err := info.ParamsOneOf.ToJSONSchema()
		if err != nil {
			t.Fatal(err)
		}
		values[info.Name] = map[string]any{"description": info.Desc, "parameters": params}
	}
	hash := sha256.Sum256(domain.JSON(values))
	return hex.EncodeToString(hash[:])
}

func TestFrozenProtocolInputsAndToolSchemas(t *testing.T) {
	var cases []struct {
		Kind     string `json:"kind"`
		Revision int    `json:"revision"`
		Question bool   `json:"question"`
		SHA256   string `json:"sha256"`
		Level    string `json:"level"`
		Policy   string `json:"policy"`
	}
	raw, err := os.ReadFile("testdata/protocol-golden.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		var actual string
		if c.Kind == "review_text_prompt_v4" {
			hash := sha256.Sum256([]byte(evidenceReviewerTextInstructionsV4))
			actual = hex.EncodeToString(hash[:])
		} else if c.Kind == "review_visual_prompt_v4" {
			hash := sha256.Sum256([]byte(evidenceReviewerVisualInstructionsV4))
			actual = hex.EncodeToString(hash[:])
		} else if c.Kind == "review_text_prompt_v3" {
			hash := sha256.Sum256([]byte(evidenceReviewerTextInstructionsV3))
			actual = hex.EncodeToString(hash[:])
		} else if c.Kind == "review_visual_prompt_v3" {
			hash := sha256.Sum256([]byte(evidenceReviewerVisualInstructionsV3))
			actual = hex.EncodeToString(hash[:])
		} else if c.Kind == "review_prompt_v2" {
			hash := sha256.Sum256([]byte(evidenceReviewerInstructionsV2))
			actual = hex.EncodeToString(hash[:])
		} else if c.Kind == "review_prompt" {
			if c.Revision != 15 && c.Revision != 16 {
				t.Fatal("unknown independent review prompt revision")
			}
			hash := sha256.Sum256([]byte(evidenceReviewerInstructions))
			actual = hex.EncodeToString(hash[:])
		} else if c.Kind == "tools" {
			actual = toolProtocolHash(t, c.Revision, c.Question)
		} else {
			p, _ := domain.InterpretationPolicy(c.Level)
			p.Revision = c.Policy
			spec := domain.GenerationSpec{Version: 2, DetailPolicy: &p}
			if c.Policy == "none" {
				spec.DetailPolicy = nil
			}
			layout := draftLayout
			if c.Question {
				layout = "source-context-v2"
			}
			revision, prompt := initialProtocol(spec, c.Question, layout, true, 0)
			if revision != c.Revision {
				t.Fatalf("%s/%s question=%v: protocol revision changed", c.Level, c.Policy, c.Question)
			}
			hash := sha256.Sum256([]byte(prompt))
			actual = hex.EncodeToString(hash[:])
		}
		if actual != c.SHA256 {
			t.Fatalf("%s level=%s policy=%s revision=%d question=%v: frozen protocol changed", c.Kind, c.Level, c.Policy, c.Revision, c.Question)
		}
	}
}
func TestExistingCheckpointProtocolIsNotUpgraded(t *testing.T) {
	p, _ := domain.InterpretationPolicy("detailed")
	for revision := 0; revision <= 16; revision++ {
		spec := domain.GenerationSpec{Version: 2, DetailPolicy: &p}
		actual, _ := initialProtocol(spec, false, draftLayout, false, revision)
		if actual != revision {
			t.Fatalf("checkpoint %d upgraded to %d", revision, actual)
		}
	}
	s := &session{cp: checkpoint{HarnessRevision: 3, Calls: 9, Repairs: 2, Progress: progressState{Stalled: 4}}}
	before := domain.JSON(s.cp)
	if !s.policy().ContentPlan || s.policy().Recovery {
		t.Fatal("historical checkpoint policy changed")
	}
	if string(before) != string(domain.JSON(s.cp)) {
		t.Fatal("resolving policy mutated checkpoint counters")
	}
}
