package model

import "testing"

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
	for _, marker := range []string{"vidlens:agent-run-subject:2", "vidlens:agent-run-subject:3"} {
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
