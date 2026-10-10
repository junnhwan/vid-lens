package service

import (
	"context"
	"vid-lens/internal/repository"
)

// UserTagService keeps explicit user decisions separate from frozen automated
// recipe candidates. No operation grants access to another user's vocabulary.
type UserTagService struct{ repo *repository.UserTagRepository }

func NewUserTagService(repo *repository.UserTagRepository) *UserTagService {
	return &UserTagService{repo}
}
func (s *UserTagService) List(ctx context.Context, owner int64, page, size int, search string, sortBy ...string) ([]repository.UserTagView, int64, error) {
	return s.repo.List(ctx, owner, page, size, search, sortBy...)
}
func (s *UserTagService) Create(ctx context.Context, owner int64, name string) (repository.UserTagView, error) {
	return s.repo.Create(ctx, owner, name)
}
func (s *UserTagService) Rename(ctx context.Context, owner int64, id, name string, version int64) (repository.UserTagView, error) {
	return s.repo.Rename(ctx, owner, id, name, version)
}
func (s *UserTagService) Aliases(ctx context.Context, owner int64, id string, aliases []string, version int64) (repository.UserTagView, error) {
	return s.repo.SetAliases(ctx, owner, id, aliases, version)
}
func (s *UserTagService) Merge(ctx context.Context, owner int64, id, key string, input repository.TagMergeInput) (repository.UserTagView, error) {
	return s.repo.Merge(ctx, owner, id, key, input)
}
func (s *UserTagService) Task(ctx context.Context, owner, taskID int64) (repository.TaskTagState, error) {
	return s.repo.TaskState(ctx, owner, taskID)
}
func (s *UserTagService) Patch(ctx context.Context, owner, taskID int64, input repository.TagPatch) (repository.TaskTagState, error) {
	return s.repo.PatchTask(ctx, owner, taskID, input)
}
func (s *UserTagService) Decide(ctx context.Context, owner, taskID int64, id string, input repository.TagSuggestionDecisionInput) (repository.TaskTagState, error) {
	return s.repo.DecideSuggestion(ctx, owner, taskID, id, input)
}
func (s *UserTagService) PublishCandidates(ctx context.Context, input repository.PublishTagCandidatesRequest) (repository.TaskTagState, error) {
	return s.repo.PublishCandidates(ctx, input)
}
