package vector

import (
	"context"

	"vid-lens/internal/service"
)

type NoopStore struct{}

func NewNoopStore() *NoopStore {
	return &NoopStore{}
}

func (s *NoopStore) UpsertChunks(context.Context, []service.RAGVector) error {
	return nil
}

func (s *NoopStore) DeleteTaskChunks(context.Context, int64, int64, string) error {
	return nil
}
