package model

import "testing"

func TestPostgresArtifactMigrationRepairsSessionNullabilityOnRestart(t *testing.T) {
	db, _ := openPostgresModelTestDB(t)
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	chat := AgentRun{ID: "preserved-chat", UserID: 1, SessionID: 42, SubjectKind: "chat_session", SubjectID: "42", ExecutionKind: "chat", Status: AgentRunStatusCompleted}
	if err := db.Create(&chat).Error; err != nil {
		t.Fatal(err)
	}
	// The installed subject marker must not hide a historical column constraint.
	if err := db.Exec("ALTER TABLE agent_runs ALTER COLUMN session_id SET NOT NULL").Error; err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := Migrate(db); err != nil {
			t.Fatal(err)
		}
	}
	var nullable string
	if err := db.Raw("SELECT is_nullable FROM information_schema.columns WHERE table_schema=current_schema() AND table_name='agent_runs' AND column_name='session_id'").Scan(&nullable).Error; err != nil {
		t.Fatal(err)
	}
	if nullable != "YES" {
		t.Fatalf("artifact submissions still blocked: session_id nullable=%s", nullable)
	}
	for _, subject := range []string{AgentRunSubjectGeneration, AgentRunSubjectArtifactEdit, AgentRunSubjectSummaryEdit, AgentRunSubjectSummaryGeneration} {
		run := AgentRun{ID: subject, UserID: 1, SubjectKind: subject, SubjectID: "request", ExecutionKind: "artifact", RecipeVersion: "study-v1", Status: AgentRunStatusPending}
		if err := db.Create(&run).Error; err != nil {
			t.Fatal(err)
		}
	}
	var saved AgentRun
	if err := db.First(&saved, "id=?", chat.ID).Error; err != nil || saved.SessionID != 42 {
		t.Fatalf("chat identity changed: %+v %v", saved, err)
	}
}

func TestPostgresArtifactSubjectMigrationIsMonotonic(t *testing.T) {
	db, _ := openPostgresModelTestDB(t)
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	// Reproduce the historical narrower, unversioned constraint.
	for _, sql := range []string{
		"ALTER TABLE agent_runs DROP CONSTRAINT chk_agent_run_subject",
		"ALTER TABLE agent_runs ADD CONSTRAINT chk_agent_run_subject CHECK (subject_kind IN ('chat_session','generation_request','artifact_edit_request'))",
	} {
		if err := db.Exec(sql).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := migrateArtifactSubjects(db); err != nil {
		t.Fatal(err)
	}
	var originalOID int64
	if err := db.Raw("SELECT oid FROM pg_constraint WHERE conrelid='agent_runs'::regclass AND conname='chk_agent_run_subject'").Scan(&originalOID).Error; err != nil {
		t.Fatal(err)
	}
	valid := AgentRun{ID: "summary-migration", UserID: 1, SubjectKind: AgentRunSubjectSummaryEdit, SubjectID: "op", ExecutionKind: "artifact", RecipeVersion: "summary-edit-v1", Status: AgentRunStatusPending}
	if err := db.Create(&valid).Error; err != nil {
		t.Fatalf("summary subject not accepted after upgrade: %v", err)
	}
	invalid := valid
	invalid.ID, invalid.SubjectID = "invalid-migration", ""
	if err := db.Create(&invalid).Error; err == nil {
		t.Fatal("constraint accepted missing subject identity")
	}
	if err := db.Model(&valid).Update("recipe_version", nil).Error; err == nil {
		t.Fatal("constraint accepted NULL recipe version")
	}
	for _, marker := range []string{"vidlens:agent-run-subject:3", "vidlens:agent-run-subject:4"} {
		if err := db.Exec("COMMENT ON CONSTRAINT chk_agent_run_subject ON agent_runs IS '" + marker + "'").Error; err != nil {
			t.Fatal(err)
		}
		if err := migrateArtifactSubjects(db); err != nil {
			t.Fatal(err)
		}
		var current struct {
			OID    int64 `gorm:"column:oid"`
			Marker string
		}
		if err := db.Raw("SELECT oid, obj_description(oid, 'pg_constraint') AS marker FROM pg_constraint WHERE conrelid='agent_runs'::regclass AND conname='chk_agent_run_subject'").Scan(&current).Error; err != nil {
			t.Fatal(err)
		}
		if current.OID != originalOID || current.Marker != marker {
			t.Fatalf("migration rewrote current/newer constraint: %+v, original=%d marker=%s", current, originalOID, marker)
		}
	}
}
