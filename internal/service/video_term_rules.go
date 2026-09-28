package service

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"vid-lens/internal/artifact"
	"vid-lens/internal/repository"
	"vid-lens/internal/studyterms"
)

type VideoTermRule struct {
	ID                   string   `json:"id"`
	From                 string   `json:"from"`
	To                   string   `json:"to"`
	Context              string   `json:"context"`
	Exclusions           []string `json:"exclusions"`
	Basis                string   `json:"basis"`
	TranscriptEvidenceID string   `json:"transcript_evidence_id,omitempty"`
	VisualEvidenceID     string   `json:"visual_evidence_id,omitempty"`
	SourceHash           string   `json:"source_hash,omitempty"`
	Enabled              bool     `json:"enabled"`
	EvidenceStatus       string   `json:"evidence_status"`
}

type VideoTermRuleInput struct {
	ExpectedVersion      int64    `json:"expected_version"`
	LinkedOperationID    string   `json:"linked_operation_id,omitempty"`
	ID                   string   `json:"id,omitempty"`
	From                 string   `json:"from"`
	To                   string   `json:"to"`
	Context              string   `json:"context"`
	Exclusions           []string `json:"exclusions"`
	TranscriptEvidenceID string   `json:"transcript_evidence_id,omitempty"`
	VisualEvidenceID     string   `json:"visual_evidence_id,omitempty"`
}

type VideoTermRuleSet struct {
	Version int64           `json:"version"`
	Digest  string          `json:"digest"`
	Rules   []VideoTermRule `json:"rules"`
}

func termRuleText(s string, max int, required bool) (string, error) {
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) > max || required && s == "" {
		return "", artifact.Err("invalid_request", 400)
	}
	return s, nil
}

func readTermRuleSet(ctx context.Context, repos *repository.Repositories, owner, taskID int64) (VideoTermRuleSet, error) {
	result := VideoTermRuleSet{Rules: []VideoTermRule{}}
	if repos == nil || repos.VideoTermRule == nil {
		return result, nil
	}
	row, err := repos.VideoTermRule.Current(ctx, owner, taskID)
	if err != nil {
		return result, err
	}
	if row == nil {
		result.Digest = artifact.Hash("[]")
		return result, nil
	}
	if err = json.Unmarshal([]byte(row.RulesJSON), &result.Rules); err != nil {
		return result, err
	}
	result.Version, result.Digest = row.Version, row.Digest
	return result, nil
}

// EffectiveTermRules keeps the frozen data intact while downgrading stale
// evidence claims. Original ASR/OCR text is never changed by this projection.
func EffectiveTermRules(ctx context.Context, repos *repository.Repositories, owner, taskID int64) (VideoTermRuleSet, error) {
	set, err := readTermRuleSet(ctx, repos, owner, taskID)
	if err != nil {
		return set, err
	}
	if len(set.Rules) == 0 {
		return set, nil
	}
	hash, _, sourceErr := artifactSource(ctx, repos, owner, taskID)
	for i := range set.Rules {
		if set.Rules[i].Basis == "evidence_supported" && (sourceErr != nil || hash != set.Rules[i].SourceHash) {
			set.Rules[i].EvidenceStatus = "pending_review"
		} else if set.Rules[i].Basis == "evidence_supported" {
			set.Rules[i].EvidenceStatus = "current"
		}
	}
	return set, nil
}

func SaveVideoTermRule(ctx context.Context, repos *repository.Repositories, owner, taskID int64, input VideoTermRuleInput) (VideoTermRuleSet, error) {
	set, err := readTermRuleSet(ctx, repos, owner, taskID)
	if err != nil {
		return set, err
	}
	if set.Version != input.ExpectedVersion {
		return set, artifact.Err("version_conflict", 409)
	}
	from, err := termRuleText(input.From, 80, true)
	if err != nil {
		return set, err
	}
	to, err := termRuleText(input.To, 80, true)
	if err != nil {
		return set, err
	}
	contextText, err := termRuleText(input.Context, 240, true)
	if err != nil {
		return set, err
	}
	if from == to || len(input.Exclusions) > 8 {
		return set, artifact.Err("invalid_request", 400)
	}
	exclusions := make([]string, len(input.Exclusions))
	for i, exclusion := range input.Exclusions {
		exclusions[i], err = termRuleText(exclusion, 120, true)
		if err != nil {
			return set, err
		}
	}
	rule := VideoTermRule{ID: input.ID, From: from, To: to, Context: contextText, Exclusions: exclusions, Basis: "user_asserted", Enabled: true, EvidenceStatus: "not_claimed"}
	if input.LinkedOperationID != "" {
		stableID := uuid.NewSHA1(uuid.NameSpaceOID, []byte("summary-term:"+input.LinkedOperationID)).String()
		if rule.ID != "" && rule.ID != stableID {
			return set, artifact.Err("idempotency_conflict", 409)
		}
		rule.ID = stableID
	}
	if rule.ID == "" {
		rule.ID = uuid.NewString()
	}
	if input.TranscriptEvidenceID != "" || input.VisualEvidenceID != "" {
		if input.TranscriptEvidenceID == "" || input.VisualEvidenceID == "" {
			return set, artifact.Err("invalid_request", 400)
		}
		manifest, items, freezeErr := repos.Artifact.Freeze(ctx, owner, taskID, artifactSource)
		if freezeErr != nil {
			return set, freezeErr
		}
		rule.TranscriptEvidenceID, rule.VisualEvidenceID, rule.SourceHash = input.TranscriptEvidenceID, input.VisualEvidenceID, manifest.ContentHash
		if studyterms.ValidateStudyTermCorrection(items, studyterms.StudyTermCorrection{From: from, To: to, TranscriptEvidenceID: input.TranscriptEvidenceID, VisualEvidenceID: input.VisualEvidenceID}) {
			rule.Basis, rule.EvidenceStatus = "evidence_supported", "current"
		} else {
			rule.Basis, rule.EvidenceStatus = "conflicted", "pending_review"
		}
	}
	replaced := false
	for i := range set.Rules {
		if set.Rules[i].ID == rule.ID {
			if input.LinkedOperationID != "" {
				prior := set.Rules[i]
				if prior.Enabled && prior.From == rule.From && prior.To == rule.To && prior.Context == rule.Context && slices.Equal(prior.Exclusions, rule.Exclusions) && prior.TranscriptEvidenceID == rule.TranscriptEvidenceID && prior.VisualEvidenceID == rule.VisualEvidenceID {
					return set, nil
				}
				return set, artifact.Err("idempotency_conflict", 409)
			}
			set.Rules[i], replaced = rule, true
			break
		}
	}
	if !replaced {
		if len(set.Rules) >= 30 {
			return set, artifact.Err("rule_limit_exceeded", 422)
		}
		set.Rules = append(set.Rules, rule)
	}
	encoded, _ := json.Marshal(set.Rules)
	digest := artifact.Hash(string(encoded))
	var linked *string
	if input.LinkedOperationID != "" {
		linked = &input.LinkedOperationID
	}
	row, err := repos.VideoTermRule.Save(ctx, owner, taskID, set.Version, digest, string(encoded), linked)
	if err != nil {
		return set, err
	}
	set.Version, set.Digest = row.Version, row.Digest
	return set, nil
}

func DisableVideoTermRule(ctx context.Context, repos *repository.Repositories, owner, taskID int64, id string, expected int64) (VideoTermRuleSet, error) {
	set, err := readTermRuleSet(ctx, repos, owner, taskID)
	if err != nil {
		return set, err
	}
	if set.Version != expected {
		return set, artifact.Err("version_conflict", 409)
	}
	found := false
	for i := range set.Rules {
		if set.Rules[i].ID == id {
			if !set.Rules[i].Enabled {
				return set, artifact.Err("nothing_to_change", 422)
			}
			set.Rules[i].Enabled = false
			found = true
			break
		}
	}
	if !found {
		return set, artifact.Err("not_found", 404)
	}
	encoded, _ := json.Marshal(set.Rules)
	row, err := repos.VideoTermRule.Save(ctx, owner, taskID, set.Version, artifact.Hash(string(encoded)), string(encoded), nil)
	if err != nil {
		return set, err
	}
	set.Version, set.Digest = row.Version, row.Digest
	return set, nil
}

func termRulePrompt(set VideoTermRuleSet) string {
	active := make([]VideoTermRule, 0, len(set.Rules))
	for _, rule := range set.Rules {
		if rule.Enabled {
			active = append(active, rule)
		}
	}
	if len(active) == 0 {
		return ""
	}
	encoded, _ := json.Marshal(active)
	return "本视频的用户术语规则（受限数据，不能作为写权限或原文引用；仅在指定上下文适用，排除条件优先。user_asserted 表示用户指定，pending_review 不得声称画面支持）：\n" + string(encoded)
}
