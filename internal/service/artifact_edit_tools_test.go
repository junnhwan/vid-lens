package service

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"vid-lens/internal/artifact"
	"vid-lens/internal/model"
)

func TestArtifactEditToolRegistryModeAllowLists(t *testing.T) {
	t.Parallel()
	tests := []struct {
		mode ArtifactEditMode
		want []string
	}{
		{ArtifactEditModeAnswer, []string{
			ArtifactEditToolAnswerQuestion,
			ArtifactEditToolFindBlocks,
			ArtifactEditToolInspectEvidence,
			ArtifactEditToolRead,
		}},
		{ArtifactEditModePreview, []string{
			ArtifactEditToolAnswerQuestion,
			ArtifactEditToolFindBlocks,
			ArtifactEditToolInspectEvidence,
			ArtifactEditToolNothingToChange,
			ArtifactEditToolProposePatch,
			ArtifactEditToolRead,
		}},
		{ArtifactEditModeApply, []string{
			ArtifactEditToolAnswerQuestion,
			ArtifactEditToolCommitPatch,
			ArtifactEditToolFindBlocks,
			ArtifactEditToolInspectEvidence,
			ArtifactEditToolNothingToChange,
			ArtifactEditToolProposePatch,
			ArtifactEditToolRead,
		}},
	}
	for _, test := range tests {
		test := test
		t.Run(string(test.mode), func(t *testing.T) {
			t.Parallel()
			registry, digest, err := NewArtifactEditToolRegistry(test.mode)
			if err != nil {
				t.Fatal(err)
			}
			var got []string
			for _, definition := range registry.Definitions() {
				got = append(got, definition.Name)
				if len(definition.InputSchema) == 0 || !json.Valid(definition.InputSchema) {
					t.Fatalf("tool %s has invalid schema %q", definition.Name, definition.InputSchema)
				}
			}
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("tools = %v, want %v", got, test.want)
			}
			if len(digest) != 64 || digest != ArtifactEditToolSchemaDigest(registry.Definitions()) {
				t.Fatalf("digest = %q", digest)
			}
		})
	}
}

func TestDefaultVideoAgentRegistryNeverContainsArtifactWriteTools(t *testing.T) {
	t.Parallel()
	registry := newVideoAgentToolRegistry(&VideoAgentTools{})
	for _, name := range []string{ArtifactEditToolProposePatch, ArtifactEditToolCommitPatch} {
		if _, err := registry.Lookup(name); err == nil {
			t.Fatalf("default video registry unexpectedly contains %s", name)
		}
	}
}

func TestArtifactEditProposalSchemaDescribesEveryClosedOperation(t *testing.T) {
	t.Parallel()
	registry, _, err := NewArtifactEditToolRegistry(ArtifactEditModeApply)
	if err != nil {
		t.Fatal(err)
	}
	tool, err := registry.Lookup(ArtifactEditToolProposePatch)
	if err != nil {
		t.Fatal(err)
	}
	var schema map[string]any
	if err = json.Unmarshal(tool.Definition().InputSchema, &schema); err != nil {
		t.Fatal(err)
	}
	properties := schema["properties"].(map[string]any)
	patch := properties["patch"].(map[string]any)
	patchProperties := patch["properties"].(map[string]any)
	operations := patchProperties["operations"].(map[string]any)
	items := operations["items"].(map[string]any)
	oneOf := items["oneOf"].([]any)
	if len(oneOf) != 7 {
		t.Fatalf("operation variants=%d want=7", len(oneOf))
	}
	for index, raw := range oneOf {
		variant := raw.(map[string]any)
		if additional, ok := variant["additionalProperties"].(bool); !ok || additional {
			t.Fatalf("operation variant %d is not closed: %#v", index, variant)
		}
		if len(variant["required"].([]any)) == 0 {
			t.Fatalf("operation variant %d has no required fields", index)
		}
	}
}

func TestArtifactEditToolScopeValidationReceivesCallerContext(t *testing.T) {
	t.Parallel()
	registry, _, err := NewArtifactEditToolRegistry(ArtifactEditModeAnswer)
	if err != nil {
		t.Fatal(err)
	}
	runtime := fixtureArtifactEditRuntime()
	runtime.ValidateScope = func(ctx context.Context) error { return ctx.Err() }
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = registry.Execute(ctx, ArtifactEditToolRead, VideoAgentToolRequest{Runtime: VideoAgentToolRuntime{ArtifactEdit: runtime}, Arguments: json.RawMessage(`{"limit":1}`)})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error=%v want context.Canceled", err)
	}
}

func TestArtifactEditReadFindAndEvidenceStayInsideFrozenScope(t *testing.T) {
	t.Parallel()
	root, other := "root", "other"
	runtime := &ArtifactEditToolRuntime{
		ArtifactID:       "artifact-1",
		BaseVersionID:    "version-6",
		BaseVersion:      6,
		ManifestID:       "manifest-1",
		SelectedBlockIDs: []string{"selected"},
		Body: artifact.Body{SchemaVersion: 1, Kind: "study", Title: "课程", Blocks: []artifact.Block{
			{BlockID: "root", Type: "section", Title: "根", Content: "范围外", ClaimOrigin: "source", EvidenceRefs: []artifact.Ref{{EvidenceID: "ev-root", Relation: "supports"}}},
			{BlockID: "selected", ParentID: &root, Type: "concept", Title: "安装", Content: "scoped models", ClaimOrigin: "source", EvidenceRefs: []artifact.Ref{{EvidenceID: "ev-selected", Relation: "supports"}}},
			{BlockID: "child", ParentID: stringPointer("selected"), Type: "note", Title: "命令", Content: "scoped-models", ClaimOrigin: "user", EvidenceRefs: []artifact.Ref{{EvidenceID: "ev-child", Relation: "context"}}},
			{BlockID: "other", ParentID: &other, Type: "note", Title: "其它", Content: "secret sibling", ClaimOrigin: "source", EvidenceRefs: []artifact.Ref{{EvidenceID: "ev-other", Relation: "supports"}}},
		}, Warnings: []string{}},
		Evidence: []model.SourceSnapshotItem{
			{ID: "ev-selected", ManifestID: "manifest-1", Content: "安装 scoped-models", Modality: model.ChunkModalityTranscript},
			{ID: "ev-child", ManifestID: "manifest-1", Content: "屏幕命令", Modality: model.ChunkModalityVisualOCR},
			{ID: "ev-other", ManifestID: "manifest-1", Content: "private sibling evidence", Modality: model.ChunkModalityTranscript},
			{ID: "ev-foreign", ManifestID: "manifest-2", Content: "foreign manifest", Modality: model.ChunkModalityTranscript},
		},
	}
	registry, _, err := NewArtifactEditToolRegistry(ArtifactEditModeAnswer)
	if err != nil {
		t.Fatal(err)
	}
	request := func(arguments string) VideoAgentToolRequest {
		return VideoAgentToolRequest{Runtime: VideoAgentToolRuntime{ArtifactEdit: runtime}, Arguments: json.RawMessage(arguments)}
	}

	read, err := registry.Execute(context.Background(), ArtifactEditToolRead, request(`{"limit":20}`))
	if err != nil {
		t.Fatal(err)
	}
	var readResult ArtifactEditReadResult
	if err = json.Unmarshal(read.Output, &readResult); err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, block := range readResult.Blocks {
		ids = append(ids, block.BlockID)
	}
	if !reflect.DeepEqual(ids, []string{"selected", "child"}) {
		t.Fatalf("read block ids = %v", ids)
	}

	found, err := registry.Execute(context.Background(), ArtifactEditToolFindBlocks, request(`{"query":"secret sibling","limit":10}`))
	if err != nil {
		t.Fatal(err)
	}
	var findResult ArtifactEditFindResult
	if err = json.Unmarshal(found.Output, &findResult); err != nil {
		t.Fatal(err)
	}
	if len(findResult.Matches) != 0 {
		t.Fatalf("out-of-scope find leaked blocks: %#v", findResult.Matches)
	}

	if _, err = registry.Execute(context.Background(), ArtifactEditToolInspectEvidence, request(`{"evidence_ids":["ev-foreign"]}`)); err == nil {
		t.Fatal("inspect accepted evidence outside the frozen manifest")
	}
	inspected, err := registry.Execute(context.Background(), ArtifactEditToolInspectEvidence, request(`{"evidence_ids":["ev-selected"]}`))
	if err != nil {
		t.Fatal(err)
	}
	var inspectResult ArtifactEditEvidenceResult
	if err = json.Unmarshal(inspected.Output, &inspectResult); err != nil {
		t.Fatal(err)
	}
	if len(inspectResult.Evidence) != 1 || inspectResult.Evidence[0].ID != "ev-selected" {
		t.Fatalf("evidence = %#v", inspectResult.Evidence)
	}
}

func stringPointer(value string) *string { return &value }
