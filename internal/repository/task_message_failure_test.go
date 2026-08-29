package repository

import (
	"testing"

	"vid-lens/internal/model"
)

func TestKafkaMessageFailureRepositoryIsIdempotentByConsumerOffset(t *testing.T) {
	repos := newTestRepositories(t)
	failure := &model.KafkaMessageFailure{ConsumerGroup: "rag-group", ConsumerName: "rag_index", Topic: "rag", Partition: 2, MessageOffset: 41, MessageKey: []byte("key"), Payload: []byte("bad"), ErrorMessage: "invalid json"}
	if err := repos.TaskMessageFailure.Record(failure); err != nil {
		t.Fatalf("first record: %v", err)
	}
	failure.ErrorMessage = "same poison retried"
	if err := repos.TaskMessageFailure.Record(failure); err != nil {
		t.Fatalf("second record: %v", err)
	}
	var count int64
	if err := repos.db.Model(&model.KafkaMessageFailure{}).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("poison rows = %d, want 1", count)
	}
	var stored model.KafkaMessageFailure
	if err := repos.db.First(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if stored.ErrorMessage != "same poison retried" {
		t.Fatalf("error = %q", stored.ErrorMessage)
	}
}
