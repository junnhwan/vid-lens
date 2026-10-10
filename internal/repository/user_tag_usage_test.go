package repository

import (
	"context"
	"gorm.io/gorm"
	"testing"
	"vid-lens/internal/model"
)

func TestUserTagUsageOrderBeforePagination(t *testing.T) {
	runUserTagUsageOrder(t, summaryRevisionDB(t))
}
func TestPostgresUserTagUsageOrderBeforePagination(t *testing.T) {
	runUserTagUsageOrder(t, openPostgresRepositoryTestDB(t).db)
}
func runUserTagUsageOrder(t *testing.T, db *gorm.DB) {
	repo := NewUserTagRepository(db)
	first := mustCreateTag(t, repo, 17, "Alpha")
	popular := mustCreateTag(t, repo, 17, "Zulu")
	createSourceTask(t, db, 711, 17)
	createSourceTask(t, db, 712, 17)
	ctx := context.Background()
	for _, id := range []int64{711, 712} {
		if _, err := repo.PatchTask(ctx, 17, id, TagPatch{AddIDs: []string{popular.ID}}); err != nil {
			t.Fatal(err)
		}
	}
	rows, total, err := repo.List(ctx, 17, 1, 1, "", "usage")
	if err != nil || total != 2 || len(rows) != 1 || rows[0].ID != popular.ID || rows[0].VideoCount != 2 {
		t.Fatal(rows, total, err)
	}
	if err = db.Delete(&model.VideoTask{}, 711).Error; err != nil {
		t.Fatal(err)
	}
	rows, _, err = repo.List(ctx, 17, 1, 1, "", "usage")
	if err != nil || rows[0].VideoCount != 1 {
		t.Fatal(rows, err)
	}
	rows, _, err = repo.List(ctx, 17, 1, 1, "")
	if err != nil || rows[0].ID != first.ID {
		t.Fatal("default name order changed", rows, err)
	}
	if _, _, err = repo.List(ctx, 17, 1, 1, "", "unknown"); err == nil {
		t.Fatal("unknown sort accepted")
	}
}
