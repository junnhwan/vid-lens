package investigator

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/cloudwego/eino/schema"
	"github.com/jackc/pgx/v5"
	"vidlens/internal/domain"
)

type frozenModelInput struct {
	ResponsePolicy *modelResponsePolicy `json:"response_policy,omitempty"`
	Args           json.RawMessage      `json:"args"`
	Messages       []*schema.Message    `json:"messages"`
	Budget         callBudget           `json:"budget"`
	ToolNames      []string             `json:"tool_names"`
	ToolSchemaJSON string               `json:"tool_schema_json,omitempty"`
}

// Freeze the complete logical input before any physical submission. Readiness
// may change after a crash; it must never change an already authorized model:N.
func (s *session) freezeModelInput(ctx context.Context, key string, in []*schema.Message, args any, budget callBudget, tools []*schema.ToolInfo) (frozenModelInput, error) {
	operation := "input:" + key
	op, err := s.engine.Store.Operation(ctx, s.claim.Run.ID, operation)
	var out frozenModelInput
	if err == nil && op.Status == "completed" {
		err = json.Unmarshal(op.Result, &out)
		if err == nil && domain.ContentHash(out.ResponsePolicy) != domain.ContentHash(s.responsePolicy()) {
			return out, loopStop("MODEL_INPUT_CONFLICT", "已冻结调用的响应预算不一致，不重新发送")
		}
		if err == nil && s.loopReferenceReview() && (domain.ArticleJSONHash(out.Args) != domain.ArticleJSONHash(args) || domain.ArticleJSONHash(out.Messages) != domain.ArticleJSONHash(stripPixels(in)) || out.ToolSchemaJSON != string(domain.JSON(tools))) {
			return out, loopStop("MODEL_INPUT_CONFLICT", "已冻结调用的输入或工具身份不一致，不重新发送")
		}
		return out, err
	}
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return out, err
	}
	if _, err = s.engine.Store.BeginOperation(ctx, s.claim, operation, "model_input", map[string]string{"key": key, "protocol": s.cp.Protocol}, false); err != nil {
		return out, err
	}
	out = frozenModelInput{Args: domain.JSON(args), Messages: stripPixels(in), Budget: budget, ToolNames: []string{}, ToolSchemaJSON: string(domain.JSON(tools))}
	out.ResponsePolicy = s.responsePolicy()
	for _, t := range tools {
		out.ToolNames = append(out.ToolNames, t.Name)
	}
	err = s.engine.Store.FinishOperation(ctx, s.claim, operation, out, "", nil)
	return out, err
}
