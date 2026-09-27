package model

import "gorm.io/gorm"

// Idempotent upgrade: existing chat rows retain their session identities.
func migrateArtifactSubjects(db *gorm.DB) error {
	if db.Dialector.Name() != "postgres" {
		return nil
	}
	return db.Transaction(func(tx *gorm.DB) error {
		for _, sql := range []string{
			"ALTER TABLE agent_runs ALTER COLUMN session_id DROP NOT NULL",
			"UPDATE agent_runs SET subject_kind='chat_session', subject_id=session_id::text, execution_kind='chat' WHERE subject_kind='chat_session' AND subject_id=''",
			"ALTER TABLE agent_runs DROP CONSTRAINT IF EXISTS chk_agent_run_subject",
			"ALTER TABLE agent_runs ADD CONSTRAINT chk_agent_run_subject CHECK ((subject_kind='chat_session' AND session_id IS NOT NULL AND session_id>0 AND execution_kind='chat') OR (subject_kind='generation_request' AND session_id IS NULL AND subject_id<>'' AND execution_kind='artifact' AND recipe_version<>''))",
		} {
			if err := tx.Exec(sql).Error; err != nil {
				return err
			}
		}
		return nil
	})
}
