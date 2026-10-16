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
	key := "model:" + strconv.Itoa(s.cp.Calls)
	args := stripPixels(in)
	inputChars := utf8.RuneCount(domain.JSON(args)) + m.toolsChars
	if inputChars+s.profile.MaxOutputTokens*4 > s.profile.MaxInputChars {
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
	// A recovered operation tries an unused supplier first. Later Agent calls
	// continue on the last successful route instead of repeatedly hitting a failure.
	sort.SliceStable(routes, func(i, j int) bool {
		a, b := routes[i].ConfigID, routes[j].ConfigID
		if attempts[a] != attempts[b] {
			return attempts[a] < attempts[b]
		}
		return a == preferred && b != preferred
	})
	single := len(routes) == 1
	if single {
		// Preserve the small existing retry for a single transiently failing route.
		routes = append(routes, routes[0])
	}
	title, phase := s.modelActivity()
	step := domain.Step{ID: "model_" + s.claim.Run.ID + "_" + strconv.Itoa(s.cp.Calls), Kind: "model_response", Title: "正在等待模型可用名额", Phase: phase, Status: "running", FrameIDs: []string{}, StartedAt: domain.Ptr(time.Now().UTC())}
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
		if p.MaxOutputTokens < s.profile.MaxOutputTokens || inputChars+s.profile.MaxOutputTokens*4 > p.MaxInputChars {
			if last == nil {
				last = domain.Err(422, "RESOURCE_LIMIT_EXCEEDED", "可用模型线路无法容纳本次完整上下文与输出额度。")
			}
			continue
		}
		p.MaxOutputTokens = s.profile.MaxOutputTokens
		p.CacheHintsVerified = false
		if p.CachePolicy == "qwen_explicit" {
			p.CacheHintsVerified, err = s.engine.Store.CacheHintsVerified(ctx, route.ConfigID)
			if err != nil {
				return nil, err
			}
		}
		base, err := s.engine.baseModel(ctx, p)
		if err == nil && m.tools != nil {
			base, err = base.WithTools(m.tools)
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
		step.Title = title
		if _, err = s.engine.Store.Step(ctx, s.claim, step); err != nil {
			release()
			return nil, err
		}
		limits := modelResponseLimits{Total: 180 * time.Second, First: 90 * time.Second, Idle: 60 * time.Second}
		if s.compositionReady {
			limits.Total = 300 * time.Second
		}
		submission := 0
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
			return err
		})
		reportedActivity := ""
		sub = context.WithValue(sub, modelActivityKey{}, func(name string) error {
			title := streamActivity(name)
			if title == "" || reportedActivity == title {
				return nil
			}
			reportedActivity = title
			step.Title = title
			_, err := s.engine.Store.Step(ctx, s.claim, step)
			return err
		})
		options := append(cacheOptions(p), opts...)
		result, err := receiveModelResponse(sub, base, NormalizeToolImages(in), limits, options...)
		release()
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
				return nil, submitErr
			}
			var work *domain.WorkError
			if errors.As(err, &work) {
				if work.Code != "PROVIDER_UNAVAILABLE" && work.Code != "PROVIDER_RATE_LIMITED" {
					return nil, work
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
		var usage any
		if result.ResponseMeta != nil {
			usage = result.ResponseMeta.Usage
		}
		if err = s.engine.Store.FinishOperation(ctx, s.claim, key, result, providerID(result), usage); err != nil {
			return nil, err
		}
		step.Status = "completed"
		step.FinishedAt = domain.Ptr(time.Now().UTC())
		step.Title, title = responseActivity(result)
		step.Outcome = &domain.Outcome{Message: title, EvidenceStatus: "not_assessed"}
		if _, err = s.engine.Store.Step(ctx, s.claim, step); err != nil {
			return nil, err
		}
		slog.Info("model response completed", "run_id", s.claim.Run.ID, "attempt_id", s.claim.AttemptID, "operation_key", key, "submission", submission, "tool_calls", len(result.ToolCalls))
		return result, nil
	}
	return nil, last
}
