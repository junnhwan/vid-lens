package repository

import (
	"context"
	"testing"
	"vid-lens/internal/model"
)

func TestPostgresArtifactSubjectUpgradePreservesChat(t *testing.T) {
	fixture := openPostgresRepositoryTestDB(t)
	db := fixture.db
	chat := createAgentExecutionRun(t, NewAgentExecutionRepository(db), "legacy-chat", 3, 2)
	// Recreate the relevant pre-artifact shape inside this disposable test schema.
	for _, sql := range []string{
		"ALTER TABLE agent_runs DROP CONSTRAINT chk_agent_run_subject",
		"ALTER TABLE agent_runs DROP COLUMN subject_kind, DROP COLUMN subject_id, DROP COLUMN execution_kind, DROP COLUMN recipe_version",
		"ALTER TABLE agent_runs ALTER COLUMN session_id SET NOT NULL",
	} {
		if err := db.Exec(sql).Error; err != nil {
			t.Fatal(err)
		}
	}
	for range 2 {
		if err := model.Migrate(db); err != nil {
			t.Fatal(err)
		}
	}
	stored, err := NewAgentExecutionRepository(db).GetRun(context.Background(), 7, chat.ID)
	if err != nil || stored == nil || stored.SessionID != 9 || stored.SubjectKind != "chat_session" || stored.SubjectID != "9" {
		t.Fatalf("chat upgrade %+v %v", stored, err)
	}
	generation := chat
	generation.ID = "new-generation"
	generation.SessionID = 0
	generation.SubjectKind = "generation_request"
	generation.SubjectID = "request-1"
	generation.ExecutionKind = "artifact"
	generation.RecipeVersion = "study-v1"
	if err = db.Create(&generation).Error; err != nil {
		t.Fatal(err)
	}
	var nulls int64
	if err = db.Raw("SELECT COUNT(*) FROM agent_runs WHERE id=? AND session_id IS NULL", generation.ID).Scan(&nulls).Error; err != nil || nulls != 1 {
		t.Fatalf("generation session null %d %v", nulls, err)
	}
	generation.ID = "invalid-generation"
	generation.SessionID = 9
	if err = db.Create(&generation).Error; err == nil {
		t.Fatal("generation accepted fake chat session")
	}
	edit := chat
	edit.ID = "new-artifact-edit"
	edit.SessionID = 0
	edit.SubjectKind = model.AgentRunSubjectArtifactEdit
	edit.SubjectID = "edit-request-1"
	edit.ExecutionKind = "artifact"
	edit.RecipeVersion = "study-edit-v1"
	if err = db.Create(&edit).Error; err != nil {
		t.Fatal(err)
	}
	edit.ID = "invalid-artifact-edit"
	edit.SessionID = 9
	if err = db.Create(&edit).Error; err == nil {
		t.Fatal("artifact edit accepted fake chat session")
	}
	if got, e := NewAgentExecutionRepository(db).GetRun(context.Background(), 7, "new-generation"); e != nil || got != nil {
		t.Fatalf("chat journal exposed artifact run %+v %v", got, e)
	}
}
