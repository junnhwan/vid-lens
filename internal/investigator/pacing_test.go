package investigator

import (
	"testing"

	"github.com/cloudwego/eino/schema"
	"vidlens/internal/domain"
	"vidlens/internal/store"
)

func TestWrapUpAccountsForSlowResponsesAndReservedCalls(t *testing.T) {
	s := &session{spec: domain.GenerationSpec{DetailLevel: "standard"}}
	s.profile.MaxIterations = 40
	if s.wrapUp(store.ModelExecutionStats{ResponseMS: 239999}) {
		t.Fatal("closed early")
	}
	if !s.wrapUp(store.ModelExecutionStats{ResponseMS: 240000}) {
		t.Fatal("slow provider waits ignored")
	}
	s.cp.Calls = 6
	if !s.wrapUp(store.ModelExecutionStats{}) {
		t.Fatal("investigation calls ignored")
	}
	s.spec.DetailLevel = "detailed"
	if s.wrapUp(store.ModelExecutionStats{}) {
		t.Fatal("detailed policy ignored")
	}
	s.cp.Calls = 32
	if !s.wrapUp(store.ModelExecutionStats{}) {
		t.Fatal("writing reserve exhausted")
	}
}

func TestPhaseSchemasKeepEvidenceAndBoundDraftTools(t *testing.T) {
	in := []*schema.ToolInfo{}
	for _, name := range []string{"record_observation", "inspect_video_window", "begin_article", "repair_article_plan", "save_article_sections", "submit_article_draft", "submit_article"} {
		in = append(in, &schema.ToolInfo{Name: name})
	}
	s := &session{cp: checkpoint{HarnessRevision: 4}}
	names := func() map[string]bool {
		out := map[string]bool{}
		for _, x := range s.phaseTools(in) {
			out[x.Name] = true
		}
		return out
	}
	initial := names()
	if !initial["record_observation"] || !initial["inspect_video_window"] || !initial["begin_article"] || initial["submit_article"] || initial["save_article_sections"] {
		t.Fatal(initial)
	}
	s.cp.Composing = true
	composition := names()
	if !composition["save_article_sections"] || !composition["submit_article_draft"] || !composition["inspect_video_window"] || composition["begin_article"] {
		t.Fatal(composition)
	}
	s.cp.HarnessRevision = 3
	if len(s.phaseTools(in)) != len(in) {
		t.Fatal("historical checkpoint tool set changed")
	}
}
