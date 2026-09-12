package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"strings"
	"unicode/utf8"
	"vid-lens/internal/model"
)

var ErrInvalidFeedback = errors.New("invalid feedback")

type ChatFeedbackRepository struct{ db *gorm.DB }

func NewChatFeedbackRepository(db *gorm.DB) *ChatFeedbackRepository {
	return &ChatFeedbackRepository{db: db}
}
func ValidateFeedback(rating, category, note string) error {
	if utf8.RuneCountInString(note) > 2000 {
		return ErrInvalidFeedback
	}
	if rating == "helpful" && category == "" {
		return nil
	}
	if rating == "problem" {
		switch category {
		case "content", "citation", "incomplete", "slow":
			return nil
		}
	}
	return ErrInvalidFeedback
}
func feedbackMessage(db *gorm.DB, userID, sessionID, messageID int64) (*model.ChatMessage, string, error) {
	var session model.ChatSession
	if e := db.Where("id=? AND user_id=?", sessionID, userID).First(&session).Error; e != nil {
		return nil, "", e
	}
	var msg model.ChatMessage
	if e := db.Where("id=? AND session_id=? AND user_id=? AND role=?", messageID, sessionID, userID, "assistant").First(&msg).Error; e != nil {
		return nil, "", e
	}
	var snapshot struct {
		RunID string `json:"run_id"`
	}
	if msg.RetrievalSnapshot != nil && strings.TrimSpace(*msg.RetrievalSnapshot) != "" {
		if e := json.Unmarshal([]byte(*msg.RetrievalSnapshot), &snapshot); e != nil {
			return nil, "", ErrInvalidFeedback
		}
	}
	if snapshot.RunID != "" {
		var run model.AgentRun
		if e := db.Select("id").Where("id=? AND user_id=? AND session_id=?", snapshot.RunID, userID, sessionID).First(&run).Error; e != nil {
			return nil, "", e
		}
	}
	return &msg, snapshot.RunID, nil
}
func (r *ChatFeedbackRepository) Put(ctx context.Context, userID, sessionID, messageID int64, rating, category, note string) (*model.ChatFeedback, error) {
	if e := ValidateFeedback(rating, category, note); e != nil {
		return nil, e
	}
	var result model.ChatFeedback
	e := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		_, runID, e := feedbackMessage(tx, userID, sessionID, messageID)
		if e != nil {
			return e
		}
		result = model.ChatFeedback{UserID: userID, SessionID: sessionID, MessageID: messageID, RunID: runID, Rating: rating, Category: category, Note: note}
		if e = tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "user_id"}, {Name: "message_id"}}, DoUpdates: clause.AssignmentColumns([]string{"rating", "category", "note", "run_id", "updated_at"})}).Create(&result).Error; e != nil {
			return e
		}
		return tx.Where("user_id=? AND message_id=?", userID, messageID).First(&result).Error
	})
	return &result, e
}
func (r *ChatFeedbackRepository) Get(ctx context.Context, userID, sessionID, messageID int64) (*model.ChatFeedback, error) {
	if _, _, e := feedbackMessage(r.db.WithContext(ctx), userID, sessionID, messageID); e != nil {
		return nil, e
	}
	var f model.ChatFeedback
	e := r.db.WithContext(ctx).Where("user_id=? AND session_id=? AND message_id=?", userID, sessionID, messageID).First(&f).Error
	if errors.Is(e, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &f, e
}
func (r *ChatFeedbackRepository) Delete(ctx context.Context, userID, sessionID, messageID int64) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, _, e := feedbackMessage(tx, userID, sessionID, messageID); e != nil {
			return e
		}
		return tx.Where("user_id=? AND session_id=? AND message_id=?", userID, sessionID, messageID).Delete(&model.ChatFeedback{}).Error
	})
}

// FeedbackCandidate keeps the observed answer separate from reviewed gold.
type FeedbackCandidate struct {
	CandidateID       string              `json:"candidate_id"`
	Feedback          *model.ChatFeedback `json:"feedback,omitempty"`
	Origin            string              `json:"origin"`
	UserID            int64               `json:"user_id"`
	SessionID         int64               `json:"session_id"`
	RunID             string              `json:"run_id,omitempty"`
	OriginalTurns     json.RawMessage     `json:"original_turns,omitempty"`
	ExecutionIdentity json.RawMessage     `json:"execution_identity,omitempty"`
	ExecutionResult   json.RawMessage     `json:"execution_result,omitempty"`
	Question          string              `json:"question"`
	ObservedAnswer    string              `json:"observed_answer"`
	SourceGroup       string              `json:"source_group"`
	AssetVersion      string              `json:"asset_version"`
	Kind              string              `json:"kind"`
	Status            string              `json:"status"`
	RunStatus         string              `json:"run_status,omitempty"`
	StopReason        string              `json:"stop_reason,omitempty"`
	Model             string              `json:"model,omitempty"`
	ProfileSnapshot   json.RawMessage     `json:"profile_snapshot,omitempty"`
	BudgetSnapshot    json.RawMessage     `json:"budget_snapshot,omitempty"`
	SourceSnapshot    json.RawMessage     `json:"source_snapshot,omitempty"`
	AnswerSnapshot    json.RawMessage     `json:"answer_snapshot,omitempty"`
}

func (r *ChatFeedbackRepository) Candidates(ctx context.Context, userID int64, page, size int) ([]FeedbackCandidate, int64, error) {
	if page < 1 || size < 1 || size > 100 {
		return nil, 0, ErrInvalidFeedback
	}
	db := r.db.WithContext(ctx)
	query := db.Model(&model.ChatFeedback{}).Where("user_id=? AND rating=?", userID, "problem")
	query = query.Where("EXISTS (SELECT 1 FROM chat_sessions s WHERE s.id=chat_feedback.session_id AND s.user_id=chat_feedback.user_id)").
		Where("EXISTS (SELECT 1 FROM chat_messages m WHERE m.id=chat_feedback.message_id AND m.session_id=chat_feedback.session_id AND m.user_id=chat_feedback.user_id AND m.role='assistant')")
	var total int64
	if e := query.Count(&total).Error; e != nil {
		return nil, 0, e
	}
	var feedback []model.ChatFeedback
	if e := query.Order("id ASC").Offset((page - 1) * size).Limit(size).Find(&feedback).Error; e != nil {
		return nil, 0, e
	}
	result := make([]FeedbackCandidate, 0, len(feedback))
	for _, f := range feedback {
		msg, runID, e := feedbackMessage(db, userID, f.SessionID, f.MessageID)
		if errors.Is(e, gorm.ErrRecordNotFound) {
			continue
		}
		if e != nil {
			return nil, 0, e
		}
		var session model.ChatSession
		if e = db.Where("id=? AND user_id=?", f.SessionID, userID).First(&session).Error; e != nil {
			return nil, 0, e
		}
		var question model.ChatMessage
		e = db.Where("session_id=? AND user_id=? AND role=? AND id<?", f.SessionID, userID, "user", f.MessageID).Order("id DESC").First(&question).Error
		if e != nil && !errors.Is(e, gorm.ErrRecordNotFound) {
			return nil, 0, e
		}
		c := FeedbackCandidate{CandidateID: fmt.Sprintf("feedback-%d", f.ID), Feedback: &f, Origin: "feedback", UserID: userID, SessionID: f.SessionID, RunID: runID, Question: question.Content, ObservedAnswer: msg.Content, Kind: "chat", Status: "candidate", Model: msg.ModelName, SourceGroup: fmt.Sprintf("video-%d", session.TaskID)}
		if session.KnowledgeBaseID > 0 {
			c.SourceGroup = fmt.Sprintf("knowledge-base-%d", session.KnowledgeBaseID)
		}
		var sources []model.ChatMessageSource
		if e = db.Where("message_id=?", msg.ID).Order("task_id ASC").Find(&sources).Error; e != nil {
			return nil, 0, e
		}
		c.SourceSnapshot, _ = json.Marshal(sources)
		if msg.RetrievalSnapshot != nil {
			c.AnswerSnapshot = json.RawMessage(*msg.RetrievalSnapshot)
		}
		taskIDs := []int64{}
		if session.TaskID > 0 {
			taskIDs = append(taskIDs, session.TaskID)
		}
		for _, source := range sources {
			taskIDs = append(taskIDs, source.TaskID)
		}
		// Hash current authoritative source assets, never client facts or generated
		// answers. Deleted/foreign assets cannot be read through old message refs.
		var transcripts []model.VideoTranscription
		if len(taskIDs) > 0 {
			if e = db.Where("task_id IN (?) AND task_id IN (SELECT id FROM video_tasks WHERE user_id=?)", taskIDs, userID).Order("task_id ASC").Find(&transcripts).Error; e != nil {
				return nil, 0, e
			}
		}
		assetBytes, _ := json.Marshal(transcripts)
		asset := sha256.Sum256(assetBytes)
		c.AssetVersion = hex.EncodeToString(asset[:])
		if runID != "" {
			var run model.AgentRun
			if e = db.Where("id=? AND user_id=? AND session_id=?", runID, userID, f.SessionID).First(&run).Error; e != nil {
				return nil, 0, e
			}
			c.Kind = "agent"
			c.Question = run.Goal
			c.StopReason = run.StopReason
			c.RunStatus = run.Status
			c.ProfileSnapshot = json.RawMessage(run.ProfileSnapshot)
			c.BudgetSnapshot = json.RawMessage(run.BudgetSnapshot)
		}
		result = append(result, c)
	}
	return result, total, nil
}
