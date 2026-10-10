package repository

import (
	"context"
	"testing"
	"vid-lens/internal/model"
	"vid-lens/internal/summarydoc"
)

func TestSummaryEditBeginRejectsChangedGeneratedSelection(t *testing.T) {
	db := summaryRevisionDB(t)
	repos, task, doc := revisionDocumentFixture(t, db)
	seedRevisionDocument(t, db, task, doc)
	current, err := repos.SummaryRevision.Effective(context.Background(), task.UserID, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	// Same body and artificial revision zero, but a different generated version.
	if err = db.Model(&model.AISummary{}).Where("task_id=?", task.ID).Update("generated_version", 2).Error; err != nil {
		t.Fatal(err)
	}
	op, run := summaryEditFixture(task.UserID, task.ID, current.Content)
	op.BaseContentHash = current.ContentDigest
	op.BaseContentHashKind = summarydoc.HashKind
	op.BaseDocumentJSON = current.DocumentJSON
	op.BaseGeneratedHash = current.BaseHash
	op.BaseGeneratedHashKind = current.BaseHashKind
	op.BaseGenerationID = current.GenerationID
	op.BaseSourceID, op.BaseSourceDigest = doc.SourceID, doc.SourceDigest
	oldVersion := int64(1)
	if _, err = repos.SummaryRevision.Begin(context.Background(), op, run, SummaryEditExpectation{ContentDigest: current.ContentDigest, VersionRef: &model.SummaryVersionRef{GeneratedVersion: &oldVersion}}); err == nil {
		t.Fatal("transaction accepted an old generated selection")
	}
	var count int64
	db.Model(&model.SummaryEditOperation{}).Count(&count)
	if count != 0 {
		t.Fatal("stale selection persisted operation")
	}
	db.Model(&model.AgentRun{}).Count(&count)
	if count != 0 {
		t.Fatal("stale selection persisted run")
	}
}
