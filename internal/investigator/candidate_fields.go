package investigator

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
	"github.com/cloudwego/eino/schema"
	"vidlens/internal/domain"
	"vidlens/internal/store"
)

func jsonField(raw []byte, path string) (json.RawMessage, error) {
	if !strings.HasPrefix(path, "/") {
		return nil, errors.New("use an existing JSON Pointer field")
	}
	var at any
	if err := json.Unmarshal(raw, &at); err != nil {
		return nil, err
	}
	for _, key := range strings.Split(path[1:], "/") {
		key = strings.ReplaceAll(strings.ReplaceAll(key, "~1", "/"), "~0", "~")
		switch x := at.(type) {
		case map[string]any:
			var ok bool
			at, ok = x[key]
			if !ok {
				return nil, errors.New("field does not exist")
			}
		case []any:
			n, err := strconv.Atoi(key)
			if err != nil || n < 0 || n >= len(x) {
				return nil, errors.New("array index does not exist")
			}
			at = x[n]
		default:
			return nil, errors.New("field path is not an object")
		}
	}
	return domain.JSON(at), nil
}

func (s *session) candidateReadTool(add func(tool.EnhancedInvokableTool, error) error) error {
	return add(utils.InferEnhancedTool("read_candidate_fields", "Read up to 8 existing JSON Pointer fields from a retained rejected candidate, at most 24KiB total. Use a narrower path if too large. Raw payloads stay private. Does not validate or publish anything.", func(ctx context.Context, in struct {
		CandidateID  string   `json:"candidate_id"`
		ExpectedHash string   `json:"expected_hash"`
		Paths        []string `json:"paths"`
	}) (*schema.ToolResult, error) {
		op, err := s.engine.Store.Operation(ctx, s.claim.Run.ID, "candidate-attempt:"+in.CandidateID+":invalid")
		if err != nil {
			return toolError("候选不存在或不属于本次执行。")
		}
		var saved struct {
			Arguments string `json:"arguments"`
		}
		if err = json.Unmarshal(op.Result, &saved); err != nil {
			return nil, err
		}
		if store.Hash(saved.Arguments) != in.ExpectedHash || len(in.Paths) < 1 || len(in.Paths) > 8 {
			return toolError("候选哈希需一致，每次读取1–8个字段。")
		}
		fields := map[string]json.RawMessage{}
		for _, path := range in.Paths {
			value, e := jsonField([]byte(saved.Arguments), path)
			if e != nil {
				return toolError(e.Error())
			}
			fields[path] = value
		}
		if len(domain.JSON(fields)) > 24<<10 {
			return toolError("目标字段过大，请读取更具体的正文块或引用字段。")
		}
		return textResult(map[string]any{"candidate_id": in.CandidateID, "expected_hash": in.ExpectedHash, "fields": fields}), nil
	}))
}

func projectRejectedCandidates(messages []*schema.Message) []*schema.Message {
	out := append([]*schema.Message{}, messages...)
	for i, m := range out {
		if m.Role != schema.Assistant || len(m.ToolCalls) == 0 {
			continue
		}
		done := map[string]bool{}
		for j := i + 1; j < len(out) && out[j].Role == schema.Tool; j++ {
			done[out[j].ToolCallID] = true
		}
		copy := *m
		copy.ToolCalls = append([]schema.ToolCall{}, m.ToolCalls...)
		for j, c := range copy.ToolCalls {
			if done[c.ID] && (c.Function.Name == "submit_answer" || c.Function.Name == "submit_article") && len(c.Function.Arguments) > 24<<10 {
				copy.ToolCalls[j].Function.Arguments = string(domain.JSON(map[string]any{"retained_candidate_id": c.ID, "expected_hash": store.Hash(c.Function.Arguments), "note": "Original private payload retained. Use read_candidate_fields then repair_candidate; do not regenerate the complete candidate."}))
			}
		}
		out[i] = &copy
	}
	return out
}
