package investigator

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"sort"
	"strconv"
	"time"
	"unicode/utf8"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/jackc/pgx/v5"
	"vidlens/internal/domain"
)

type meteredModel struct {
	session    *session
	tools      []*schema.ToolInfo
	toolsChars int
}

func (m *meteredModel) WithTools(t []*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	chars := 0
	for _, info := range t {
		var parameters any
		if info.ParamsOneOf != nil {
			p, err := info.ParamsOneOf.ToJSONSchema()
			if err != nil {
				return nil, err
			}
			parameters = p
		}
		chars += utf8.RuneCount(domain.JSON(map[string]any{"type": "function", "function": map[string]any{"name": info.Name, "description": info.Desc, "parameters": parameters}}))
	}
	return &meteredModel{session: m.session, tools: append([]*schema.ToolInfo{}, t...), toolsChars: chars}, nil
}

func (m *meteredModel) Generate(ctx context.Context, in []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	s := m.session
	if !s.reviewing {
		if err := s.autoFinalize(ctx); err != nil {
			return nil, err
		}
	}
	key := "model:" + strconv.Itoa(s.cp.Calls)
	var args any = stripPixels(in)
	activeTools := model.GetCommonOptions(&model.Options{Tools: m.tools}, opts...).Tools
	activeTools = s.phaseTools(activeTools)
	if s.boundedSourceContext() && !s.reviewing {
		activeTools = append([]*schema.ToolInfo{}, activeTools...)
		for i, info := range activeTools {
			if info.Name == "record_source_notes" {
				// The agent binds tools once, while the delivered chunk advances
				// between calls. Refresh before freezing this logical input;
				// recovery below still restores its exact persisted schema.
				current, err := s.tools[info.Name].Info(ctx)
				if err != nil {
					return nil, err
				}
				activeTools[i] = current
			}
		}
	}
	if s.forceDraftSubmit {
		subset := []*schema.ToolInfo{}
		for _, t := range activeTools {
			if t.Name == "submit_article_draft" {
				subset = append(subset, t)
			}
		}
		activeTools = subset
	}
	bound, err := m.WithTools(activeTools)
	if err != nil {
		return nil, err
	}
	inputChars := utf8.RuneCount(domain.JSON(args)) + bound.(*meteredModel).toolsChars
	var budget callBudget
	outputTokens := s.profile.MaxOutputTokens
	if s.policy().Enabled {
		var frozen frozenModelInput
		saved, e := s.engine.Store.Operation(ctx, s.claim.Run.ID, "input:"+key)
		if e == nil && saved.Status == "completed" {
			e = json.Unmarshal(saved.Result, &frozen)
		} else if e == nil || errors.Is(e, pgx.ErrNoRows) {
			budget, e = s.budget(ctx, in, bound.(*meteredModel).toolsChars)
			if e != nil {
				var problem *domain.Problem
				if errors.As(e, &problem) && problem.Meta["budget_reason"] != nil {
					if saveErr := s.engine.Store.BudgetDiagnostic(ctx, s.claim, key, map[string]any{
						"budget": budget, "reason": problem.Meta["budget_reason"], "model_revision_id": s.spec.Model.RevisionID,
					}); saveErr != nil {
						return nil, saveErr
					}
				}
			}
			if e == nil {
				args = map[string]any{"messages": stripPixels(in), "budget": budget, "tools_hash": digest(domain.JSON(activeTools)), "policy_revision": s.spec.DetailPolicyRevision}
				frozen, e = s.freezeModelInput(ctx, key, in, args, budget, activeTools)
			}
		}
		if e != nil {
			return nil, e
		}
		// PostgreSQL jsonb reorders object keys. Decode back to a canonical map
		// before hashing; hashing RawMessage bytes would conflict on takeover.
		if e = json.Unmarshal(frozen.Args, &args); e != nil {
			return nil, e
		}
		budget = frozen.Budget
		outputTokens = budget.OutputTokens
		in, e = s.hydrate(ctx, frozen.Messages)
		if e != nil {
			return nil, e
		}
		selected := []*schema.ToolInfo{}
		for _, name := range frozen.ToolNames {
			t := s.tools[name]
			if t == nil {
				return nil, domain.Err(422, "INTERNAL_FAILURE", "冻结调用的工具协议不兼容。")
			}
			info, e := t.Info(ctx)
			if e != nil {
				return nil, e
			}
			selected = append(selected, info)
		}
		activeTools = selected
		if frozen.ToolSchemaJSON != "" {
			if e = json.Unmarshal([]byte(frozen.ToolSchemaJSON), &activeTools); e != nil {
				return nil, e
			}
		} else if expected, ok := args.(map[string]any); ok && expected["tools_hash"] != digest(domain.JSON(activeTools)) {
			return nil, domain.Err(422, "MODEL_RESULT_INVALID", "冻结调用的工具协议已改变，原调用不会重新发送；已保存草稿可通过显式重新尝试接续。")
		}
		s.modelMessages = stripPixels(in)
	} else if inputChars+s.profile.MaxOutputTokens*4 > s.profile.MaxInputChars {
		return nil, domain.Err(422, "RESOURCE_LIMIT_EXCEEDED", "完整上下文与输出预留超过模型限制；已保留来源，不会截断全片后继续生成。")
	}
	op, err := s.engine.Store.BeginOperation(ctx, s.claim, key, "model", args, false)
	if err != nil {
		return nil, err
	}
	if op.Status == "completed" {
		var saved schema.Message
		if err = json.Unmarshal(op.Result, &saved); err != nil {
			return nil, err
		}
		return &saved, nil
	}
	if s.reviewStep != nil {
		// A complete length-limited submission is a known outcome. On takeover,
		// replay it into the review planner instead of paying for the same input.
		truncated, err := s.engine.Store.ModelOutputTruncated(ctx, s.claim.Run.ID, key)
		if err != nil {
			return nil, err
		}
		if truncated {
			return nil, &domain.WorkError{Code: "MODEL_OUTPUT_TRUNCATED", Message: "模型输出达到长度上限，未产生完整结果。"}
		}
	}
	routes, err := s.engine.Store.ModelRoutes(ctx, s.claim.Run.ConfigID)
	if err != nil {
		return nil, err
	}
	attempts, err := s.engine.Store.ModelRouteAttempts(ctx, s.claim.Run.ID, key, s.claim.Run.ConfigID)
	if err != nil {
		return nil, err
	}
	preferred, err := s.engine.Store.SuccessfulModelRoute(ctx, s.claim.Run.ID)
	if err != nil {
		return nil, err
	}
	cooldowns, err := s.engine.Store.ModelRouteCooldowns(ctx, s.claim.Run.ID)
	if err != nil {
		return nil, err
	}
	// Recovery prefers routes with fewer submissions. Configured priority orders
	// healthy routes with equal attempt counts; a recent transport failure in
	// this run temporarily yields to another eligible route of the exact model.
	sort.SliceStable(routes, func(i, j int) bool {
		a, b := routes[i].ConfigID, routes[j].ConfigID
		if cooldowns[a] != cooldowns[b] {
			return !cooldowns[a]
		}
		if attempts[a] != attempts[b] {
			return attempts[a] < attempts[b]
		}
		priorityA, priorityB := routes[i].RoutePriority, routes[j].RoutePriority
		if priorityA == 0 {
			priorityA = 1001
		}
		if priorityB == 0 {
			priorityB = 1001
		}
		if priorityA != priorityB {
			return priorityA < priorityB
		}
		return a == preferred && b != preferred
	})
	single := len(routes) == 1
	if single {
		// Preserve the small existing retry for a single transiently failing route.
		routes = append(routes, routes[0])
	}
	title, phase := s.modelActivity()
	step := domain.Step{ID: "model_" + s.claim.Run.ID + "_" + strconv.Itoa(s.cp.Calls), Kind: "model_response", ModelCall: s.cp.Calls + 1, Title: modelCallTitle(s.cp.Calls + 1), Purpose: domain.Ptr("正在等待模型可用名额"), Phase: phase, Status: "running", FrameIDs: []string{}, StartedAt: domain.Ptr(time.Now().UTC())}
	open, err := s.engine.Store.OpenModelSubmissions(ctx, s.claim.Run.ID, step.ModelCall)
	if err != nil {
		return nil, err
	}
	for _, previous := range open {
		previous.Status = "failed"
		previous.FinishedAt = domain.Ptr(time.Now().UTC())
		previous.Outcome = &domain.Outcome{Message: "上次执行未保存完整响应，结果未知；原发送记录保留。", EvidenceStatus: "not_assessed"}
		if _, err = s.engine.Store.Step(ctx, s.claim, previous); err != nil {
			return nil, err
		}
	}
	step, err = s.engine.Store.Step(ctx, s.claim, step)
	if err != nil {
		return nil, err
	}
	var last error
	for i, route := range routes {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if single && i > 0 {
			var work *domain.WorkError
			if !errors.As(last, &work) || !work.Transient || work.RetryAfter > time.Second {
				break
			}
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(time.Second):
			}
		}
		p := route.Profile
		// Failover may not silently reduce the accepted output budget or truncate
		// context to fit a supplier with lower configured limits.
		fits := inputChars+s.profile.MaxOutputTokens*4 <= p.MaxInputChars
		if s.policy().Enabled {
			fits = budget.check(p) == nil
		}
		if p.MaxOutputTokens < outputTokens || !fits {
			if last == nil {
				last = domain.Err(422, "RESOURCE_LIMIT_EXCEEDED", "可用模型线路无法容纳本次完整上下文与输出额度。")
			}
			continue
		}
		p.MaxOutputTokens = outputTokens
		p.CacheHintsVerified = false
		if p.CachePolicy == "qwen_explicit" {
			p.CacheHintsVerified, err = s.engine.Store.CacheHintsVerified(ctx, route.ConfigID)
			if err != nil {
				return nil, err
			}
		}
		baseCtx := ctx
		if s.authoring() {
			baseCtx = context.WithValue(ctx, nativeStrictKey{}, true)
		}
		base, err := s.engine.baseModel(baseCtx, p)
		if err == nil && activeTools != nil {
			base, err = base.WithTools(activeTools)
		}
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			last = &domain.WorkError{Code: "PROVIDER_UNAVAILABLE", Message: "模型线路配置暂不可用。"}
			continue
		}
		actx, release, err := s.engine.Resources.Acquire(ctx, s.claim, "ai")
		if err != nil {
			return nil, err
		}
		step.Purpose = domain.Ptr(title)
		if _, err = s.engine.Store.Step(ctx, s.claim, step); err != nil {
			release()
			return nil, err
		}
		limits := modelResponseLimits{Total: 180 * time.Second, First: 90 * time.Second, Idle: 60 * time.Second}
		if s.compositionReady {
			limits.Total = 300 * time.Second
		}
		if s.reviewStep != nil {
			limits.Total = 300 * time.Second
		}
		submission := 0
		var transport *domain.Step
		observer := newUsageObserver(s.cp.LayoutVersion, p.CachePolicy, func(report usageReport) {
			cctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			if err := s.engine.Store.RecordSubmission(cctx, s.claim, key, submission, report); err != nil {
				slog.Warn("model usage observation unavailable")
			}
		})
		var submitErr error
		sub := context.WithValue(actx, usageKey{}, observer)
		sub = context.WithValue(sub, remoteSubmitKey{}, func() error {
			reserved, err := s.engine.Store.BeginModelSubmission(actx, s.claim, key, args, route.ConfigID)
			submission, submitErr = reserved.Submissions, err
			if err == nil && s.policy().Enabled {
				err = s.engine.Store.RecordSubmission(actx, s.claim, key, submission, map[string]any{"budget": budget})
				submitErr = err
			}
			if err == nil {
				x := domain.Step{ID: step.ID + "_send_" + strconv.Itoa(submission), Kind: "model_submission", ModelCall: step.ModelCall, Submission: submission, Title: "模型调用 " + strconv.Itoa(step.ModelCall) + " · 发送 " + strconv.Itoa(submission), Phase: step.Phase, Status: "running", FrameIDs: []string{}, StartedAt: domain.Ptr(time.Now().UTC())}
				x, err = s.engine.Store.Step(actx, s.claim, x)
				transport = &x
				submitErr = err
			}
			return err
		})
		reportedActivity := ""
		sub = context.WithValue(sub, modelActivityKey{}, func(name string) error {
			title := streamActivity(name)
			if title == "" || reportedActivity == title {
				return nil
			}
			reportedActivity = title
			step.Purpose = domain.Ptr(title)
			switch name {
			case "begin_article", "repair_article_plan", "save_article_sections":
				step.Phase = "compose"
			case "submit_article", "submit_article_draft":
				step.Phase = "validate"
			}
			_, err := s.engine.Store.Step(ctx, s.claim, step)
			return err
		})
		options := append(cacheOptions(p), opts...)
		if s.policy().Enabled {
			options = append(options, model.WithTools(activeTools))
		}
		if s.reviewing {
			name := "report_evidence_review"
			if s.reviewStep != nil {
				name = reviewUnitsTool
			}
			options = append(options, model.WithToolChoice(schema.ToolChoiceForced, name))
		} else if s.forceDraftSubmit {
			options = append(options, model.WithTools(activeTools), model.WithToolChoice(schema.ToolChoiceForced, "submit_article_draft"))
		} else if s.policy().Recovery && len(activeTools) == 1 && (activeTools[0].Name == "repair_article_plan" || activeTools[0].Name == "repair_article_sections") {
			options = append(options, model.WithToolChoice(schema.ToolChoiceForced, activeTools[0].Name))
		} else if s.turn != nil && s.policy().ComposedAnswers {
			// Require an executable action, while retaining evidence tools when
			// the question needs investigation. No plain-text-then-submit round.
			options = append(options, model.WithToolChoice(schema.ToolChoiceForced))
		}
		result, err := receiveModelResponse(sub, base, NormalizeToolImages(in), limits, options...)
		release()
		if errors.Is(err, errEmptyModelResponse) {
			// Preserve the provider's complete stream/usage report while recording
			// that it supplied no executable action or visible answer.
			if saveErr := s.engine.Store.RecordSubmission(ctx, s.claim, key, submission, map[string]any{"response_empty": true}); saveErr != nil {
				return nil, saveErr
			}
		}
		if transport != nil && ctx.Err() == nil {
			transport.Status = "completed"
			message := "已收到模型响应，接下来校验工具参数与内容。"
			if err == nil && !validModelToolArguments(result) {
				message = "响应已结束，但工具参数JSON不完整或无效；尚未执行动作。"
			}
			if err != nil {
				transport.Status = "failed"
				message = modelFailureMessage(err)
				report := map[string]any{"failure_message": message}
				var work *domain.WorkError
				if errors.As(err, &work) {
					report["failure_code"] = work.Code
					if work.Transient && (work.Code == "PROVIDER_UNAVAILABLE" || work.Code == "PROVIDER_RATE_LIMITED") {
						report["route_cooldown_until"] = time.Now().UTC().Add(min(5*time.Minute, max(2*time.Minute, work.RetryAfter)))
					}
				}
				if saveErr := s.engine.Store.RecordSubmission(ctx, s.claim, key, submission, report); saveErr != nil {
					return nil, saveErr
				}
			}
			transport.FinishedAt = domain.Ptr(time.Now().UTC())
			transport.Outcome = &domain.Outcome{Message: message, EvidenceStatus: "not_assessed"}
			if _, saveErr := s.engine.Store.Step(ctx, s.claim, *transport); saveErr != nil {
				return nil, saveErr
			}
		}
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return nil, err
			}
			if submitErr != nil {
				var problem *domain.Problem
				if errors.As(submitErr, &problem) && problem.Code == "MODEL_UNAVAILABLE" {
					last = submitErr
					continue
				}
				if errors.As(submitErr, &problem) && problem.Code == "PROVIDER_UNAVAILABLE" && s.policy().Recovery {
					if delay, reserved, saveErr := s.engine.Store.ScheduleModelRecovery(ctx, s.claim, key); saveErr != nil {
						return nil, saveErr
					} else if reserved {
						submitErr = &domain.WorkError{Code: "PROVIDER_UNAVAILABLE", Message: "本轮模型响应连续失败，已安排等待后从原进度自动恢复。", Transient: true, RetryAfter: delay}
					}
				}
				return nil, s.failModelStep(ctx, step, submitErr)
			}
			var work *domain.WorkError
			if errors.As(err, &work) {
				if work.Code != "PROVIDER_UNAVAILABLE" && work.Code != "PROVIDER_RATE_LIMITED" {
					return nil, s.failModelStep(ctx, step, work)
				}
				last = work
			} else {
				slog.Warn("model invocation rejected", "class", "incompatible_response")
				last = &domain.WorkError{Code: "PROVIDER_UNAVAILABLE", Message: "模型请求格式不兼容。"}
			}
			continue
		}
		result.ReasoningContent = ""
		result.Extra = cleanExtra(result.Extra)
		if s.policy().ObservationBudget {
			var exhausted *bool
			if result.ResponseMeta != nil && result.ResponseMeta.Usage != nil && result.ResponseMeta.Usage.CompletionTokens > 0 {
				exhausted = domain.Ptr(result.ResponseMeta.Usage.CompletionTokens >= outputTokens)
			}
			if err = s.engine.Store.RecordSubmission(ctx, s.claim, key, submission, map[string]any{"tool_arguments_valid": validModelToolArguments(result), "output_limit_reached": exhausted}); err != nil {
				return nil, err
			}
		}
		if s.policy().Enabled {
			// Some compatible suppliers reuse IDs across separate responses.
			// Eino caches tool results by ID, so scope them to this logical call
			// before persistence/dispatch. A worker replay keeps the saved IDs.
			for j := range result.ToolCalls {
				result.ToolCalls[j].ID = "vl_" + digest(domain.JSON(map[string]any{"operation": key, "provider_call_id": result.ToolCalls[j].ID, "index": j}))[:40]
			}
		}
		var usage any
		if result.ResponseMeta != nil {
			usage = result.ResponseMeta.Usage
		}
		if err = s.engine.Store.FinishOperation(ctx, s.claim, key, result, providerID(result), usage); err != nil {
			return nil, err
		}
		step.Status = "completed"
		step.FinishedAt = domain.Ptr(time.Now().UTC())
		responseTitle, outcome := responseActivity(result)
		step.Purpose = domain.Ptr(responseTitle)
		step.Outcome = &domain.Outcome{Message: outcome, EvidenceStatus: "not_assessed"}
		if _, err = s.engine.Store.Step(ctx, s.claim, step); err != nil {
			return nil, err
		}
		slog.Info("model response completed", "run_id", s.claim.Run.ID, "attempt_id", s.claim.AttemptID, "operation_key", key, "submission", submission, "tool_calls", len(result.ToolCalls))
		return result, nil
	}
	return nil, s.failModelStep(ctx, step, last)
}

func modelFailureMessage(err error) string {
	var work *domain.WorkError
	if errors.As(err, &work) {
		return work.Message
	}
	var problem *domain.Problem
	if errors.As(err, &problem) {
		return problem.Detail
	}
	return "未取得完整模型响应；未将未完成内容作为结果。"
}

func (s *session) failModelStep(ctx context.Context, step domain.Step, cause error) error {
	step.Status = "failed"
	step.FinishedAt = domain.Ptr(time.Now().UTC())
	step.Outcome = &domain.Outcome{Message: modelFailureMessage(cause), EvidenceStatus: "not_assessed"}
	if _, err := s.engine.Store.Step(ctx, s.claim, step); err != nil {
		return err
	}
	return cause
}
