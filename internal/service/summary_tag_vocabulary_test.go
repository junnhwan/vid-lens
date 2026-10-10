package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"vid-lens/internal/artifact"
	"vid-lens/internal/model"
	"vid-lens/internal/processing"
	"vid-lens/internal/repository"
)

func TestImportFreezesOwnerTagVocabularyBeforeModelExecution(t *testing.T) {
	svc, _, _ := importFixture(t)
	ctx := context.Background()
	tag, err := svc.repo.UserTag.Create(ctx, 7, "PostgreSQL")
	if err != nil {
		t.Fatal(err)
	}
	tag, err = svc.repo.UserTag.SetAliases(ctx, 7, tag.ID, []string{"Postgres"}, tag.Version)
	if err != nil {
		t.Fatal(err)
	}
	foreign, err := svc.repo.UserTag.Create(ctx, 8, "Owner eight only")
	if err != nil {
		t.Fatal(err)
	}
	options, _ := processing.Normalize(importOpts("tag-vocabulary").Options, false)
	request := &preparedImport{options: options}
	if err = svc.freezeImport(7, request); err != nil {
		t.Fatal(err)
	}
	frozen := request.intent.TagVocabulary
	if frozen == nil || len(frozen.Candidates) != 1 || frozen.Candidates[0].TagID != tag.ID || frozen.Candidates[0].Aliases[0] != "Postgres" || strings.Contains(artifact.JSON(frozen), foreign.ID) {
		t.Fatalf("owner vocabulary not frozen: %+v", frozen)
	}
	if request.intent.ExpectedTagVersion == nil || *request.intent.ExpectedTagVersion != 0 {
		t.Fatal("new-task acceptance did not freeze tag version")
	}
	if _, err = svc.repo.UserTag.Rename(ctx, 7, tag.ID, "Changed after acceptance", tag.Version); err != nil {
		t.Fatal(err)
	}
	if frozen.Candidates[0].Name != "PostgreSQL" {
		t.Fatal("acceptance snapshot changed")
	}
}

func TestSummaryGenerationAcceptedTagVersionPreservesLaterManualSelection(t *testing.T) {
	f := newGenerationFixture(t, false)
	ctx := context.Background()
	tag, err := f.repos.UserTag.Create(ctx, 7, "Manual choice")
	if err != nil {
		t.Fatal(err)
	}
	var frozen processing.GenerationSnapshot
	if err = json.Unmarshal([]byte(f.job.InputSnapshotJSON), &frozen); err != nil {
		t.Fatal(err)
	}
	expected := int64(0)
	frozen.Intent.ExpectedTagVersion = &expected
	frozen.Intent.TagVocabulary, err = f.repos.UserTag.FreezeVocabulary(ctx, 7, "")
	if err != nil {
		t.Fatal(err)
	}
	f.job.InputSnapshotJSON = artifact.JSON(frozen)
	if err = f.db.Model(f.job).Update("input_snapshot_json", f.job.InputSnapshotJSON).Error; err != nil {
		t.Fatal(err)
	}
	if _, err = f.repos.UserTag.PatchTask(ctx, 7, f.task.ID, repository.TagPatch{ExpectedVersion: 0, AddIDs: []string{tag.ID}}); err != nil {
		t.Fatal(err)
	}
	f.chat.candidates = []repository.TagCandidate{{Name: "Automatic candidate", Reason: "来源内容"}}
	if err = f.svc.Generate(ctx, f.task, f.job, f.job.ProcessingToken); err != nil {
		t.Fatal(err)
	}
	state, err := f.repos.UserTag.TaskState(ctx, 7, f.task.ID)
	if err != nil || len(state.Assignments) != 1 || state.Assignments[0].TagID != tag.ID || state.Assignments[0].Origin != "manual" || state.Classification == nil || state.Classification.ErrorCode != "version_conflict" {
		t.Fatalf("accepted CAS overwrote manual selection: %+v err=%v", state, err)
	}
}

func TestSummaryGenerationUsesFrozenVocabularyAndRejectsInventedTagID(t *testing.T) {
	for _, invalid := range []bool{false, true} {
		t.Run(map[bool]string{false: "reuse", true: "invented-id"}[invalid], func(t *testing.T) {
			f := newGenerationFixture(t, false)
			ctx := context.Background()
			tag, err := f.repos.UserTag.Create(ctx, 7, "PostgreSQL")
			if err != nil {
				t.Fatal(err)
			}
			tag, err = f.repos.UserTag.SetAliases(ctx, 7, tag.ID, []string{"Postgres"}, tag.Version)
			if err != nil {
				t.Fatal(err)
			}
			vocabulary, err := f.repos.UserTag.FreezeVocabulary(ctx, 7, "Postgres")
			if err != nil {
				t.Fatal(err)
			}
			var frozen processing.GenerationSnapshot
			if err = json.Unmarshal([]byte(f.job.InputSnapshotJSON), &frozen); err != nil {
				t.Fatal(err)
			}
			frozen.Intent.TagVocabulary = vocabulary
			f.job.InputSnapshotJSON = artifact.JSON(frozen)
			if err = f.db.Model(f.job).Update("input_snapshot_json", f.job.InputSnapshotJSON).Error; err != nil {
				t.Fatal(err)
			}
			if _, err = f.repos.UserTag.Rename(ctx, 7, tag.ID, "Changed after acceptance", tag.Version); err != nil {
				t.Fatal(err)
			}
			candidateID := tag.ID
			if invalid {
				candidateID = "not-in-frozen-owner-vocabulary"
			}
			f.chat.candidates = []repository.TagCandidate{{TagID: candidateID, Reason: "来源数据库主题"}}
			if err = f.svc.Generate(ctx, f.task, f.job, f.job.ProcessingToken); err != nil {
				t.Fatal(err)
			}
			if len(f.chat.calls) != 1 || !strings.Contains(f.chat.calls[0], `"name":"PostgreSQL"`) || !strings.Contains(f.chat.calls[0], `"aliases":["Postgres"]`) || strings.Contains(f.chat.calls[0], "Changed after acceptance") {
				t.Fatal("prompt reread later vocabulary instead of frozen snapshot")
			}
			state, err := f.repos.UserTag.TaskState(ctx, 7, f.task.ID)
			if err != nil {
				t.Fatal(err)
			}
			if invalid {
				if len(state.Assignments) != 0 || state.Classification == nil || state.Classification.ErrorCode != "invalid_tag_candidates" {
					t.Fatalf("invented ID allowed: %+v", state)
				}
			} else if len(state.Assignments) != 1 || state.Assignments[0].TagID != tag.ID {
				t.Fatal("existing canonical ID not reused")
			}
			var summary model.AISummary
			if err = f.db.First(&summary, "task_id = ?", f.task.ID).Error; err != nil || summary.GeneratedVersion != 1 {
				t.Fatal("classification invalidated good summary", err)
			}
		})
	}
}
