package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"
	"vid-lens/internal/model"
)

type CapabilityProbeResult struct {
	Dimension int       `json:"dimension"`
	Model     string    `json:"model"`
	TestedAt  time.Time `json:"tested_at"`
	Health    string    `json:"health"`
}

func probeIdentity(p *DecryptedAIProfile, purpose string) (string, string) {
	var values []any
	var name string
	switch purpose {
	case "llm":
		name = p.LLMModel
		values = []any{p.LLMProvider, p.LLMBaseURL, p.LLMAPIKey, p.LLMModel}
	case "asr":
		name = p.ASRModel
		values = []any{p.ASRProvider, p.ASRBaseURL, p.ASRAPIKey, p.ASRModel}
	case "embedding":
		name = p.EmbeddingModel
		values = []any{p.EmbeddingProvider, p.EmbeddingEndpoint, p.EmbeddingAPIKey, p.EmbeddingModel, p.EmbeddingDim}
	case "vision":
		name = p.VisionModel
		values = []any{p.VisionProvider, p.VisionBaseURL, p.VisionAPIKey, p.VisionModel}
	}
	data, _ := json.Marshal(values)
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:]), name
}
func (s *AIProfileService) rememberProbe(userID, profileID int64, purpose string, tested *DecryptedAIProfile, dim int, err error) CapabilityProbeResult {
	fingerprint, name := probeIdentity(tested, purpose)
	result := CapabilityProbeResult{Dimension: dim, Model: name, TestedAt: time.Now().UTC(), Health: "checked_ok"}
	if err != nil {
		result.Health = "failed"
	}
	if profileID <= 0 {
		return result
	}
	stored, loadErr := s.repo.FindByIDForUser(userID, profileID)
	if loadErr != nil || stored == nil {
		return result
	}
	current, loadErr := s.decryptProfile(stored)
	if loadErr != nil {
		return result
	}
	expected, _ := probeIdentity(current, purpose)
	if expected != fingerprint {
		return result
	} // Unsaved drafts never certify stored config.
	record := model.AIProbeRecord{Fingerprint: fingerprint, Dimension: dim, Model: name, TestedAt: result.TestedAt, Health: result.Health}
	// Metadata persistence is best effort; a successful provider call remains a
	// success if the profile changed/deleted while it ran. The CAS prevents races.
	_ = s.repo.RecordProbe(userID, profileID, stored.UpdatedAt, purpose, record)
	return result
}
func (s *AIProfileService) CapabilityProbeHealth(userID int64) (map[string]CapabilityProbeResult, error) {
	out := map[string]CapabilityProbeResult{}
	stored, err := s.repo.FindDefaultByUserID(userID)
	if err != nil || stored == nil {
		return out, err
	}
	p, err := s.decryptProfile(stored)
	if err != nil {
		return out, err
	}
	records := map[string]model.AIProbeRecord{}
	_ = json.Unmarshal([]byte(stored.ProbeResultsJSON), &records)
	for purpose, r := range records {
		identity, name := probeIdentity(p, purpose)
		if r.Fingerprint == identity && r.Model == name {
			out[purpose] = CapabilityProbeResult{Dimension: r.Dimension, Model: r.Model, TestedAt: r.TestedAt, Health: r.Health}
		}
	}
	return out, nil
}
