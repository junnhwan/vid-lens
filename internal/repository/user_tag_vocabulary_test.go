package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"gorm.io/gorm"
	"vid-lens/internal/model"
	"vid-lens/internal/processing"
)

func TestUserTagFrozenVocabularyAndRemovedAutoRestore(t *testing.T) {
	runFrozenTagVocabulary(t, summaryRevisionDB(t))
}
func TestPostgresUserTagFrozenVocabularyAndRemovedAutoRestore(t *testing.T) {
	runFrozenTagVocabulary(t, openPostgresRepositoryTestDB(t).db)
}

func runFrozenTagVocabulary(t *testing.T, db *gorm.DB) {
	ctx := context.Background()
	repo := NewUserTagRepository(db)
	existing := mustCreateTag(t, repo, 17, "PostgreSQL")
	existing, err := repo.SetAliases(ctx, 17, existing.ID, []string{"Postgres"}, existing.Version)
	if err != nil {
		t.Fatal(err)
	}
	foreign := mustCreateTag(t, repo, 18, "Foreign owner only")
	for i := 0; i < processing.MaxTagVocabularyCandidates+5; i++ {
		mustCreateTag(t, repo, 17, fmt.Sprintf("a-%03d", i))
	}
	frozen, err := repo.FreezeVocabulary(ctx, 17, "配置 ＰＯＳＴＧＲＥＳ 连接池")
	if err != nil || frozen.Validate() != nil || len(frozen.Candidates) != processing.MaxTagVocabularyCandidates || !frozen.Truncated || frozen.Candidates[0].TagID != existing.ID || frozen.Candidates[0].Aliases[0] != "Postgres" {
		t.Fatalf("shortlist=%+v err=%v", frozen, err)
	}
	for _, entry := range frozen.Candidates {
		if entry.TagID == foreign.ID {
			t.Fatal("foreign vocabulary leaked")
		}
	}
	again, err := repo.FreezeVocabulary(ctx, 17, "配置 ＰＯＳＴＧＲＥＳ 连接池")
	if err != nil || !reflect.DeepEqual(frozen, again) {
		t.Fatal("wordbook ordering is nondeterministic", err)
	}
	original, _ := json.Marshal(frozen)
	if _, err = repo.Rename(ctx, 17, existing.ID, "PG renamed", existing.Version); err != nil {
		t.Fatal(err)
	}
	current, err := repo.FreezeVocabulary(ctx, 17, "Postgres")
	if err != nil || current.Version <= frozen.Version || current.Candidates[0].Name != "PG renamed" {
		t.Fatal("fresh acceptance ignored vocabulary edit", err)
	}
	raw, _ := json.Marshal(frozen)
	if !reflect.DeepEqual(raw, original) {
		t.Fatal("accepted vocabulary mutated")
	}
	if len(raw) > processing.MaxTagVocabularyBytes {
		t.Fatal("wordbook prompt exceeded byte bound")
	}
	large := mustCreateTag(t, repo, 19, "Bounded aliases")
	aliases := []string{}
	for i := 0; i < 20; i++ {
		aliases = append(aliases, fmt.Sprintf("%02d-%s", i, strings.Repeat("字", 75)))
	}
	if _, err = repo.SetAliases(ctx, 19, large.ID, aliases, large.Version); err != nil {
		t.Fatal(err)
	}
	small, err := repo.FreezeVocabulary(ctx, 19, "Bounded aliases", 512)
	raw, _ = json.Marshal(small)
	if err != nil || small.Validate() != nil || len(raw) > 512 || !small.Truncated || len(small.Candidates) != 1 || small.Candidates[0].TagID != large.ID {
		t.Fatalf("budget did not preserve bounded ID: %+v, %v", small, err)
	}

	_, req := tagTaskFixture(t, db, 204, 17)
	req.Candidates = []TagCandidate{{TagID: existing.ID, Reason: "数据库主题"}}
	state, err := repo.PublishCandidates(ctx, req)
	if err != nil || len(state.Assignments) != 1 {
		t.Fatal("existing ID was not reused", err)
	}
	suggestion := state.Suggestions[0]
	var before model.VideoTagSuggestion
	if err = db.First(&before, "id = ?", suggestion.ID).Error; err != nil {
		t.Fatal(err)
	}
	removed, err := repo.PatchTask(ctx, 17, req.TaskID, TagPatch{ExpectedVersion: state.Version, RemoveIDs: []string{existing.ID}})
	if err != nil || len(removed.Assignments) != 0 || removed.Suggestions[0].Status != "accepted" || removed.Suggestions[0].EffectiveStatus != "rejected" {
		t.Fatalf("remove lost restore authority: %+v err=%v", removed, err)
	}
	var after model.VideoTagSuggestion
	if err = db.First(&after, "id = ?", suggestion.ID).Error; err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatal("remove rewrote generation candidate history")
	}
	read, err := repo.TaskState(ctx, 17, req.TaskID)
	if err != nil || read.Suggestions[0].EffectiveStatus != "rejected" {
		t.Fatal("reject projection was not durable", err)
	}
	if _, err = repo.DecideSuggestion(ctx, 18, req.TaskID, suggestion.ID, TagSuggestionDecisionInput{ExpectedVersion: removed.Version, Decision: "restore"}); err == nil {
		t.Fatal("foreign owner restored tag")
	}
	restored, err := repo.DecideSuggestion(ctx, 17, req.TaskID, suggestion.ID, TagSuggestionDecisionInput{ExpectedVersion: removed.Version, Decision: "restore"})
	if err != nil || len(restored.Assignments) != 0 || restored.Suggestions[0].EffectiveStatus != "pending" {
		t.Fatalf("restore attached or hid candidate: %+v err=%v", restored, err)
	}
	accepted, err := repo.DecideSuggestion(ctx, 17, req.TaskID, suggestion.ID, TagSuggestionDecisionInput{ExpectedVersion: restored.Version, Decision: "accept"})
	if err != nil || len(accepted.Assignments) != 1 || accepted.Assignments[0].Origin != "manual" {
		t.Fatal("explicit accept did not protect selection", err)
	}
	if _, err = repo.PatchTask(ctx, 17, req.TaskID, TagPatch{ExpectedVersion: removed.Version, RemoveIDs: []string{existing.ID}}); err == nil {
		t.Fatal("stale remove bypassed tag CAS")
	}
}
