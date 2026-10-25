package investigator

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/cloudwego/eino/schema"
	"vidlens/internal/domain"
)

// A compatible provider may call an exhausted response "tool_calls". Only
// reported usage plus actually incomplete arguments establish this boundary;
// complete replies and unknown usage keep their normal validation path.
func loopReportedOutputCap(p articleLoopCall, msg *schema.Message) bool {
	return p.Mode == "review" && p.ReferenceRepairKey == "" && p.Scope != nil && p.OutputTokens > 0 && msg != nil && len(msg.ToolCalls) == 1 && !json.Valid([]byte(msg.ToolCalls[0].Function.Arguments)) && msg.ResponseMeta != nil && msg.ResponseMeta.Usage != nil && msg.ResponseMeta.Usage.CompletionTokens >= p.OutputTokens
}

func (s *session) narrowMeasuredReviewOutput(ctx context.Context, p articleLoopCall) (bool, error) {
	key := fmt.Sprint("model:", p.Slot)
	op, err := s.engine.Store.Operation(ctx, s.claim.Run.ID, key)
	if err != nil {
		return false, err
	}
	if err = s.engine.Store.RecordSubmission(ctx, s.claim, key, op.Submissions, map[string]any{"failure_code": "MODEL_OUTPUT_TRUNCATED", "failure_message": "供应商报告已达输出上限且工具参数不完整；原回包保留，不把结束标记当完整语义结果。", "tool_arguments_valid": false, "output_limit_reached": true}); err != nil {
		return false, err
	}
	before := s.cp.ArticleLoop.NoProgress
	err = s.recoverLoopFailure(ctx, p, &domain.WorkError{Code: "MODEL_OUTPUT_TRUNCATED", Message: "实际报告的输出额度已耗尽，工具参数未完成"})
	if err == nil {
		s.cp.ArticleLoop.NoProgress = before
	} // Outer turn accounts for this failed action once.
	return false, err
}
