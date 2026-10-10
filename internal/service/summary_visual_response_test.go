package service

import (
	"context"
	"strings"
	"testing"

	"vid-lens/internal/artifact"
	"vid-lens/internal/model"
)

func TestSummaryVisualFormatRepairIsBoundedDurableAndPrivate(t *testing.T) {
	for _, repairFails := range []bool{false, true} {
		f := newGenerationFixture(t, false)
		v := enableGenerationVisualFixture(t, f)
		invalid := `{"targets":[{"block_id":"foreign","cue_id":"foreign","goal":"wrong"}],"unexpected":"fixture-private-value"}`
		second := `{"public_title":"无需配图","reason":"没有视觉增益","targets":[]}`
		if repairFails {
			second = "```json\n{}\n```"
		}
		v.planResponses = []string{invalid, second}
		if err := f.svc.Generate(context.Background(), f.task, f.job, f.job.ProcessingToken); err != nil {
			t.Fatal(err)
		}
		if v.chatCalls != 2 || v.inspectCalls != 0 {
			t.Fatalf("calls=%d inspect=%d", v.chatCalls, v.inspectCalls)
		}
		records, err := f.repos.AgentExecution.GetExecution(context.Background(), 7, f.job.GenerationID)
		if err != nil || records.Run.Status != model.AgentRunStatusCompleted {
			t.Fatalf("execution=%+v err=%v", records, err)
		}
		invalidSteps := 0
		for _, step := range records.Steps {
			if strings.Contains(step.ResultCheckpoint, "fixture-private-value") {
				t.Fatal("provider response leaked into durable diagnostics")
			}
			var failure summaryVisualInvalidCheckpoint
			if artifact.Decode([]byte(step.ResultCheckpoint), &failure) == nil && failure.Invalid {
				invalidSteps++
				if failure.Kind == "" || failure.Bytes == 0 {
					t.Fatal("missing format diagnosis")
				}
			}
		}
		want := 1
		if repairFails {
			want = 2
		}
		if invalidSteps != want {
			t.Fatalf("invalid steps=%d want=%d", invalidSteps, want)
		}
		if err := f.svc.Generate(context.Background(), f.task, f.job, f.job.ProcessingToken); err != nil {
			t.Fatal(err)
		}
		if v.chatCalls != 2 || v.inspectCalls != 0 {
			t.Fatal("delivery replay repeated paid repair")
		}
	}
}

func TestSummaryVisualFormatDiagnosticsPreserveStrictContract(t *testing.T) {
	cases := []struct{ raw, kind string }{
		{"```json\n{}\n```", "syntax"},
		{`{"targets":[],"extra":true}`, "unknown_field"},
		{`{"targets":"wrong-type"}`, "field_type"},
		{`{} {}`, "trailing_data"},
		{strings.Repeat(" ", 65537), "oversize"},
	}
	for _, tc := range cases {
		candidate, failure := decodeSummaryVisualResponse(tc.raw, &summaryVisualPlan{})
		if candidate != nil || failure == nil || failure.Kind != tc.kind {
			t.Fatalf("kind=%s failure=%+v", tc.kind, failure)
		}
	}
	old := &summaryVisualPlan{Targets: []summaryVisualTarget{{BlockID: "prior-invalid"}}}
	value, failure := decodeSummaryVisualResponse(`{"targets":[]}`, old)
	if failure != nil || len(value.(*summaryVisualPlan).Targets) != 0 || old.Targets[0].BlockID != "prior-invalid" {
		t.Fatal("decode reused partially mutated state")
	}
}

func TestSummaryVisualNormalizesOnlyNamedFactLeaves(t *testing.T) {
	f := newGenerationFixture(t, false)
	v := enableGenerationVisualFixture(t, f)
	v.planResponses = []string{`{"public_title":"核对配置","targets":[{"block_id":"block-cue-a","cue_id":"cue-a","goal":"检查参数","required_facts":["最大连接数",{"name":"超时"}]}]}`}
	if err := f.svc.Generate(context.Background(), f.task, f.job, f.job.ProcessingToken); err != nil {
		t.Fatal(err)
	}
	if v.visualError != nil || v.inspectCalls != 1 || len(v.requests[0].RequiredFacts) != 2 || v.requests[0].RequiredFacts[0].Name != "最大连接数" {
		t.Fatalf("error=%v requests=%+v", v.visualError, v.requests)
	}
	for _, raw := range []string{`{"targets":[{"required_facts":[{"name":"x","extra":true}]}]}`, `{"targets":[{"required_facts":[""]}]}`, `{"targets":[{"required_facts":[9]}]}`} {
		if value, failure := decodeSummaryVisualResponse(raw, &summaryVisualPlan{}); failure == nil || value != nil {
			t.Fatal("invalid leaf accepted")
		}
	}
}
