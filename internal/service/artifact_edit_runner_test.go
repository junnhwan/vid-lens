package service

import (
	"context"
	"encoding/json"
	"testing"

	"vid-lens/internal/ai"
	"vid-lens/internal/artifact"
	"vid-lens/internal/model"
	"vid-lens/internal/repository"
)

type artifactEditSequenceChat struct {
	responses []string
}

func (c *artifactEditSequenceChat) Chat(context.Context, []ai.ChatMessage) (string, error) {
	if len(c.responses) == 0 {
		return "", nil
	}
	response := c.responses[0]
	c.responses = c.responses[1:]
	return response, nil
}

func TestArtifactEditRunnerAnswerCreatesNoProposalOrVersionAndRecordsTools(t *testing.T) {
	t.Parallel()
	registry, digest, err := NewArtifactEditToolRegistry(ArtifactEditModeAnswer)
	if err != nil {
		t.Fatal(err)
	}
	store := &scriptedAgentExecutionStore{claims: []repository.AgentStepClaim{{Outcome: repository.AgentStepClaimAcquired}, {Outcome: repository.AgentStepClaimAcquired}}}
	planner := NewLLMArtifactEditPlanner(&artifactEditSequenceChat{responses: []string{
		`{"tool":"answer_artifact_question","reason":"普通询问","public_summary":"只回答，不修改","arguments":{"message":"当前内容没有说明该名称是否正确。","evidence_ids":[]}}`,
	}})
	runner, err := NewArtifactEditRunner(registry, planner, NewAgentExecutionJournal(store), 7, "edit-run-answer", 2, nil)
	if err != nil {
		t.Fatal(err)
	}
	runtime := fixtureArtifactEditRuntime()
	proposalCalls, commitCalls := 0, 0
	runtime.PersistProposal = func(context.Context, ArtifactEditProposal) (ArtifactEditProposal, error) {
		proposalCalls++
		return ArtifactEditProposal{}, nil
	}
	runtime.CommitProposal = func(context.Context, ArtifactEditProposal) (ArtifactEditCommitResult, error) {
		commitCalls++
		return ArtifactEditCommitResult{}, nil
	}
	state := fixtureArtifactEditPlannerState(t, runtime, registry, digest, ArtifactEditModeAnswer, "这个名称对吗？")
	outcome, err := runner.Run(context.Background(), state, runtime)
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Kind != "answer" || proposalCalls != 0 || commitCalls != 0 {
		t.Fatalf("outcome=%+v proposal_calls=%d commit_calls=%d", outcome, proposalCalls, commitCalls)
	}
	if len(store.completed) != 2 || len(store.claimInputs) != 2 || store.claimInputs[1].ToolName != ArtifactEditToolAnswerQuestion {
		t.Fatalf("journal claims=%+v completions=%+v", store.claimInputs, store.completed)
	}
}

func TestArtifactEditRunnerApplyPersistsProposalThenCommitsAndRecordsBothTools(t *testing.T) {
	t.Parallel()
	registry, digest, err := NewArtifactEditToolRegistry(ArtifactEditModeApply)
	if err != nil {
		t.Fatal(err)
	}
	runtime := fixtureArtifactEditRuntime()
	updated := "第一步安装。第二步验证。"
	patch := artifact.Patch{SchemaVersion: 1, ArtifactID: runtime.ArtifactID, BaseVersionID: runtime.BaseVersionID, BaseVersion: runtime.BaseVersion, Basis: artifact.PatchBasisUserInstruction, EvidenceIDs: []string{}, Operations: []artifact.PatchOperation{{Op: artifact.PatchOpUpdateBlock, BlockID: "install", ExpectedHash: artifact.BlockHash(runtime.Body.Blocks[0]), Content: &updated}}}
	patchJSON, _ := json.Marshal(patch)
	chat := &artifactEditSequenceChat{responses: []string{
		`{"tool":"propose_artifact_patch","reason":"明确要求拆分","public_summary":"生成受约束修改","arguments":{"summary":"拆成两个步骤","patch":` + string(patchJSON) + `}}`,
		`{"tool":"commit_artifact_patch","reason":"proposal 已验证","public_summary":"正在保存新版本","arguments":{}}`,
	}}
	store := &scriptedAgentExecutionStore{claims: []repository.AgentStepClaim{
		{Outcome: repository.AgentStepClaimAcquired}, {Outcome: repository.AgentStepClaimAcquired},
		{Outcome: repository.AgentStepClaimAcquired}, {Outcome: repository.AgentStepClaimAcquired},
	}}
	runner, err := NewArtifactEditRunner(registry, NewLLMArtifactEditPlanner(chat), NewAgentExecutionJournal(store), 7, "edit-run-apply", 4, nil)
	if err != nil {
		t.Fatal(err)
	}
	proposalCalls, commitCalls := 0, 0
	runtime.PersistProposal = func(_ context.Context, proposal ArtifactEditProposal) (ArtifactEditProposal, error) {
		proposalCalls++
		return proposal, nil
	}
	runtime.CommitProposal = func(_ context.Context, proposal ArtifactEditProposal) (ArtifactEditCommitResult, error) {
		commitCalls++
		return ArtifactEditCommitResult{OperationID: proposal.OperationID, ResultVersionID: "version-2"}, nil
	}
	state := fixtureArtifactEditPlannerState(t, runtime, registry, digest, ArtifactEditModeApply, "把安装部分拆成两个步骤")
	outcome, err := runner.Run(context.Background(), state, runtime)
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Kind != "committed" || outcome.ResultVersionID != "version-2" || proposalCalls != 1 || commitCalls != 1 {
		t.Fatalf("outcome=%+v proposal_calls=%d commit_calls=%d", outcome, proposalCalls, commitCalls)
	}
	if len(store.completed) != 4 || len(store.claimInputs) != 4 {
		t.Fatalf("journal claims=%d completions=%d", len(store.claimInputs), len(store.completed))
	}
	if store.claimInputs[1].ToolName != ArtifactEditToolProposePatch || store.claimInputs[3].ToolName != ArtifactEditToolCommitPatch {
		t.Fatalf("tool records = %s, %s", store.claimInputs[1].ToolName, store.claimInputs[3].ToolName)
	}
}

func fixtureArtifactEditRuntime() *ArtifactEditToolRuntime {
	body := artifact.Body{SchemaVersion: 1, Kind: "study", Title: "安装笔记", Blocks: []artifact.Block{{BlockID: "install", Type: "concept", Title: "安装", Content: "旧内容", ClaimOrigin: "source", EvidenceRefs: []artifact.Ref{{EvidenceID: "evidence-1", Relation: "supports"}}}}, Warnings: []string{}}
	return &ArtifactEditToolRuntime{
		ArtifactID: "artifact-1", BaseVersionID: "version-1", BaseVersion: 1, ManifestID: "manifest-1", OperationID: "operation-1",
		SelectedBlockIDs: []string{"install"}, Body: body,
		Evidence: []model.SourceSnapshotItem{{ID: "evidence-1", ManifestID: "manifest-1", Content: "安装证据", Modality: model.ChunkModalityTranscript}},
	}
}

func fixtureArtifactEditPlannerState(t *testing.T, runtime *ArtifactEditToolRuntime, registry *VideoAgentToolRegistry, digest string, mode ArtifactEditMode, instruction string) ArtifactEditPlannerState {
	t.Helper()
	digests, err := FreezeArtifactEditDigests(runtime.Body, runtime.SelectedBlockIDs, registry.Definitions())
	if err != nil {
		t.Fatal(err)
	}
	if digests.ToolSchema != digest {
		t.Fatalf("schema digest=%s want %s", digests.ToolSchema, digest)
	}
	return ArtifactEditPlannerState{
		ArtifactID: runtime.ArtifactID, BaseVersionID: runtime.BaseVersionID, BaseVersion: runtime.BaseVersion,
		SelectedBlockIDs: append([]string(nil), runtime.SelectedBlockIDs...), Instruction: instruction, Mode: mode,
		Intent: ClassifyArtifactEditIntent(instruction, mode), BaseDigest: digests.Base, ScopeDigest: digests.Scope, ToolSchemaDigest: digest,
	}
}
