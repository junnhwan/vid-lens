package repository

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"gorm.io/gorm"
	"vid-lens/internal/model"
)

func tagTaskFixture(t *testing.T, db *gorm.DB, id, owner int64) (model.VideoTask, PublishTagCandidatesRequest) {
	t.Helper()
	task := createSourceTask(t, db, id, owner)
	repos := NewRepositories(db)
	source, err := repos.PublishTextSource(context.Background(), PublishTextSourceRequest{UserID: owner, TaskID: id, Snapshot: sourceFixture(t, "连接池与 PostgreSQL 配置说明。", 3000)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	gen := fmt.Sprintf("tag-generation-%d", id)
	summary := model.AISummary{TaskID: id, FileMD5: task.FileMD5, Content: "配置说明。", SourceID: source.ID, SourceDigest: source.SourceDigest, GenerationID: gen, GeneratedVersion: 1}
	if err = db.Create(&summary).Error; err != nil {
		t.Fatal(err)
	}
	return task, PublishTagCandidatesRequest{UserID: owner, TaskID: id, SourceDigest: source.SourceDigest, GenerationID: gen, GeneratedVersion: 1, Enabled: true}
}
func mustCreateTag(t *testing.T, repo *UserTagRepository, owner int64, name string) UserTagView {
	t.Helper()
	tag, err := repo.Create(context.Background(), owner, name)
	if err != nil {
		t.Fatal(err)
	}
	return tag
}

func TestUserTagNamespaceNormalizationAndConflicts(t *testing.T) {
	runUserTagNamespace(t, summaryRevisionDB(t))
}
func TestPostgresUserTagNamespaceNormalizationAndConflicts(t *testing.T) {
	runUserTagNamespace(t, openPostgresRepositoryTestDB(t).db)
}
func runUserTagNamespace(t *testing.T, db *gorm.DB) {
	repo := NewUserTagRepository(db)
	ctx := context.Background()
	cpp := mustCreateTag(t, repo, 17, " Ｃ＋＋ ")
	again := mustCreateTag(t, repo, 17, "c++")
	if cpp.ID != again.ID || cpp.DisplayName != "C++" {
		t.Fatal("NFKC/casefold did not reuse canonical tag")
	}
	csharp := mustCreateTag(t, repo, 17, "C#")
	c := mustCreateTag(t, repo, 17, "C")
	dotnet := mustCreateTag(t, repo, 17, ".NET")
	if cpp.ID == c.ID || csharp.ID == c.ID || dotnet.ID == c.ID {
		t.Fatal("technical punctuation was removed")
	}
	cpp, err := repo.SetAliases(ctx, 17, cpp.ID, []string{"CPP", "c plus plus"}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if alias := mustCreateTag(t, repo, 17, "  ｃｐｐ "); alias.ID != cpp.ID {
		t.Fatal("alias did not reuse canonical ID")
	}
	if _, err = repo.SetAliases(ctx, 17, csharp.ID, []string{"cpp"}, 1); err == nil {
		t.Fatal("alias namespace collision accepted")
	}
	if _, err = repo.Rename(ctx, 17, csharp.ID, "CPP", 1); err == nil {
		t.Fatal("rename collided with confirmed alias")
	}
	cpp, err = repo.Rename(ctx, 17, cpp.ID, "C++ Language", 2)
	if err != nil {
		t.Fatal(err)
	}
	if old := mustCreateTag(t, repo, 17, "c++"); old.ID != cpp.ID {
		t.Fatal("rename discarded previous canonical alias")
	}
	if _, err = repo.Rename(ctx, 17, cpp.ID, "changed", 2); err == nil {
		t.Fatal("stale tag version accepted")
	}
	other := mustCreateTag(t, repo, 18, "cpp")
	if other.ID == cpp.ID {
		t.Fatal("tag namespace crossed users")
	}
	if _, err = repo.Get(ctx, 18, cpp.ID); err == nil {
		t.Fatal("other user could read tag")
	}
	rows, total, err := repo.List(ctx, 17, 1, 20, "CPP")
	if err != nil || total != 1 || len(rows) != 1 || rows[0].ID != cpp.ID {
		t.Fatalf("alias search=%+v total%d err%v", rows, total, err)
	}
}

func TestUserTagCandidateFencesManualDecisionsAndRestore(t *testing.T) {
	runUserTagCandidates(t, summaryRevisionDB(t))
}
func TestPostgresUserTagCandidateFencesManualDecisionsAndRestore(t *testing.T) {
	runUserTagCandidates(t, openPostgresRepositoryTestDB(t).db)
}
func runUserTagCandidates(t *testing.T, db *gorm.DB) {
	_, req := tagTaskFixture(t, db, 201, 17)
	repo := NewUserTagRepository(db)
	ctx := context.Background()
	existing := mustCreateTag(t, repo, 17, "PostgreSQL")
	existing, err := repo.SetAliases(ctx, 17, existing.ID, []string{"Postgres"}, existing.Version)
	if err != nil {
		t.Fatal(err)
	}
	req.Candidates = []TagCandidate{{Name: "Postgres", Reason: "视频讲解数据库配置"}, {Name: "连接池", Reason: "完整来源中的配置主题"}, {Name: "待确认概念", Reason: "证据不足", Uncertain: true}, {Name: "已学会", Reason: "不能推断个人状态"}}
	disabled := req
	disabled.Enabled = false
	state, err := repo.PublishCandidates(ctx, disabled)
	if err != nil || state.Version != 0 || len(state.Assignments) != 0 {
		t.Fatal("disabled auto tags changed task")
	}
	state, err = repo.PublishCandidates(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if state.Version != 1 || len(state.Assignments) != 2 || len(state.Suggestions) != 4 {
		t.Fatalf("auto state=%+v", state)
	}
	var poolID, pendingID string
	for _, a := range state.Assignments {
		if a.TagID != existing.ID {
			poolID = a.TagID
			if a.Tag.CreationOrigin != "agent" || a.Tag.ProtectedByUser {
				t.Fatal("automatic creation incorrectly protected")
			}
		}
	}
	for _, s := range state.Suggestions {
		if s.DisplayName == "待确认概念" {
			pendingID = s.ID
		}
	}
	state, err = repo.PatchTask(ctx, 17, 201, TagPatch{ExpectedVersion: 1, RemoveIDs: []string{existing.ID}, KeepAutoIDs: []string{poolID}})
	if err != nil {
		t.Fatal(err)
	}
	if state.Version != 2 || len(state.Assignments) != 1 || state.Assignments[0].Origin != "manual" || !state.Assignments[0].Tag.ProtectedByUser {
		t.Fatal("manual retain did not protect relation")
	}
	// A replay after user decisions returns their current set and never replaces it.
	state, err = repo.PublishCandidates(ctx, req)
	if err != nil || state.Version != 2 || len(state.Assignments) != 1 {
		t.Fatal("candidate retry overwrote user decisions")
	}
	if err = db.Model(&model.AISummary{}).Where("task_id = ?", 201).Update("generated_version", 2).Error; err != nil {
		t.Fatal(err)
	}
	req.GeneratedVersion = 2
	req.ExpectedTagVersion = 2
	state, err = repo.PublishCandidates(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Assignments) != 1 || state.Assignments[0].Origin != "manual" {
		t.Fatal("background regenerated rejected tag or deleted manual tag")
	}
	oldReq := req
	oldReq.GeneratedVersion = 1
	if _, err = repo.PublishCandidates(ctx, oldReq); err == nil {
		t.Fatal("old generation overwrote tags")
	}
	if _, err = repo.DecideSuggestion(ctx, 17, 201, pendingID, TagSuggestionDecisionInput{Decision: "accept", ExpectedVersion: state.Version}); err == nil {
		t.Fatal("accepted stale suggestion")
	}
	var sid string
	for _, s := range state.Suggestions {
		if s.DisplayName == "PostgreSQL" && s.GeneratedVersion == 2 {
			sid = s.ID
		}
	}
	state, err = repo.DecideSuggestion(ctx, 17, 201, sid, TagSuggestionDecisionInput{Decision: "restore", ExpectedVersion: 3})
	if err != nil {
		t.Fatal(err)
	}
	if state.Version != 4 || len(state.Assignments) != 1 {
		t.Fatal("restore immediately associated tag")
	}
	state, err = repo.DecideSuggestion(ctx, 17, 201, sid, TagSuggestionDecisionInput{Decision: "accept", ExpectedVersion: 4})
	if err != nil {
		t.Fatal(err)
	}
	if state.Version != 5 || len(state.Assignments) != 2 {
		t.Fatal("manual accept did not revoke rejection")
	}
	replay, err := repo.DecideSuggestion(ctx, 17, 201, sid, TagSuggestionDecisionInput{Decision: "accept", ExpectedVersion: 4})
	if err != nil || replay.Version != 5 {
		t.Fatal("decision replay not idempotent")
	}
	if _, err = repo.PatchTask(ctx, 17, 201, TagPatch{ExpectedVersion: 5, AddIDs: []string{existing.ID}, RemoveIDs: []string{existing.ID}}); err == nil {
		t.Fatal("contradictory canonical operations accepted")
	}
	if _, err = repo.PatchTask(ctx, 17, 201, TagPatch{ExpectedVersion: 4, RemoveIDs: []string{poolID}}); err == nil {
		t.Fatal("stale manual CAS accepted")
	}
	if _, err = repo.TaskState(ctx, 18, 201); err == nil {
		t.Fatal("tag task crossed user boundary")
	}
}

func TestUserTagMergeAndFullDatabaseFilters(t *testing.T) {
	runUserTagMergeFilter(t, summaryRevisionDB(t))
}
func TestPostgresUserTagMergeAndFullDatabaseFilters(t *testing.T) {
	runUserTagMergeFilter(t, openPostgresRepositoryTestDB(t).db)
}
func runUserTagMergeFilter(t *testing.T, db *gorm.DB) {
	task, _ := tagTaskFixture(t, db, 201, 17)
	other, _ := tagTaskFixture(t, db, 202, 17)
	third := model.VideoTask{ID: 203, UserID: 17, FileMD5: task.FileMD5, Filename: "unrelated.mp4", Title: "无关内容", Status: model.TaskStatusPending}
	if err := db.Create(&third).Error; err != nil {
		t.Fatal(err)
	}
	repo := NewUserTagRepository(db)
	ctx := context.Background()
	a := mustCreateTag(t, repo, 17, "Postgres")
	b := mustCreateTag(t, repo, 17, "PostgreSQL")
	domain := mustCreateTag(t, repo, 17, "数据库")
	if _, err := repo.PatchTask(ctx, 17, 201, TagPatch{AddIDs: []string{a.ID, domain.ID}}); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.PatchTask(ctx, 17, 202, TagPatch{AddIDs: []string{b.ID}}); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.PatchTask(ctx, 17, 202, TagPatch{ExpectedVersion: 1, RemoveIDs: []string{a.ID}}); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.VideoTagAssignment{UserID: 17, TaskID: 201, TagID: b.ID, Origin: "auto"}).Error; err != nil {
		t.Fatal(err)
	}
	input := TagMergeInput{TargetID: b.ID, SourceVersion: a.Version, TargetVersion: b.Version}
	merged, err := repo.Merge(ctx, 17, a.ID, "merge-postgres", input)
	if err != nil {
		t.Fatal(err)
	}
	if merged.ID != b.ID || !merged.ProtectedByUser {
		t.Fatal("manual merge failed")
	}
	again, err := repo.Merge(ctx, 17, a.ID, "merge-postgres", input)
	if err != nil || again.Version != merged.Version {
		t.Fatal("merge replay failed")
	}
	if _, err = repo.Merge(ctx, 17, b.ID, "merge-postgres", TagMergeInput{TargetID: domain.ID, SourceVersion: b.Version, TargetVersion: domain.Version}); err == nil {
		t.Fatal("merge key borrowed by different request")
	}
	state, err := repo.TaskState(ctx, 17, 201)
	if err != nil {
		t.Fatal(err)
	}
	for _, assignment := range state.Assignments {
		if assignment.TagID == a.ID || assignment.Origin != "manual" {
			t.Fatal("merge lost manual priority or left source relation")
		}
	}
	state, err = repo.TaskState(ctx, 17, 202)
	if err != nil {
		t.Fatal(err)
	}
	rejected := false
	for _, decision := range state.Decisions {
		if decision.TagID == b.ID && decision.Decision == "rejected" {
			rejected = true
		}
	}
	if !rejected {
		t.Fatal("merge erased rejection")
	}
	tasks, total, err := NewTaskRepository(db).ListByUserIDFiltered(17, 1, 1, "", TagFilter{IDs: []string{a.ID, b.ID}, Match: "all"})
	if err != nil || total != 2 || len(tasks) != 1 {
		t.Fatalf("merged all pagination: %d %+v %v", total, tasks, err)
	}
	tasks, total, err = NewTaskRepository(db).ListByUserIDFiltered(17, 1, 20, "", TagFilter{IDs: []string{b.ID, domain.ID}, Match: "all"})
	if err != nil || total != 1 || tasks[0].ID != 201 {
		t.Fatal("AND filter wrong")
	}
	_, total, err = NewTaskRepository(db).ListByUserIDFiltered(17, 1, 20, "", TagFilter{IDs: []string{b.ID, domain.ID}, Match: "any"})
	if err != nil || total != 2 {
		t.Fatal("OR filter duplicate count")
	}
	_, total, err = NewTaskRepository(db).ListByUserIDFiltered(17, 1, 20, "unrelated", TagFilter{IDs: []string{b.ID}, Match: "all"})
	if err != nil || total != 0 {
		t.Fatal("empty intersection fell back to full library")
	}
	foreign := mustCreateTag(t, repo, 18, "foreign")
	if _, _, err = NewTaskRepository(db).ListByUserIDFiltered(17, 1, 20, "", TagFilter{IDs: []string{foreign.ID}, Match: "any"}); err == nil {
		t.Fatal("other user filter disclosed tag scope")
	}
	if _, _, err = NewTaskRepository(db).ListByUserIDFiltered(17, 1, 20, "", TagFilter{Match: "unknown"}); err == nil {
		t.Fatal("invalid mode accepted")
	}
	if err = db.Delete(&other).Error; err != nil {
		t.Fatal(err)
	}
	view, err := repo.Get(ctx, 17, b.ID)
	if err != nil || view.VideoCount != 1 {
		t.Fatal("counts included deleted task")
	}
	_, total, err = NewTaskRepository(db).ListByUserIDFiltered(17, 1, 20, "", TagFilter{IDs: []string{b.ID}, Match: "all"})
	if err != nil || total != 1 {
		t.Fatal("filtered total included deleted task")
	}
}

func TestPostgresUserTagConcurrentCreationAndManualCAS(t *testing.T) {
	db := openPostgresRepositoryTestDB(t).db
	repo := NewUserTagRepository(db)
	ctx := context.Background()
	var wg sync.WaitGroup
	ids := make(chan string, 8)
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tag, err := repo.Create(ctx, 17, " ＰｏｓｔｇｒｅＳＱＬ ")
			if err != nil {
				errs <- err
			} else {
				ids <- tag.ID
			}
		}()
	}
	wg.Wait()
	close(ids)
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	id := ""
	for got := range ids {
		if id != "" && id != got {
			t.Fatal("concurrent create duplicated tag")
		}
		id = got
	}
	_, req := tagTaskFixture(t, db, 201, 17)
	req.Candidates = []TagCandidate{{Name: "连接池", Reason: "来源中的领域"}}
	published, err := repo.PublishCandidates(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := repo.PatchTask(ctx, 17, 201, TagPatch{ExpectedVersion: published.Version, AddIDs: []string{id}})
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	success, conflict := 0, 0
	for err := range results {
		if err == nil {
			success++
		} else {
			conflict++
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatalf("CAS successes=%d conflicts=%d", success, conflict)
	}
}
