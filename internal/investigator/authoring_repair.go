package investigator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
	"github.com/cloudwego/eino/schema"
	"github.com/jackc/pgx/v5"
	"vidlens/internal/domain"
	"vidlens/internal/store"
)

type authoringFieldPatch struct {
	Op    string          `json:"op" jsonschema:"enum=replace,enum=add"`
	Path  string          `json:"path"`
	Value json.RawMessage `json:"value"`
}
type authoringRepairInput struct {
	CandidateID     string                `json:"candidate_id"`
	ExpectedHash    string                `json:"expected_hash"`
	Version         int                   `json:"version"`
	Patches         []authoringFieldPatch `json:"patches,omitempty" jsonschema:"description=1-16 add/replace operations on rejected /sections/index/blocks/index fields. Use for structurally complete JSON. Mutually exclusive with replacement_json."`
	ReplacementJSON *string               `json:"replacement_json,omitempty" jsonschema:"description=Only when the retained candidate has invalid JSON or missing/empty sections or blocks. Supply one complete save_article_sections object, at most 64KiB, for pending chapters. Accepted chapters stay unchanged. Otherwise use patches."`
}

func patchableAuthoringBody(raw string) bool {
	raw = normalizeAuthoringContainers("save_article_sections", normalizeAuthoringJSON(raw))
	var body struct {
		Sections []struct {
			ID     string                       `json:"id"`
			Blocks []map[string]json.RawMessage `json:"blocks"`
		} `json:"sections"`
	}
	if json.Unmarshal([]byte(raw), &body) != nil || len(body.Sections) == 0 {
		return false
	}
	for _, section := range body.Sections {
		if section.ID == "" || len(section.Blocks) == 0 {
			return false
		}
	}
	return true
}

func repairAuthoringBody(raw string, in authoringRepairInput) ([]byte, error) {
	if in.ReplacementJSON == nil {
		return patchAuthoringBody(raw, in.Patches)
	}
	if len(in.Patches) != 0 || len(*in.ReplacementJSON) > 64<<10 || !json.Valid([]byte(*in.ReplacementJSON)) {
		return nil, errors.New("replacement_json须为一个完整JSON对象、最多64KiB，且不能同时提供patches")
	}
	if patchableAuthoringBody(raw) {
		return nil, errors.New("原候选结构完整，请用patches仅修复错误字段")
	}
	var body map[string]json.RawMessage
	if json.Unmarshal([]byte(*in.ReplacementJSON), &body) != nil || body == nil {
		return nil, errors.New("replacement_json必须为正文批次对象")
	}
	return []byte(*in.ReplacementJSON), nil
}

func patchAuthoringBody(raw string, patches []authoringFieldPatch) ([]byte, error) {
	var root any
	raw = normalizeAuthoringContainers("save_article_sections", normalizeAuthoringJSON(raw))
	if len(patches) < 1 || len(patches) > 16 || len(domain.JSON(patches)) > 64<<10 || json.Unmarshal([]byte(raw), &root) != nil {
		return nil, errors.New("需要合法候选与1–16个字段修复，总计最多64KiB")
	}
	for _, p := range patches {
		parts := strings.Split(strings.TrimPrefix(p.Path, "/"), "/")
		if !strings.HasPrefix(p.Path, "/") || len(parts) < 4 || parts[0] != "sections" || parts[2] != "blocks" || p.Op != "replace" && p.Op != "add" || !json.Valid(p.Value) {
			return nil, errors.New("只能修复候选中已有章节的正文块字段")
		}
		if _, err := strconv.Atoi(parts[1]); err != nil {
			return nil, err
		}
		if _, err := strconv.Atoi(parts[3]); err != nil {
			return nil, err
		}
		if len(parts) > 4 && (parts[4] == "id" || parts[4] == "snapshot_id" || parts[4] == "time_ms") {
			return nil, errors.New("不能修复服务器身份或来源时间")
		}
		var value any
		_ = json.Unmarshal(p.Value, &value)
		at := root
		for i, key := range parts {
			last := i == len(parts)-1
			switch x := at.(type) {
			case map[string]any:
				next, exists := x[key]
				if last {
					if !exists && (p.Op != "add" || len(parts) != 5) {
						return nil, errors.New("add只允许缺少的正文块字段")
					}
					x[key] = value
				} else {
					if !exists {
						return nil, errors.New("修复路径不存在")
					}
					at = next
				}
			case []any:
				n, err := strconv.Atoi(key)
				if err != nil || n < 0 || n >= len(x) || strconv.Itoa(n) != key {
					return nil, errors.New("数组路径必须指向已有元素")
				}
				if last {
					x[n] = value
				} else {
					at = x[n]
				}
			default:
				return nil, errors.New("修复路径穿过非对象字段")
			}
		}
	}
	return domain.JSON(root), nil
}

func (s *session) authoringRepairTool(add func(tool.EnhancedInvokableTool, error) error) error {
	return add(utils.InferEnhancedTool("repair_article_sections", "Repair retained rejected body fields. Bind candidate_id, exact expected_hash and current version. Use 1-16 patches for complete JSON. For invalid JSON or missing/empty sections or blocks only, use replacement_json with a complete pending body batch up to 64KiB, without patches. Accepted chapters remain unchanged. Every repaired body runs the ordinary source, pixel and publication validators.", func(ctx context.Context, in authoringRepairInput) (*schema.ToolResult, error) {
		if err := s.reserveFieldRepair(ctx, in); err != nil {
			return nil, err
		}
		key := "field-repair:" + callID(ctx)
		op, err := s.engine.Store.Operation(ctx, s.claim.Run.ID, key)
		if err != nil {
			return nil, err
		}
		if op.Status == "completed" {
			var saved struct {
				Result *schema.ToolResult `json:"result"`
			}
			if json.Unmarshal(op.Result, &saved) != nil || saved.Result == nil {
				return nil, errors.New("修复记录不可读")
			}
			return saved.Result, nil
		}
		finish := func(result *schema.ToolResult, err error) (*schema.ToolResult, error) {
			if err != nil {
				return result, err
			}
			e := s.engine.Store.FinishOperation(ctx, s.claim, key, map[string]any{"parent_candidate_id": in.CandidateID, "parent_hash": in.ExpectedHash, "result": result}, "", nil)
			return result, e
		}
		// If a worker died after the atomic draft write, acknowledge that exact
		// receipt instead of consuming another revision or rewriting chapters.
		if child, e := s.engine.Store.Operation(ctx, s.claim.Run.ID, "article-draft:"+callID(ctx)); e == nil && child.Status == "completed" {
			var saved store.Draft
			var draft articleDraft
			if json.Unmarshal(child.Result, &saved) != nil || json.Unmarshal(saved.Data, &draft) != nil {
				return nil, errors.New("修复草稿不可读")
			}
			return finish(textResult(s.draftState(saved.Version, draft)), nil)
		} else if e != nil && !errors.Is(e, pgx.ErrNoRows) {
			return nil, e
		}
		candidate, err := s.engine.Store.Operation(ctx, s.claim.Run.ID, "candidate-attempt:"+in.CandidateID+":invalid")
		if err != nil {
			return finish(toolError("候选不属于当前执行。"))
		}
		var saved struct {
			Tool      string `json:"tool"`
			Arguments string `json:"arguments"`
		}
		if json.Unmarshal(candidate.Result, &saved) != nil || saved.Tool != "save_article_sections" || store.Hash(saved.Arguments) != in.ExpectedHash {
			return finish(toolError("候选类型或原始哈希不匹配。"))
		}
		version, draft, err := s.loadDraft(ctx)
		if err != nil {
			return nil, err
		}
		if in.Version != version || version == 0 {
			return finish(toolError("请复制当前草稿version。"))
		}
		raw, err := repairAuthoringBody(saved.Arguments, in)
		if err != nil {
			return finish(toolError(err.Error()))
		}
		if err := authoringShapeIssues(string(raw)); err != nil {
			return finish(toolError(string(domain.JSON(candidateIssue(err)))))
		}
		var next saveSectionsInput
		decoder := json.NewDecoder(strings.NewReader(normalizeAuthoringBody(string(raw))))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&next) != nil {
			return finish(toolError("修复后正文类型不正确。"))
		}
		// The mixed original batch may also contain accepted chapters. Their
		// durable content cannot be replaced as a side effect of this repair.
		pending := []domain.Section{}
		for _, section := range next.Sections {
			if _, accepted := draft.Sections[section.ID]; !accepted {
				pending = append(pending, section)
			}
		}
		if len(pending) == 0 {
			return finish(toolError("候选没有待修复章节；已接受内容保持。"))
		}
		next.Version = version
		next.Sections = pending
		result, err := s.tools["save_article_sections"].InvokableRun(context.WithValue(ctx, draftRepairContextKey{}, true), &schema.ToolArgument{Text: string(domain.JSON(next))})
		return finish(result, err)
	}))
}

func (s *session) authoringFacts(ctx context.Context, sections []domain.Section) error {
	// Absolute years deserve explicit source support. This catches the real
	// record's invented 2025 while leaving relative dates relative.
	available := ""
	if s.snapshot.Source.Title != nil {
		available = *s.snapshot.Source.Title
	}
	for _, segment := range s.transcript {
		available += "\n" + segment.Text
	}
	observations, err := s.observations(ctx)
	if err != nil {
		return err
	}
	for _, o := range observations {
		if o.Status != "supported" {
			continue
		}
		available += "\n" + strings.Join(o.VisibleFacts, "\n") + "\n" + strings.Join(o.LiteralDetails, "\n")
	}
	for _, sec := range sections {
		for blockIndex, b := range sec.Blocks {
			if b.Origin != "video" {
				continue
			}
			text := domain.BlockText(b)
			for year := 2000; year <= 2099; year++ {
				n := strconv.Itoa(year)
				if strings.Contains(text, n+" 年") || strings.Contains(text, n+"年") {
					if !strings.Contains(available, n) {
						return &domain.Problem{Status: 422, Code: "MODEL_RESULT_INVALID", Detail: "视频正文补入了来源未明确支持的年份；保留原文相对日期或补充真实取证。", Meta: map[string]any{"path": fmt.Sprintf("/blocks/%d", blockIndex), "code": "unsupported_year", "actual": n, "repairable": true}}
					}
				}
			}
		}
	}
	return nil
}
