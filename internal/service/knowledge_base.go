package service

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"
	"vid-lens/internal/model"
	"vid-lens/internal/repository"
)

const (
	KnowledgeBaseMaxVideos           = 50
	knowledgeBaseMaxNameRunes        = 100
	knowledgeBaseMaxDescriptionRunes = 500
)

var (
	ErrKnowledgeBaseNotFound               = errors.New("知识库不存在")
	ErrKnowledgeBaseNameRequired           = errors.New("知识库名称不能为空")
	ErrKnowledgeBaseNameTooLong            = errors.New("知识库名称不能超过 100 个字符")
	ErrKnowledgeBaseDescriptionTooLong     = errors.New("知识库描述不能超过 500 个字符")
	ErrKnowledgeBaseDefaultProfileRequired = ErrAIProfileRequired
	ErrKnowledgeBaseTaskNotFound           = ErrTaskNotFound
	ErrKnowledgeBaseTaskNotIndexed         = errors.New("视频尚未完成当前 embedding 模型的 RAG 索引")
	ErrKnowledgeBaseVideoLimit             = errors.New("单个知识库最多添加 50 个视频")
	ErrKnowledgeBaseVideoNotFound          = errors.New("知识库中不存在该视频")
)

type CreateKnowledgeBaseRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type UpdateKnowledgeBaseRequest struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
}

type KnowledgeBaseResponse struct {
	ID             int64                        `json:"id"`
	Name           string                       `json:"name"`
	Description    string                       `json:"description"`
	MemberCount    int64                        `json:"member_count"`
	EmbeddingModel string                       `json:"embedding_model,omitempty"`
	Videos         []KnowledgeBaseVideoResponse `json:"videos,omitempty"`
	CreatedAt      time.Time                    `json:"created_at"`
	UpdatedAt      time.Time                    `json:"updated_at"`
}

type KnowledgeBaseVideoResponse struct {
	TaskID      int64  `json:"task_id"`
	Title       string `json:"title"`
	Status      int8   `json:"status"`
	IndexStatus string `json:"index_status"`
	Retrievable bool   `json:"retrievable"`
}

type KnowledgeBaseService struct {
	repos *repository.Repositories
}

func NewKnowledgeBaseService(repos *repository.Repositories) *KnowledgeBaseService {
	return &KnowledgeBaseService{repos: repos}
}

func (s *KnowledgeBaseService) Create(_ context.Context, userID int64, req CreateKnowledgeBaseRequest) (*KnowledgeBaseResponse, error) {
	name, description, err := validateKnowledgeBaseText(req.Name, req.Description)
	if err != nil {
		return nil, err
	}
	kb := &model.KnowledgeBase{UserID: userID, Name: name, Description: description}
	if err := s.repos.KnowledgeBase.Create(kb); err != nil {
		return nil, err
	}
	return knowledgeBaseResponse(kb, 0, "", nil), nil
}

func (s *KnowledgeBaseService) List(_ context.Context, userID int64) ([]KnowledgeBaseResponse, error) {
	knowledgeBases, err := s.repos.KnowledgeBase.ListByUserID(userID)
	if err != nil {
		return nil, err
	}
	result := make([]KnowledgeBaseResponse, 0, len(knowledgeBases))
	for i := range knowledgeBases {
		count, err := s.repos.KnowledgeBase.CountMembers(userID, knowledgeBases[i].ID)
		if err != nil {
			return nil, err
		}
		result = append(result, *knowledgeBaseResponse(&knowledgeBases[i], count, "", nil))
	}
	return result, nil
}

func (s *KnowledgeBaseService) Get(ctx context.Context, userID, knowledgeBaseID int64) (*KnowledgeBaseResponse, error) {
	kb, err := s.repos.KnowledgeBase.FindByIDForUser(userID, knowledgeBaseID)
	if err != nil {
		return nil, err
	}
	if kb == nil {
		return nil, ErrKnowledgeBaseNotFound
	}

	profile, err := s.repos.AIProfile.FindDefaultByUserID(userID)
	if err != nil {
		return nil, err
	}
	embeddingModel := ""
	if profile != nil {
		embeddingModel = profile.EmbeddingModel
	}
	memberTaskIDs, err := s.repos.KnowledgeBase.ListMemberTaskIDsForUser(userID, knowledgeBaseID)
	if err != nil {
		return nil, err
	}
	videos := make([]KnowledgeBaseVideoResponse, 0, len(memberTaskIDs))
	for _, taskID := range memberTaskIDs {
		task, err := s.repos.Task.FindByID(taskID)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				continue
			}
			return nil, err
		}
		if task.UserID != userID {
			continue
		}
		indexStatus := model.RAGIndexStatusNotIndexed
		if embeddingModel != "" {
			index, err := s.repos.RAGIndex.FindByTaskAndModel(userID, task.ID, embeddingModel)
			if err != nil {
				return nil, err
			}
			if index != nil {
				indexStatus = index.Status
			}
		}
		title := strings.TrimSpace(task.Title)
		if title == "" {
			title = task.Filename
		}
		videos = append(videos, KnowledgeBaseVideoResponse{
			TaskID: task.ID, Title: title, Status: task.Status,
			IndexStatus: indexStatus, Retrievable: indexStatus == model.RAGIndexStatusIndexed,
		})
	}
	count := int64(len(videos))
	return knowledgeBaseResponse(kb, count, embeddingModel, videos), nil
}

func (s *KnowledgeBaseService) Update(_ context.Context, userID, knowledgeBaseID int64, req UpdateKnowledgeBaseRequest) (*KnowledgeBaseResponse, error) {
	kb, err := s.repos.KnowledgeBase.FindByIDForUser(userID, knowledgeBaseID)
	if err != nil {
		return nil, err
	}
	if kb == nil {
		return nil, ErrKnowledgeBaseNotFound
	}
	name := kb.Name
	description := kb.Description
	if req.Name != nil {
		name = strings.TrimSpace(*req.Name)
	}
	if req.Description != nil {
		description = *req.Description
	}
	if err := validateKnowledgeBaseName(name); err != nil {
		return nil, err
	}
	if err := validateKnowledgeBaseDescription(description); err != nil {
		return nil, err
	}
	kb.Name = name
	kb.Description = description
	if err := s.repos.KnowledgeBase.UpdateForUser(userID, kb); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrKnowledgeBaseNotFound
		}
		return nil, err
	}
	memberCount, err := s.repos.KnowledgeBase.CountMembers(userID, knowledgeBaseID)
	if err != nil {
		return nil, err
	}
	return knowledgeBaseResponse(kb, memberCount, "", nil), nil
}

func (s *KnowledgeBaseService) Delete(_ context.Context, userID, knowledgeBaseID int64) error {
	if err := s.repos.KnowledgeBase.DeleteForUser(userID, knowledgeBaseID); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrKnowledgeBaseNotFound
		}
		return err
	}
	return nil
}

func (s *KnowledgeBaseService) AddVideo(ctx context.Context, userID, knowledgeBaseID, taskID int64) error {
	kb, err := s.repos.KnowledgeBase.FindByIDForUser(userID, knowledgeBaseID)
	if err != nil {
		return err
	}
	if kb == nil {
		return ErrKnowledgeBaseNotFound
	}
	profile, err := s.repos.AIProfile.FindDefaultByUserID(userID)
	if err != nil {
		return err
	}
	if profile == nil || strings.TrimSpace(profile.EmbeddingModel) == "" {
		return ErrKnowledgeBaseDefaultProfileRequired
	}
	task, err := s.repos.Task.FindByID(taskID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrKnowledgeBaseTaskNotFound
		}
		return err
	}
	if task.UserID != userID || task.DeletedAt.Valid {
		return ErrKnowledgeBaseTaskNotFound
	}
	index, err := s.repos.RAGIndex.FindByTaskAndModel(userID, taskID, profile.EmbeddingModel)
	if err != nil {
		return err
	}
	if index == nil || index.Status != model.RAGIndexStatusIndexed {
		return ErrKnowledgeBaseTaskNotIndexed
	}

	return s.repos.TransactionContext(ctx, func(txRepos *repository.Repositories) error {
		// Task cleanup locks task first and then related knowledge bases. Keep
		// the same order here so a stale precheck cannot add a deleted task.
		lockedTask, err := txRepos.Task.FindByIDForUpdate(taskID)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrKnowledgeBaseTaskNotFound
		}
		if err != nil {
			return err
		}
		if lockedTask.UserID != userID || lockedTask.DeletedAt.Valid {
			return ErrKnowledgeBaseTaskNotFound
		}

		lockedKB, count, err := txRepos.KnowledgeBase.LockForUpdateAndCountMembers(userID, knowledgeBaseID)
		if err != nil {
			return err
		}
		if lockedKB == nil {
			return ErrKnowledgeBaseNotFound
		}
		memberIDs, err := txRepos.KnowledgeBase.ListMemberTaskIDsForUser(userID, knowledgeBaseID)
		if err != nil {
			return err
		}
		for _, memberID := range memberIDs {
			if memberID == taskID {
				return nil
			}
		}
		if count >= KnowledgeBaseMaxVideos {
			return ErrKnowledgeBaseVideoLimit
		}
		_, err = txRepos.KnowledgeBase.AddVideoForUser(userID, knowledgeBaseID, taskID)
		return err
	})
}

func (s *KnowledgeBaseService) RemoveVideo(ctx context.Context, userID, knowledgeBaseID, taskID int64) error {
	err := s.repos.TransactionContext(ctx, func(txRepos *repository.Repositories) error {
		kb, err := txRepos.KnowledgeBase.FindByIDForUserForUpdate(userID, knowledgeBaseID)
		if err != nil {
			return err
		}
		if kb == nil {
			return ErrKnowledgeBaseNotFound
		}
		return txRepos.KnowledgeBase.RemoveVideoForUser(userID, knowledgeBaseID, taskID)
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrKnowledgeBaseVideoNotFound
	}
	return err
}

func validateKnowledgeBaseText(name, description string) (string, string, error) {
	name = strings.TrimSpace(name)
	if err := validateKnowledgeBaseName(name); err != nil {
		return "", "", err
	}
	if err := validateKnowledgeBaseDescription(description); err != nil {
		return "", "", err
	}
	return name, description, nil
}

func validateKnowledgeBaseName(name string) error {
	if name == "" {
		return ErrKnowledgeBaseNameRequired
	}
	if utf8.RuneCountInString(name) > knowledgeBaseMaxNameRunes {
		return ErrKnowledgeBaseNameTooLong
	}
	return nil
}

func validateKnowledgeBaseDescription(description string) error {
	if utf8.RuneCountInString(description) > knowledgeBaseMaxDescriptionRunes {
		return ErrKnowledgeBaseDescriptionTooLong
	}
	return nil
}

func knowledgeBaseResponse(kb *model.KnowledgeBase, memberCount int64, embeddingModel string, videos []KnowledgeBaseVideoResponse) *KnowledgeBaseResponse {
	response := &KnowledgeBaseResponse{
		ID: kb.ID, Name: kb.Name, Description: kb.Description, MemberCount: memberCount,
		EmbeddingModel: embeddingModel, CreatedAt: kb.CreatedAt,
		UpdatedAt: kb.UpdatedAt,
	}
	if videos != nil {
		response.Videos = videos
	}
	return response
}
