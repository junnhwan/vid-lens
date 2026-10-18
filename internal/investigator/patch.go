package investigator

import (
	"bytes"
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

type fieldPatch struct {
	Op    string          `json:"op" jsonschema:"enum=replace"`
	Path  string          `json:"path"`
	Value json.RawMessage `json:"value"`
}
type draftPatchInput struct {
	Version int          `json:"version"`
	Patches []fieldPatch `json:"patches"`
}
type candidatePatchInput struct {
	CandidateID     string       `json:"candidate_id"`
	ExpectedHash    string       `json:"expected_hash"`
	Patches         []fieldPatch `json:"patches,omitempty"`
	ReplacementJSON *string      `json:"replacement_json,omitempty" jsonschema:"description=Only for syntactically invalid candidates <=64KiB. Supply one strictly valid complete JSON object. Otherwise use replace patches."`
}

// The restricted JSON Pointer subset cannot add/delete/reorder fields, alter
// frozen source/configuration, or bypass canonical citation reconstruction.
func replaceFields(raw []byte, patches []fieldPatch, allowed func([]string) bool) ([]byte, error) {
	if !json.Valid(raw) || len(patches) < 1 || len(patches) > 16 || len(domain.JSON(patches)) > 64<<10 {
		return nil, errors.New("需要1–16个replace字段操作，合计最多64KiB，且原候选必须为合法JSON")
	}
	var root any
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	if err := d.Decode(&root); err != nil {
		return nil, err
	}
	for _, p := range patches {
		if p.Op != "replace" || !strings.HasPrefix(p.Path, "/") || len(p.Path) > 400 || !json.Valid(p.Value) {
			return nil, errors.New("只允许已有字段的replace操作")
		}
		parts := strings.Split(p.Path[1:], "/")
		for i, x := range parts {
			for j := 0; j < len(x); j++ {
				if x[j] == '~' && (j+1 == len(x) || x[j+1] != '0' && x[j+1] != '1') {
					return nil, errors.New("无效JSON Pointer转义")
				}
				if x[j] == '~' {
					j++
				}
			}
			parts[i] = strings.ReplaceAll(strings.ReplaceAll(x, "~1", "/"), "~0", "~")
		}
		if !allowed(parts) {
			return nil, fmt.Errorf("路径%s不可修改；只修复正文、图注、引用或限制字段", p.Path)
		}
		var value any
		v := json.NewDecoder(bytes.NewReader(p.Value))
		v.UseNumber()
		if err := v.Decode(&value); err != nil {
			return nil, err
		}
		at := root
		for i, key := range parts {
			last := i == len(parts)-1
			switch obj := at.(type) {
			case map[string]any:
				next, ok := obj[key]
				if !ok {
					return nil, fmt.Errorf("字段%s不存在", p.Path)
				}
				if last {
					obj[key] = value
				} else {
					at = next
				}
			case []any:
				n, err := strconv.Atoi(key)
				if err != nil || n < 0 || n >= len(obj) || strconv.Itoa(n) != key {
					return nil, errors.New("数组路径必须为已有元素下标")
				}
				if last {
					obj[n] = value
				} else {
					at = obj[n]
				}
			default:
				return nil, errors.New("路径穿过非对象字段")
			}
		}
	}
	out := domain.JSON(root)
	if len(out) > 512<<10 {
		return nil, errors.New("候选超过512KiB")
	}
	return out, nil
}

func (s *session) reserveFieldRepair(ctx context.Context, args any) error {
	key := "field-repair:" + callID(ctx)
	if old, err := s.engine.Store.Operation(ctx, s.claim.Run.ID, key); err == nil {
		if old.ArgsHash != store.Hash(args) {
			return errors.New("repair replay conflict")
		}
		return nil
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	count, err := s.repairUsage(ctx)
	if err != nil {
		return err
	}
	if s.spec.DetailPolicy == nil || count >= s.repairLimit() {
		return domain.Err(422, "MODEL_RESULT_INVALID", "字段修复额度已用完，草稿与原候选保留。")
	}
	_, err = s.engine.Store.BeginOperation(ctx, s.claim, key, "field_repair", args, false)
	return err
}

func (s *session) patchTools(add func(tool.EnhancedInvokableTool, error) error) error {
	if s.policy().Legacy {
		return nil
	}
	if err := s.candidateReadTool(add); err != nil {
		return err
	}
	if s.turn == nil && s.cp.LayoutVersion == draftLayout {
		if s.authoring() {
			if err := s.authoringRepairTool(add); err != nil {
				return err
			}
		}
		if s.policy().ContentPlan {
			if err := s.planRepairTool(add); err != nil {
				return err
			}
		}
		return add(utils.InferEnhancedTool("patch_article_draft", "Replace existing fields in saved draft chapters with version CAS. Paths: /title, /limitations or /sections/<id>/blocks/.... Cannot alter plan, source, config or canonical citation metadata. Full publication validation still required.", func(ctx context.Context, in draftPatchInput) (*schema.ToolResult, error) {
			if r, e := s.replayedDraft(ctx, in); r != nil || e != nil {
				return r, e
			}
			version, draft, err := s.loadDraft(ctx)
			if err != nil {
				return nil, err
			}
			if version == 0 || version != in.Version || s.forceDraftSubmit {
				return s.draftFeedback(ctx, "请使用当前版本，仅在校验指出错误后修复已保存字段。")
			}
			if err = s.reserveFieldRepair(ctx, in); err != nil {
				return nil, err
			}
			raw, err := replaceFields(domain.JSON(draft), in.Patches, func(p []string) bool {
				return len(p) == 1 && (p[0] == "title" || p[0] == "limitations") || len(p) >= 3 && p[0] == "sections" && p[2] == "blocks"
			})
			if err != nil {
				return s.draftFeedback(ctx, err.Error())
			}
			var next articleDraft
			d := json.NewDecoder(bytes.NewReader(raw))
			d.DisallowUnknownFields()
			if err = d.Decode(&next); err != nil {
				return s.draftFeedback(ctx, string(domain.JSON(candidateIssue(err))))
			}
			catalog, err := s.citationCatalog(ctx)
			if err != nil {
				return nil, err
			}
			if err = resolveDraftCitations(&next, catalog); err != nil {
				return s.draftFeedback(ctx, err.Error())
			}
			ids := map[string]bool{}
			for _, p := range next.Plan {
				ids[p.ID] = true
			}
			for id, sec := range next.Sections {
				if !ids[id] {
					return s.draftFeedback(ctx, "不能新增计划外章节")
				}
				for i := range sec.Blocks {
					for j, f := range sec.Blocks[i].FrameIDs {
						if c, ok := catalog[f]; ok && c.Kind == "video_frame" {
							sec.Blocks[i].FrameIDs[j] = c.FrameID
						}
					}
				}
				next.Sections[id] = sec
				if err = s.validateBlocks(ctx, sec.Blocks, next.Citations, true); err != nil {
					return s.draftFeedback(ctx, err.Error())
				}
			}
			if err = validateLimitations(next.Limitations, ids, next.Citations); err != nil {
				return s.draftFeedback(ctx, err.Error())
			}
			return s.writeDraft(ctx, in, version, next, "正在修正文章字段")
		}))
	}
	return add(utils.InferEnhancedTool("repair_candidate", "Repair a rejected final answer/article by candidate_id and exact expected_hash. Replace existing fields only. Syntactically invalid candidates <=64KiB may supply replacement_json instead. Every repaired result undergoes full original validation before publication.", func(ctx context.Context, in candidatePatchInput) (*schema.ToolResult, error) {
		if err := s.reserveFieldRepair(ctx, in); err != nil {
			return nil, err
		}
		var saved struct {
			Tool      string `json:"tool"`
			Arguments string `json:"arguments"`
		}
		op, err := s.engine.Store.Operation(ctx, s.claim.Run.ID, "candidate-attempt:"+in.CandidateID+":invalid")
		if err != nil {
			return nil, &candidateError{err: domain.Err(422, "MODEL_RESULT_INVALID", "候选不存在或不属于当前执行。")}
		}
		if err = json.Unmarshal(op.Result, &saved); err != nil {
			return nil, err
		}
		if saved.Tool != "submit_answer" && saved.Tool != "submit_article" || store.Hash(saved.Arguments) != in.ExpectedHash {
			return nil, &candidateError{err: domain.Err(422, "MODEL_RESULT_INVALID", "候选身份或哈希不匹配。")}
		}
		raw := []byte(saved.Arguments)
		if in.ReplacementJSON != nil {
			if json.Valid(raw) || len(raw) > 64<<10 || len(*in.ReplacementJSON) > 64<<10 || !json.Valid([]byte(*in.ReplacementJSON)) || len(in.Patches) > 0 {
				err = errors.New("仅小型语法错误候选允许完整格式修正；合法JSON请用字段replace")
			} else {
				raw = []byte(*in.ReplacementJSON)
			}
		} else {
			raw, err = replaceFields(raw, in.Patches, func(p []string) bool {
				switch p[0] {
				case "title", "sections", "mind_map", "citations", "limitations", "kind", "summary", "blocks":
					return true
				}
				return false
			})
		}
		if err != nil {
			return nil, &candidateError{err: domain.Err(422, "MODEL_RESULT_INVALID", err.Error())}
		}
		// Bind parent and child identities before the ordinary checked tool records
		// and validates the new candidate. Invalid revisions never enter candidate.
		if err = s.engine.Store.FinishOperation(ctx, s.claim, "field-repair:"+callID(ctx), map[string]any{"parent_candidate_id": in.CandidateID, "parent_hash": in.ExpectedHash, "candidate_id": callID(ctx), "hash": store.Hash(string(raw))}, "", nil); err != nil {
			return nil, err
		}
		return s.tools[saved.Tool].InvokableRun(ctx, &schema.ToolArgument{Text: string(raw)})
	}))
}
