package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"vid-lens/internal/artifact"
	"vid-lens/internal/model"
)

const ArtifactEditRecipe = "study-edit-v1"

type ArtifactEditMode string

const (
	ArtifactEditModeAnswer  ArtifactEditMode = "answer"
	ArtifactEditModePreview ArtifactEditMode = "preview"
	ArtifactEditModeApply   ArtifactEditMode = "apply"
)

const (
	ArtifactEditToolRead             = "read_artifact"
	ArtifactEditToolFindBlocks       = "find_artifact_blocks"
	ArtifactEditToolInspectEvidence  = "inspect_artifact_evidence"
	ArtifactEditToolAnswerQuestion   = "answer_artifact_question"
	ArtifactEditToolProposePatch     = "propose_artifact_patch"
	ArtifactEditToolNothingToChange  = "nothing_to_change"
	ArtifactEditToolCommitPatch      = "commit_artifact_patch"
	artifactEditMaxReadBlocks        = 50
	artifactEditMaxFindMatches       = 20
	artifactEditMaxEvidenceItems     = 20
	artifactEditMaxEvidenceTextRunes = 12000
)

// ArtifactEditToolRuntime is trusted, server-created state bound to one edit
// request. Planner arguments never select an owner, artifact, version,
// manifest, scope, operation identity, or commit callback.
type ArtifactEditToolRuntime struct {
	ArtifactID       string
	BaseVersionID    string
	BaseVersion      int64
	ManifestID       string
	OperationID      string
	SelectedBlockIDs []string
	Body             artifact.Body
	Evidence         []model.SourceSnapshotItem
	Proposal         *ArtifactEditProposal
	Outcome          *ArtifactEditToolOutcome
	ValidateScope    func(context.Context) error
	PersistProposal  func(context.Context, ArtifactEditProposal) (ArtifactEditProposal, error)
	CommitProposal   func(context.Context, ArtifactEditProposal) (ArtifactEditCommitResult, error)
}

type ArtifactEditProposal struct {
	OperationID string               `json:"operation_id"`
	Summary     string               `json:"summary"`
	Patch       artifact.Patch       `json:"patch"`
	PatchHash   string               `json:"patch_hash"`
	Result      artifact.PatchResult `json:"result"`
}

type ArtifactEditCommitResult struct {
	OperationID     string `json:"operation_id"`
	ResultVersionID string `json:"result_version_id"`
}

type ArtifactEditToolOutcome struct {
	Kind            string   `json:"kind"`
	Message         string   `json:"message,omitempty"`
	EvidenceIDs     []string `json:"evidence_ids,omitempty"`
	OperationID     string   `json:"operation_id,omitempty"`
	ResultVersionID string   `json:"result_version_id,omitempty"`
}

type ArtifactEditReadableBlock struct {
	artifact.Block
	Hash string `json:"hash"`
}

type ArtifactEditReadResult struct {
	ArtifactID    string                      `json:"artifact_id"`
	BaseVersionID string                      `json:"base_version_id"`
	BaseVersion   int64                       `json:"base_version"`
	Title         string                      `json:"title"`
	TitleHash     string                      `json:"title_hash"`
	Blocks        []ArtifactEditReadableBlock `json:"blocks"`
	Relations     []artifact.Relation         `json:"relations"`
	NextAfter     string                      `json:"next_after,omitempty"`
}

type ArtifactEditFindMatch struct {
	BlockID string `json:"block_id"`
	Title   string `json:"title"`
	Content string `json:"content"`
	Hash    string `json:"hash"`
}

type ArtifactEditFindResult struct {
	Matches []ArtifactEditFindMatch `json:"matches"`
}

type ArtifactEditEvidenceItem struct {
	ID              string `json:"id"`
	SourceID        int64  `json:"source_id"`
	SourceIdentity  string `json:"source_identity"`
	Modality        string `json:"modality"`
	Content         string `json:"content"`
	ContentHash     string `json:"content_hash"`
	StartMS         *int64 `json:"start_ms"`
	EndMS           *int64 `json:"end_ms"`
	TimeRangeStatus string `json:"time_range_status"`
}

type ArtifactEditEvidenceResult struct {
	Evidence []ArtifactEditEvidenceItem `json:"evidence"`
}

type artifactEditTool struct {
	definition VideoAgentToolDefinition
	execute    func(context.Context, VideoAgentToolRequest) (VideoAgentToolResult, error)
}

func (t *artifactEditTool) Definition() VideoAgentToolDefinition { return t.definition }
func (t *artifactEditTool) Execute(ctx context.Context, request VideoAgentToolRequest) (VideoAgentToolResult, error) {
	return t.execute(ctx, request)
}

// NewArtifactEditToolRegistry creates an isolated capability registry for one
// frozen mode. In particular, answer and preview modes have no commit tool.
func NewArtifactEditToolRegistry(mode ArtifactEditMode) (*VideoAgentToolRegistry, string, error) {
	if mode != ArtifactEditModeAnswer && mode != ArtifactEditModePreview && mode != ArtifactEditModeApply {
		return nil, "", artifact.Err("invalid_request", 400)
	}
	registry := NewVideoAgentToolRegistry()
	tools := artifactEditReadTools()
	if mode != ArtifactEditModeAnswer {
		tools = append(tools, artifactEditProposalTools()...)
	}
	if mode == ArtifactEditModeApply {
		tools = append(tools, artifactEditCommitTool())
	}
	for _, tool := range tools {
		if err := registry.Register(tool); err != nil {
			return nil, "", err
		}
	}
	return registry, ArtifactEditToolSchemaDigest(registry.Definitions()), nil
}

// ArtifactEditToolSchemaDigest freezes the complete planner-facing capability
// surface (name, description, and schema), rather than names alone.
func ArtifactEditToolSchemaDigest(definitions []VideoAgentToolDefinition) string {
	copyOf := append([]VideoAgentToolDefinition(nil), definitions...)
	sort.Slice(copyOf, func(i, j int) bool { return copyOf[i].Name < copyOf[j].Name })
	encoded, _ := json.Marshal(copyOf)
	return artifact.Hash(string(encoded))
}

func artifactEditReadTools() []VideoAgentTool {
	return []VideoAgentTool{
		newArtifactEditTool(ArtifactEditToolRead, "读取本次请求绑定的基础版本和授权块；不能选择其它成果、版本或范围。", artifactEditToolSchema(ArtifactEditToolRead), executeArtifactEditRead),
		newArtifactEditTool(ArtifactEditToolFindBlocks, "只在绑定基础版本的授权块内定位文字和结构。", artifactEditToolSchema(ArtifactEditToolFindBlocks), executeArtifactEditFind),
		newArtifactEditTool(ArtifactEditToolInspectEvidence, "按服务端证据 ID 读取绑定 manifest 中的少量原始证据。", artifactEditToolSchema(ArtifactEditToolInspectEvidence), executeArtifactEditEvidence),
		newArtifactEditTool(ArtifactEditToolAnswerQuestion, "回答用户对当前成果的普通询问；不会创建 proposal 或版本。", artifactEditToolSchema(ArtifactEditToolAnswerQuestion), executeArtifactEditAnswer),
	}
}

func artifactEditProposalTools() []VideoAgentTool {
	return []VideoAgentTool{
		newArtifactEditTool(ArtifactEditToolProposePatch, "提交一个受约束结构化 patch 供服务端整体验证；本工具本身不发布版本。选中卡片或段落改名用 update_block.title 和对应 block_id；update_title 只改整份成果标题，选中块范围内不能使用。", artifactEditToolSchema(ArtifactEditToolProposePatch), executeArtifactEditPropose),
		newArtifactEditTool(ArtifactEditToolNothingToChange, "说明为何无需修改；不会创建空版本。", artifactEditToolSchema(ArtifactEditToolNothingToChange), executeArtifactEditNothing),
	}
}

func artifactEditCommitTool() VideoAgentTool {
	return newArtifactEditTool(ArtifactEditToolCommitPatch, "发布本次运行已经验证并持久化的 proposal；不能接收新的 patch 或目标。", artifactEditToolSchema(ArtifactEditToolCommitPatch), executeArtifactEditCommit)
}

func newArtifactEditTool(name, description string, schema json.RawMessage, execute func(context.Context, VideoAgentToolRequest) (VideoAgentToolResult, error)) VideoAgentTool {
	return &artifactEditTool{definition: VideoAgentToolDefinition{Name: name, Description: description, InputSchema: schema}, execute: execute}
}

func artifactEditToolSchema(name string) json.RawMessage {
	switch name {
	case ArtifactEditToolRead:
		return json.RawMessage(`{"type":"object","properties":{"after_block_id":{"type":"string"},"limit":{"type":"integer","minimum":1,"maximum":50}},"additionalProperties":false}`)
	case ArtifactEditToolFindBlocks:
		return json.RawMessage(`{"type":"object","properties":{"query":{"type":"string","minLength":1},"limit":{"type":"integer","minimum":1,"maximum":20}},"required":["query"],"additionalProperties":false}`)
	case ArtifactEditToolInspectEvidence:
		return json.RawMessage(`{"type":"object","properties":{"evidence_ids":{"type":"array","minItems":1,"maxItems":20,"items":{"type":"string","minLength":1}}},"required":["evidence_ids"],"additionalProperties":false}`)
	case ArtifactEditToolAnswerQuestion, ArtifactEditToolNothingToChange:
		return json.RawMessage(`{"type":"object","properties":{"message":{"type":"string","minLength":1},"evidence_ids":{"type":"array","maxItems":20,"items":{"type":"string","minLength":1}}},"required":["message","evidence_ids"],"additionalProperties":false}`)
	case ArtifactEditToolProposePatch:
		return json.RawMessage(`{
			"type":"object",
			"properties":{
				"summary":{"type":"string","minLength":1,"maxLength":500},
				"patch":{
					"type":"object",
					"properties":{
						"schema_version":{"const":1},
						"artifact_id":{"type":"string","minLength":1},
						"base_version_id":{"type":"string","minLength":1},
						"base_version":{"type":"integer","minimum":1},
						"basis":{"enum":["user_instruction","evidence_supported","evidence_conflict"]},
						"evidence_ids":{"type":"array","maxItems":100,"uniqueItems":true,"items":{"type":"string","minLength":1}},
						"operations":{
							"type":"array","minItems":1,"maxItems":50,
							"items":{"oneOf":[
								{
									"type":"object","properties":{"op":{"const":"update_title"},"expected_hash":{"type":"string","minLength":1},"title":{"type":"string","minLength":1,"maxLength":200}},
									"required":["op","expected_hash","title"],"additionalProperties":false
								},
								{
									"type":"object","properties":{"op":{"const":"update_block"},"block_id":{"type":"string","minLength":1},"expected_hash":{"type":"string","minLength":1},"title":{"type":"string","minLength":1,"maxLength":200},"content":{"type":"string","maxLength":8000},"type":{"enum":["section","concept","example","note"]},"evidence_refs":{"$ref":"#/$defs/evidence_refs"}},
									"required":["op","block_id","expected_hash"],"anyOf":[{"required":["title"]},{"required":["content"]},{"required":["type"]},{"required":["evidence_refs"]}],"additionalProperties":false
								},
								{
									"type":"object","properties":{"op":{"const":"insert_block"},"key":{"type":"string","minLength":1,"maxLength":100},"parent_id":{"type":["string","null"]},"after_block_id":{"type":["string","null"]},"type":{"enum":["section","concept","example","note"]},"title":{"type":"string","minLength":1,"maxLength":200},"content":{"type":"string","maxLength":8000},"evidence_refs":{"$ref":"#/$defs/evidence_refs"}},
									"required":["op","key","parent_id","type","title","content","evidence_refs"],"additionalProperties":false
								},
								{
									"type":"object","properties":{"op":{"const":"delete_subtree"},"block_id":{"type":"string","minLength":1},"expected_hash":{"type":"string","minLength":1}},
									"required":["op","block_id","expected_hash"],"additionalProperties":false
								},
								{
									"type":"object","properties":{"op":{"const":"move_subtree"},"block_id":{"type":"string","minLength":1},"expected_hash":{"type":"string","minLength":1},"parent_id":{"type":["string","null"]},"after_block_id":{"type":["string","null"]}},
									"required":["op","block_id","expected_hash","parent_id"],"additionalProperties":false
								},
								{
									"type":"object","properties":{"op":{"const":"split_block"},"block_id":{"type":"string","minLength":1},"expected_hash":{"type":"string","minLength":1},"parts":{"type":"array","minItems":2,"maxItems":50,"items":{"type":"object","properties":{"type":{"enum":["section","concept","example","note"]},"title":{"type":"string","minLength":1,"maxLength":200},"content":{"type":"string","maxLength":8000},"evidence_refs":{"$ref":"#/$defs/evidence_refs"}},"required":["type","title","content","evidence_refs"],"additionalProperties":false}}},
									"required":["op","block_id","expected_hash","parts"],"additionalProperties":false
								},
								{
									"type":"object","properties":{"op":{"const":"merge_siblings"},"block_ids":{"type":"array","minItems":2,"maxItems":50,"uniqueItems":true,"items":{"type":"string","minLength":1}},"expected_hashes":{"type":"array","minItems":2,"maxItems":50,"items":{"type":"string","minLength":1}},"title":{"type":"string","minLength":1,"maxLength":200}},
									"required":["op","block_ids","expected_hashes"],"additionalProperties":false
								},
								{"type":"object","properties":{"op":{"const":"add_relation"},"relation":{"type":"object","properties":{"source_block_id":{"type":"string","minLength":1},"target_block_id":{"type":"string","minLength":1},"type":{"enum":["related_to","depends_on","contrasts_with"]},"origin":{"enum":["user","synthesis"]},"evidence_refs":{"$ref":"#/$defs/evidence_refs"}},"required":["source_block_id","target_block_id","type","origin","evidence_refs"],"additionalProperties":false}},"required":["op","relation"],"additionalProperties":false},
								{"type":"object","properties":{"op":{"const":"remove_relation"},"relation_id":{"type":"string","minLength":1}},"required":["op","relation_id"],"additionalProperties":false},
								{"type":"object","properties":{"op":{"const":"group_siblings"},"block_ids":{"type":"array","minItems":2,"maxItems":20,"uniqueItems":true,"items":{"type":"string","minLength":1}},"expected_hashes":{"type":"array","minItems":2,"maxItems":20,"items":{"type":"string","minLength":1}},"title":{"type":"string","minLength":1,"maxLength":200}},"required":["op","block_ids","expected_hashes","title"],"additionalProperties":false}
							]}
						}
					},
					"required":["schema_version","artifact_id","base_version_id","base_version","basis","evidence_ids","operations"],
					"additionalProperties":false
				}
			},
			"required":["summary","patch"],
			"additionalProperties":false,
			"$defs":{
				"evidence_refs":{"type":"array","maxItems":100,"items":{"type":"object","properties":{"evidence_id":{"type":"string","minLength":1},"relation":{"enum":["supports","context","contradicts"]},"chat_citation_id":{"type":"string","maxLength":32}},"required":["evidence_id","relation"],"additionalProperties":false}}
			}
		}`)
	case ArtifactEditToolCommitPatch:
		return json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`)
	default:
		return json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`)
	}
}

func artifactEditRuntime(ctx context.Context, request VideoAgentToolRequest) (*ArtifactEditToolRuntime, error) {
	runtime := request.Runtime.ArtifactEdit
	if runtime == nil {
		return nil, errors.New("artifact edit runtime unavailable")
	}
	if runtime.ValidateScope != nil {
		if err := runtime.ValidateScope(ctx); err != nil {
			return nil, err
		}
	}
	if strings.TrimSpace(runtime.ArtifactID) == "" || strings.TrimSpace(runtime.BaseVersionID) == "" || runtime.BaseVersion <= 0 || strings.TrimSpace(runtime.ManifestID) == "" {
		return nil, errors.New("artifact edit runtime identity is incomplete")
	}
	return runtime, nil
}

type artifactEditReadArguments struct {
	AfterBlockID string `json:"after_block_id,omitempty"`
	Limit        int    `json:"limit,omitempty"`
}

func executeArtifactEditRead(ctx context.Context, request VideoAgentToolRequest) (VideoAgentToolResult, error) {
	runtime, err := artifactEditRuntime(ctx, request)
	if err != nil {
		return failedArtifactEditTool(ArtifactEditToolRead, err)
	}
	var args artifactEditReadArguments
	if err = decodeVideoAgentToolArguments(request, &args); err != nil {
		return failedArtifactEditTool(ArtifactEditToolRead, err)
	}
	limit := args.Limit
	if limit == 0 {
		limit = artifactEditMaxReadBlocks
	}
	if limit < 1 || limit > artifactEditMaxReadBlocks {
		return failedArtifactEditTool(ArtifactEditToolRead, &InvalidToolArguments{Cause: errors.New("limit 超出范围")})
	}
	blocks := authorizedArtifactEditBlocks(runtime.Body, runtime.SelectedBlockIDs)
	start := 0
	if args.AfterBlockID != "" {
		start = -1
		for i := range blocks {
			if blocks[i].BlockID == args.AfterBlockID {
				start = i + 1
				break
			}
		}
		if start < 0 {
			return failedArtifactEditTool(ArtifactEditToolRead, &InvalidToolArguments{Cause: errors.New("after_block_id 不在授权范围")})
		}
	}
	end := min(start+limit, len(blocks))
	result := ArtifactEditReadResult{ArtifactID: runtime.ArtifactID, BaseVersionID: runtime.BaseVersionID, BaseVersion: runtime.BaseVersion, Title: runtime.Body.Title, TitleHash: artifact.Hash(runtime.Body.Title), Blocks: make([]ArtifactEditReadableBlock, 0, end-start), Relations: []artifact.Relation{}}
	if len(runtime.SelectedBlockIDs) == 0 {
		result.Relations = append(result.Relations, runtime.Body.Relations...)
	} else {
		scoped := make(map[string]bool, len(blocks))
		for _, block := range blocks {
			scoped[block.BlockID] = true
		}
		for _, rel := range runtime.Body.Relations {
			if scoped[rel.SourceBlockID] && scoped[rel.TargetBlockID] {
				result.Relations = append(result.Relations, rel)
			}
		}
	}
	for _, block := range blocks[start:end] {
		result.Blocks = append(result.Blocks, ArtifactEditReadableBlock{Block: block, Hash: artifact.BlockHash(block)})
	}
	if end < len(blocks) && end > 0 {
		result.NextAfter = blocks[end-1].BlockID
	}
	return marshalArtifactEditTool(ArtifactEditToolRead, result, map[string]any{"block_count": len(result.Blocks)})
}

type artifactEditFindArguments struct {
	Query string `json:"query"`
	Limit int    `json:"limit,omitempty"`
}

func executeArtifactEditFind(ctx context.Context, request VideoAgentToolRequest) (VideoAgentToolResult, error) {
	runtime, err := artifactEditRuntime(ctx, request)
	if err != nil {
		return failedArtifactEditTool(ArtifactEditToolFindBlocks, err)
	}
	var args artifactEditFindArguments
	if err = decodeVideoAgentToolArguments(request, &args); err != nil {
		return failedArtifactEditTool(ArtifactEditToolFindBlocks, err)
	}
	query := strings.ToLower(strings.TrimSpace(args.Query))
	limit := args.Limit
	if limit == 0 {
		limit = 10
	}
	if query == "" || limit < 1 || limit > artifactEditMaxFindMatches {
		return failedArtifactEditTool(ArtifactEditToolFindBlocks, &InvalidToolArguments{Cause: errors.New("query 或 limit 无效")})
	}
	result := ArtifactEditFindResult{Matches: []ArtifactEditFindMatch{}}
	for _, block := range authorizedArtifactEditBlocks(runtime.Body, runtime.SelectedBlockIDs) {
		if !strings.Contains(strings.ToLower(block.Title+"\n"+block.Content), query) {
			continue
		}
		result.Matches = append(result.Matches, ArtifactEditFindMatch{BlockID: block.BlockID, Title: block.Title, Content: trimRunes(block.Content, 1200), Hash: artifact.BlockHash(block)})
		if len(result.Matches) == limit {
			break
		}
	}
	return marshalArtifactEditTool(ArtifactEditToolFindBlocks, result, map[string]any{"match_count": len(result.Matches)})
}

type artifactEditEvidenceArguments struct {
	EvidenceIDs []string `json:"evidence_ids"`
}

func executeArtifactEditEvidence(ctx context.Context, request VideoAgentToolRequest) (VideoAgentToolResult, error) {
	runtime, err := artifactEditRuntime(ctx, request)
	if err != nil {
		return failedArtifactEditTool(ArtifactEditToolInspectEvidence, err)
	}
	var args artifactEditEvidenceArguments
	if err = decodeVideoAgentToolArguments(request, &args); err != nil {
		return failedArtifactEditTool(ArtifactEditToolInspectEvidence, err)
	}
	if len(args.EvidenceIDs) == 0 || len(args.EvidenceIDs) > artifactEditMaxEvidenceItems {
		return failedArtifactEditTool(ArtifactEditToolInspectEvidence, &InvalidToolArguments{Cause: errors.New("evidence_ids 数量无效")})
	}
	byID := make(map[string]model.SourceSnapshotItem, len(runtime.Evidence))
	for _, item := range runtime.Evidence {
		if item.ManifestID == runtime.ManifestID {
			byID[item.ID] = item
		}
	}
	seen := make(map[string]bool, len(args.EvidenceIDs))
	result := ArtifactEditEvidenceResult{Evidence: make([]ArtifactEditEvidenceItem, 0, len(args.EvidenceIDs))}
	for _, id := range args.EvidenceIDs {
		id = strings.TrimSpace(id)
		item, ok := byID[id]
		if id == "" || !ok || seen[id] {
			return failedArtifactEditTool(ArtifactEditToolInspectEvidence, artifact.Err("invalid_evidence", 400))
		}
		seen[id] = true
		result.Evidence = append(result.Evidence, ArtifactEditEvidenceItem{ID: item.ID, SourceID: item.SourceID, SourceIdentity: item.SourceIdentity, Modality: item.Modality, Content: trimRunes(item.Content, artifactEditMaxEvidenceTextRunes), ContentHash: item.ContentHash, StartMS: item.StartMS, EndMS: item.EndMS, TimeRangeStatus: item.TimeRangeStatus})
	}
	return marshalArtifactEditTool(ArtifactEditToolInspectEvidence, result, map[string]any{"evidence_count": len(result.Evidence)})
}

type artifactEditMessageArguments struct {
	Message     string   `json:"message"`
	EvidenceIDs []string `json:"evidence_ids"`
}

func executeArtifactEditAnswer(ctx context.Context, request VideoAgentToolRequest) (VideoAgentToolResult, error) {
	return executeArtifactEditMessageTool(ctx, request, ArtifactEditToolAnswerQuestion, "answer")
}

func executeArtifactEditNothing(ctx context.Context, request VideoAgentToolRequest) (VideoAgentToolResult, error) {
	return executeArtifactEditMessageTool(ctx, request, ArtifactEditToolNothingToChange, "no_change")
}

func executeArtifactEditMessageTool(ctx context.Context, request VideoAgentToolRequest, tool, kind string) (VideoAgentToolResult, error) {
	runtime, err := artifactEditRuntime(ctx, request)
	if err != nil {
		return failedArtifactEditTool(tool, err)
	}
	var args artifactEditMessageArguments
	if err = decodeVideoAgentToolArguments(request, &args); err != nil {
		return failedArtifactEditTool(tool, err)
	}
	args.Message = strings.TrimSpace(args.Message)
	if args.Message == "" || len(args.EvidenceIDs) > artifactEditMaxEvidenceItems || !artifactEditEvidenceIDsAllowed(runtime, args.EvidenceIDs) {
		return failedArtifactEditTool(tool, &InvalidToolArguments{Cause: errors.New("message 或 evidence_ids 无效")})
	}
	runtime.Outcome = &ArtifactEditToolOutcome{Kind: kind, Message: args.Message, EvidenceIDs: append([]string(nil), args.EvidenceIDs...)}
	return marshalArtifactEditTool(tool, runtime.Outcome, map[string]any{"result_kind": kind, "evidence_count": len(args.EvidenceIDs)})
}

type artifactEditProposeArguments struct {
	Summary string         `json:"summary"`
	Patch   artifact.Patch `json:"patch"`
}

func executeArtifactEditPropose(ctx context.Context, request VideoAgentToolRequest) (VideoAgentToolResult, error) {
	runtime, err := artifactEditRuntime(ctx, request)
	if err != nil {
		return failedArtifactEditTool(ArtifactEditToolProposePatch, err)
	}
	var args artifactEditProposeArguments
	if err = decodeVideoAgentToolArguments(request, &args); err != nil {
		return failedArtifactEditTool(ArtifactEditToolProposePatch, err)
	}
	args.Summary = strings.TrimSpace(args.Summary)
	if args.Summary == "" || len([]rune(args.Summary)) > 500 || strings.TrimSpace(runtime.OperationID) == "" {
		return failedArtifactEditTool(ArtifactEditToolProposePatch, &InvalidToolArguments{Cause: errors.New("proposal summary 或 operation identity 无效")})
	}
	auth := runtime.patchAuthorization()
	result, err := artifact.EditPatch(runtime.Body, args.Patch, auth)
	if err != nil {
		return failedArtifactEditTool(ArtifactEditToolProposePatch, err)
	}
	_, patchHash, err := artifact.CanonicalPatch(args.Patch)
	if err != nil {
		return failedArtifactEditTool(ArtifactEditToolProposePatch, err)
	}
	proposal := ArtifactEditProposal{OperationID: runtime.OperationID, Summary: args.Summary, Patch: args.Patch, PatchHash: patchHash, Result: result}
	if runtime.PersistProposal != nil {
		proposal, err = runtime.PersistProposal(ctx, proposal)
		if err != nil {
			return failedArtifactEditTool(ArtifactEditToolProposePatch, err)
		}
	}
	runtime.Proposal = &proposal
	return marshalArtifactEditTool(ArtifactEditToolProposePatch, proposal, map[string]any{"operation_id": proposal.OperationID, "patch_hash": proposal.PatchHash, "change_count": len(proposal.Result.Diff.Changes)})
}

func executeArtifactEditCommit(ctx context.Context, request VideoAgentToolRequest) (VideoAgentToolResult, error) {
	runtime, err := artifactEditRuntime(ctx, request)
	if err != nil {
		return failedArtifactEditTool(ArtifactEditToolCommitPatch, err)
	}
	var args struct{}
	if err = decodeVideoAgentToolArguments(request, &args); err != nil {
		return failedArtifactEditTool(ArtifactEditToolCommitPatch, err)
	}
	if runtime.Proposal == nil || runtime.CommitProposal == nil {
		return failedArtifactEditTool(ArtifactEditToolCommitPatch, errors.New("validated proposal unavailable"))
	}
	committed, err := runtime.CommitProposal(ctx, *runtime.Proposal)
	if err != nil {
		return failedArtifactEditTool(ArtifactEditToolCommitPatch, err)
	}
	if committed.OperationID != runtime.Proposal.OperationID || strings.TrimSpace(committed.ResultVersionID) == "" {
		return failedArtifactEditTool(ArtifactEditToolCommitPatch, errors.New("committed edit result is inconsistent"))
	}
	runtime.Outcome = &ArtifactEditToolOutcome{Kind: "committed", OperationID: committed.OperationID, ResultVersionID: committed.ResultVersionID}
	return marshalArtifactEditTool(ArtifactEditToolCommitPatch, runtime.Outcome, map[string]any{"operation_id": committed.OperationID, "result_version_id": committed.ResultVersionID})
}

func (r *ArtifactEditToolRuntime) patchAuthorization() artifact.PatchAuthorization {
	allowed := make(map[string]bool, len(r.Evidence))
	for _, item := range r.Evidence {
		if item.ManifestID == r.ManifestID {
			allowed[item.ID] = true
		}
	}
	return artifact.PatchAuthorization{OperationID: r.OperationID, ArtifactID: r.ArtifactID, BaseVersionID: r.BaseVersionID, BaseVersion: r.BaseVersion, SelectedBlockIDs: append([]string(nil), r.SelectedBlockIDs...), AllowedEvidenceIDs: allowed}
}

func artifactEditEvidenceIDsAllowed(runtime *ArtifactEditToolRuntime, ids []string) bool {
	allowed := make(map[string]bool, len(runtime.Evidence))
	for _, item := range runtime.Evidence {
		if item.ManifestID == runtime.ManifestID {
			allowed[item.ID] = true
		}
	}
	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		if !allowed[id] || seen[id] {
			return false
		}
		seen[id] = true
	}
	return true
}

func authorizedArtifactEditBlocks(body artifact.Body, selected []string) []artifact.Block {
	if len(selected) == 0 {
		return append([]artifact.Block(nil), body.Blocks...)
	}
	authorizedRoots := make(map[string]bool, len(selected))
	for _, id := range selected {
		authorizedRoots[id] = true
	}
	authorized := make(map[string]bool, len(body.Blocks))
	for _, block := range body.Blocks {
		if authorizedRoots[block.BlockID] || (block.ParentID != nil && authorized[*block.ParentID]) {
			authorized[block.BlockID] = true
		}
	}
	result := make([]artifact.Block, 0, len(authorized))
	for _, block := range body.Blocks {
		if authorized[block.BlockID] {
			result = append(result, block)
		}
	}
	return result
}

func marshalArtifactEditTool(tool string, value any, input map[string]any) (VideoAgentToolResult, error) {
	output, err := json.Marshal(value)
	if err != nil {
		return failedArtifactEditTool(tool, fmt.Errorf("serialize artifact edit tool output: %w", err))
	}
	return VideoAgentToolResult{Output: output, Step: VideoAgentStep{Name: strings.ReplaceAll(tool, "_", " "), Tool: tool, Input: input}}, nil
}

func failedArtifactEditTool(tool string, err error) (VideoAgentToolResult, error) {
	return VideoAgentToolResult{Step: VideoAgentStep{Name: strings.ReplaceAll(tool, "_", " "), Tool: tool, Error: safeArtifactEditError(err)}}, err
}

func safeArtifactEditError(err error) string {
	if err == nil {
		return ""
	}
	var domain *artifact.Error
	if errors.As(err, &domain) {
		return domain.Code
	}
	var invalid *InvalidToolArguments
	if errors.As(err, &invalid) {
		return "invalid_arguments"
	}
	return "artifact_edit_failed"
}
