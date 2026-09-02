package vector

import (
	"context"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"vid-lens/internal/service"
)

func TestPGVectorStoreSearchFiltersTaskSetAndReturnsTaskID(t *testing.T) {
	store, mock, cleanup := newMockPGStore(t, testPGConfig())
	defer cleanup()
	mock.ExpectQuery(regexp.QuoteMeta("task_id IN ($3,$4)")).
		WithArgs("[1,0,0]", int64(7), int64(2), int64(3), "embed-model", 3).
		WillReturnRows(sqlmock.NewRows([]string{"vector_id", "task_id", "chunk_id", "chunk_index", "content", "score"}).
			AddRow("v-1", int64(3), int64(9), 2, "hello", 0.9))
	results, err := store.Search(context.Background(), []float32{1, 0, 0}, service.RetrievalRequest{UserID: 7, TaskIDs: []int64{3, 2, 3}, EmbeddingModel: "embed-model", TopK: 3})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].TaskID != 3 {
		t.Fatalf("results=%+v", results)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
