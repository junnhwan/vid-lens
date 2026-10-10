package model

import (
	"strconv"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Idempotent upgrade: existing chat rows retain their session identities.
func migrateArtifactSubjects(db *gorm.DB) error {
	if db.Dialector.Name() != "postgres" {
		return nil
	}
	return db.Transaction(func(tx *gorm.DB) error {
		// Serialize this migration across API/worker startup. The constraint's
		// version travels with the constraint, so a newer migration cannot be
		// silently narrowed by a binary using this monotonic protocol.
		if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtext(current_schema() || ':agent_run_subject'))").Error; err != nil {
			return err
		}
		// AutoMigrate runs before this migration on every startup. Repair column
		// nullability even when the subject constraint already has a current marker.
		if err := tx.Exec("ALTER TABLE agent_runs ALTER COLUMN session_id DROP NOT NULL").Error; err != nil {
			return err
		}
		const version = 3
		const prefix = "vidlens:agent-run-subject:"
		var marker string
		if err := tx.Raw(`SELECT COALESCE(obj_description(oid, 'pg_constraint'), '') FROM pg_constraint
			WHERE conrelid = 'agent_runs'::regclass AND conname = 'chk_agent_run_subject'`).Scan(&marker).Error; err != nil {
			return err
		}
		if strings.HasPrefix(marker, prefix) {
			installed, err := strconv.Atoi(strings.TrimPrefix(marker, prefix))
			if err == nil && installed >= version {
				return nil
			}
		}
		for _, sql := range []string{
			"UPDATE agent_runs SET subject_kind='chat_session', subject_id=session_id::text, execution_kind='chat' WHERE subject_kind='chat_session' AND subject_id=''",
			"ALTER TABLE agent_runs DROP CONSTRAINT IF EXISTS chk_agent_run_subject",
			"ALTER TABLE agent_runs ADD CONSTRAINT chk_agent_run_subject CHECK ((subject_kind='chat_session' AND session_id IS NOT NULL AND session_id>0 AND execution_kind='chat') OR (subject_kind IN ('generation_request','artifact_edit_request','summary_edit_request','summary_generation') AND session_id IS NULL AND subject_id<>'' AND execution_kind='artifact' AND recipe_version IS NOT NULL AND recipe_version<>''))",
			"COMMENT ON CONSTRAINT chk_agent_run_subject ON agent_runs IS '" + prefix + strconv.Itoa(version) + "'",
		} {
			if err := tx.Exec(sql).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

// migrateArtifactEditDispatches removes any pre-release edit intents from the
// legacy generation outbox. The new table is deliberately invisible to an old
// artifact worker during a rolling deployment.
func migrateArtifactEditDispatches(db *gorm.DB) error {
	if !db.Migrator().HasTable(&GenerationDispatch{}) || !db.Migrator().HasTable(&ArtifactEditDispatch{}) || !db.Migrator().HasTable(&AgentRun{}) {
		return nil
	}
	return db.Transaction(func(tx *gorm.DB) error {
		editRunIDs := tx.Model(&AgentRun{}).Select("id").Where("execution_kind='artifact' AND subject_kind=?", AgentRunSubjectArtifactEdit)
		legacy := []GenerationDispatch{}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("run_id IN (?)", editRunIDs).Find(&legacy).Error; err != nil {
			return err
		}
		if len(legacy) == 0 {
			return nil
		}
		edits := make([]ArtifactEditDispatch, 0, len(legacy))
		for _, row := range legacy {
			edits = append(edits, ArtifactEditDispatch{
				ID: row.ID, RunID: row.RunID, NextAttemptAt: row.NextAttemptAt, CreatedAt: row.CreatedAt,
			})
		}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&edits).Error; err != nil {
			return err
		}
		ids := make([]string, 0, len(legacy))
		for _, row := range legacy {
			ids = append(ids, row.ID)
		}
		return tx.Where("id IN ?", ids).Delete(&GenerationDispatch{}).Error
	})
}
