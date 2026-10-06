package repository

import (
	"encoding/json"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"time"
	"vid-lens/internal/model"
)

func (r *AIProfileRepository) RecordProbe(userID, id int64, updatedAt time.Time, purpose string, record model.AIProbeRecord) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		var profile model.UserAIProfile
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("user_id = ? AND id = ?", userID, id).First(&profile).Error; err != nil {
			return err
		}
		if !profile.UpdatedAt.Equal(updatedAt) {
			return nil
		}
		records := map[string]model.AIProbeRecord{}
		_ = json.Unmarshal([]byte(profile.ProbeResultsJSON), &records)
		records[purpose] = record
		data, err := json.Marshal(records)
		if err != nil {
			return err
		}
		return tx.Model(&model.UserAIProfile{}).Where("user_id = ? AND id = ? AND updated_at = ?", userID, id, updatedAt).UpdateColumn("probe_results_json", string(data)).Error
	})
}

// A configuration edit permanently invalidates the affected observation. Even
// switching back to the old model requires another manual probe.
func invalidateChangedProbes(existing, next *model.UserAIProfile) {
	changed := map[string]bool{
		"llm":       existing.LLMProvider != next.LLMProvider || existing.LLMBaseURL != next.LLMBaseURL || existing.LLMAPIKeyCiphertext != next.LLMAPIKeyCiphertext || existing.LLMModel != next.LLMModel || existing.LLMContextTokens != next.LLMContextTokens,
		"asr":       existing.ASRProvider != next.ASRProvider || existing.ASRBaseURL != next.ASRBaseURL || existing.ASRAPIKeyCiphertext != next.ASRAPIKeyCiphertext || existing.ASRModel != next.ASRModel,
		"embedding": existing.EmbeddingProvider != next.EmbeddingProvider || existing.EmbeddingEndpoint != next.EmbeddingEndpoint || existing.EmbeddingAPIKeyCiphertext != next.EmbeddingAPIKeyCiphertext || existing.EmbeddingModel != next.EmbeddingModel || existing.EmbeddingDim != next.EmbeddingDim,
		"vision":    existing.VisionProvider != next.VisionProvider || existing.VisionBaseURL != next.VisionBaseURL || existing.VisionAPIKeyCiphertext != next.VisionAPIKeyCiphertext || existing.VisionModel != next.VisionModel,
	}
	records := map[string]model.AIProbeRecord{}
	_ = json.Unmarshal([]byte(existing.ProbeResultsJSON), &records)
	for purpose, modified := range changed {
		if modified {
			delete(records, purpose)
		}
	}
	if len(records) == 0 {
		existing.ProbeResultsJSON = ""
		return
	}
	data, _ := json.Marshal(records)
	existing.ProbeResultsJSON = string(data)
}
