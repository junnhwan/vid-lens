package investigator

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5"
	"vidlens/internal/domain"
	"vidlens/internal/store"
)

func (s *session) requireAnswerPublicationReview(ctx context.Context, a domain.Answer) error {
	candidate := map[string]any{"kind": a.Kind, "summary": a.Summary, "blocks": a.Blocks, "parts": a.Parts, "limitations": a.Limitations}
	err := s.requirePublicationReview(ctx, candidate, a.Citations)
	var problem *domain.Problem
	if err == nil || len(a.Parts) != 0 || !errors.As(err, &problem) || problem.Code != "REVIEW_COVERAGE_INCOMPLETE" {
		return err
	}
	// Answer.Parts is omitempty: both an approved [] and an approved nil are
	// persisted without this field and restored as nil. Retain existing v2
	// reviewer hashes and frozen model inputs. Only this semantically empty
	// projection may vary, and one complete hash's entire tree must pass; never
	// combine coverage from the two identities or initiate missing review work.
	if a.Parts == nil {
		candidate["parts"] = []domain.AnswerPart{}
	} else {
		candidate["parts"] = []domain.AnswerPart(nil)
	}
	return s.requirePublicationReview(ctx, candidate, a.Citations)
}

// A candidate operation is durable author output, not proof of independent
// approval. In particular, Engine.Run can resume directly at publication before
// tools or model context are initialized. This gate only reads completed review
// operations, retaining the candidate and never spending another provider call.
func (s *session) requirePublicationReview(ctx context.Context, candidate any, citations map[string]domain.Citation) error {
	if !s.usesBoundedReview() || !s.policy().ContentReview {
		return nil // Historical checkpoints retain their frozen contract.
	}
	hash, catalog, _, _, err := s.contentReviewIdentity(ctx, candidate, citations)
	if err != nil {
		return err
	}
	units, err := candidateReviewUnits(candidate, catalog)
	if err != nil {
		return publicationReviewIncomplete()
	}
	tasks, err := planReviewTasks(units, s.profile.MaxOutputTokens)
	if err != nil {
		return publicationReviewIncomplete()
	}
	for _, task := range tasks {
		if err := s.requireReviewedTask(ctx, hash, task, catalog); err != nil {
			return err
		}
	}
	return nil
}

func (s *session) requireReviewedTask(ctx context.Context, hash string, task reviewTask, catalog map[string]domain.Citation) error {
	ids := make([]string, 0, len(task.Units))
	for _, unit := range task.Units {
		ids = append(ids, unit.ID)
	}
	key := "review-units:" + store.Hash(map[string]any{"candidate_hash": hash, "mode": task.Mode, "unit_ids": ids})
	id := "r_" + strings.TrimPrefix(key, "review-units:")[:24]
	argsHash := store.Hash(map[string]any{"request_id": id, "unit_ids": ids, "mode": task.Mode, "protocol": boundedReviewProtocol})
	op, err := s.engine.Store.Operation(ctx, s.claim.Run.ID, key)
	if errors.Is(err, pgx.ErrNoRows) {
		return publicationReviewIncomplete()
	}
	if err != nil {
		return err
	}
	var saved reviewBatchOutcome
	if op.Status != "completed" || op.Kind != "review_batch" || op.ArgsHash != argsHash || json.Unmarshal(op.Result, &saved) != nil || saved.RequestID != id || saved.Mode != task.Mode || !slices.Equal(saved.UnitIDs, ids) {
		return publicationReviewIncomplete()
	}
	switch saved.State {
	case "complete":
		if saved.Report == nil || validateReviewUnits(*saved.Report, id, task, catalog) != nil {
			return publicationReviewIncomplete()
		}
		for _, result := range saved.Report.Results {
			if result.Verdict != "supported" {
				return publicationReviewIncomplete()
			}
		}
		return nil
	case "split":
		if len(task.Units) < 2 {
			return publicationReviewIncomplete()
		}
		middle := len(task.Units) / 2
		for _, units := range [][]reviewUnit{task.Units[:middle], task.Units[middle:]} {
			if err := s.requireReviewedTask(ctx, hash, reviewTask{Mode: task.Mode, Units: units}, catalog); err != nil {
				return err
			}
		}
		return nil
	default:
		return publicationReviewIncomplete()
	}
}

func publicationReviewIncomplete() error {
	return domain.Err(422, "REVIEW_COVERAGE_INCOMPLETE", "已保存候选缺少与当前来源及执行协议一致的完整通过记录，不能发布；候选和审核记录已保留。")
}
