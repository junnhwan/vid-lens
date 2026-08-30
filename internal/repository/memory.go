package repository

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"vid-lens/internal/model"
)

const memoryEmbeddingTable = "agent_memory_embeddings"

var memoryAppendLocks [256]sync.Mutex

type MemoryRepository struct {
	db *gorm.DB
}

type MemoryAppendResult struct {
	Item        model.AgentMemoryItem
	Created     bool
	ConflictIDs []string
}

func NewMemoryRepository(db *gorm.DB) *MemoryRepository {
	return &MemoryRepository{db: db}
}

// Append preserves conflicting values in the same owner/scope/kind instead of
// replacing either one. Exact content duplicates reuse the existing item.
func (r *MemoryRepository) Append(ctx context.Context, item *model.AgentMemoryItem) (MemoryAppendResult, error) {
	if r == nil || r.db == nil || item == nil {
		return MemoryAppendResult{}, gorm.ErrInvalidData
	}
	item.ID = strings.TrimSpace(item.ID)
	if item.ID == "" {
		item.ID = uuid.NewString()
	}
	item.ScopeType = strings.TrimSpace(item.ScopeType)
	item.ScopeID = strings.TrimSpace(item.ScopeID)
	item.Kind = strings.TrimSpace(item.Kind)
	item.Content = strings.TrimSpace(item.Content)
	item.SourceType = strings.TrimSpace(item.SourceType)
	item.SourceRef = strings.TrimSpace(item.SourceRef)
	if item.UserID <= 0 || item.ScopeID == "" || item.Kind == "" || item.Content == "" || item.SourceType == "" || item.SourceRef == "" {
		return MemoryAppendResult{}, gorm.ErrInvalidData
	}
	if item.Version <= 0 {
		item.Version = 1
	}
	if item.Status == "" {
		item.Status = model.MemoryStatusActive
	}
	if item.Importance < 0 || item.Importance > 1 {
		return MemoryAppendResult{}, gorm.ErrInvalidData
	}
	lockKey := fmt.Sprintf("%d\x00%s\x00%s\x00%s", item.UserID, item.ScopeType, item.ScopeID, item.Kind)
	lock := &memoryAppendLocks[memoryLockHash(lockKey)%uint64(len(memoryAppendLocks))]
	lock.Lock()
	defer lock.Unlock()

	var result MemoryAppendResult
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if tx.Dialector.Name() == "postgres" {
			if err := tx.Exec("SELECT pg_advisory_xact_lock(?)", int64(memoryLockHash(lockKey))).Error; err != nil {
				return err
			}
		}
		var existing []model.AgentMemoryItem
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("user_id = ? AND scope_type = ? AND scope_id = ? AND kind = ? AND status IN ?",
				item.UserID, item.ScopeType, item.ScopeID, item.Kind, []string{model.MemoryStatusActive, model.MemoryStatusConflicted}).
			Order("created_at ASC, id ASC").Find(&existing).Error; err != nil {
			return err
		}
		for _, candidate := range existing {
			if strings.EqualFold(strings.TrimSpace(candidate.Content), item.Content) {
				result.Item = candidate
				return nil
			}
		}

		if len(existing) > 0 {
			item.Status = model.MemoryStatusConflicted
			for i := range existing {
				current := &existing[i]
				result.ConflictIDs = append(result.ConflictIDs, current.ID)
				if current.Status == model.MemoryStatusConflicted {
					continue
				}
				current.Status = model.MemoryStatusConflicted
				current.Version++
				if err := tx.Model(&model.AgentMemoryItem{}).Where("id = ? AND user_id = ?", current.ID, item.UserID).
					Updates(map[string]any{"status": current.Status, "version": current.Version}).Error; err != nil {
					return err
				}
				if err := createMemoryEvent(tx, current, model.MemoryEventConflicted, item.SourceRef); err != nil {
					return err
				}
			}
		}
		if err := tx.Create(item).Error; err != nil {
			return err
		}
		if err := createMemoryEvent(tx, item, model.MemoryEventCreated, item.SourceRef); err != nil {
			return err
		}
		result.Item = *item
		result.Created = true
		result.ConflictIDs = append(result.ConflictIDs, item.ID)
		return nil
	})
	return result, err
}

func memoryLockHash(value string) uint64 {
	hash := fnv.New64a()
	_, _ = hash.Write([]byte(value))
	return hash.Sum64()
}

func createMemoryEvent(tx *gorm.DB, item *model.AgentMemoryItem, eventType, sourceRef string) error {
	return tx.Create(&model.AgentMemoryEvent{
		ID: uuid.NewString(), MemoryID: item.ID, UserID: item.UserID,
		EventType: eventType, Version: item.Version, SourceRef: strings.TrimSpace(sourceRef), OccurredAt: time.Now().UTC(),
	}).Error
}

func (r *MemoryRepository) ListRecallable(ctx context.Context, userID int64, scopes map[string][]string, limit int, now time.Time) ([]model.AgentMemoryItem, error) {
	if r == nil || r.db == nil || userID <= 0 || limit <= 0 || len(scopes) == 0 {
		return []model.AgentMemoryItem{}, nil
	}
	query := r.db.WithContext(ctx).Where("user_id = ?", userID).
		Where("status IN ?", []string{model.MemoryStatusActive, model.MemoryStatusConflicted}).
		Where("source_ref <> ''").
		Where("expires_at IS NULL OR expires_at > ?", now)
	scopeQuery := r.db.Session(&gorm.Session{NewDB: true})
	first := true
	for scopeType, scopeIDs := range scopes {
		if len(scopeIDs) == 0 {
			continue
		}
		condition := r.db.Where("scope_type = ? AND scope_id IN ?", scopeType, scopeIDs)
		if first {
			scopeQuery = condition
			first = false
		} else {
			scopeQuery = scopeQuery.Or(condition)
		}
	}
	if first {
		return []model.AgentMemoryItem{}, nil
	}
	var seeds []model.AgentMemoryItem
	err := query.Where(scopeQuery).
		Order("importance DESC, created_at DESC, id ASC").Limit(limit).Find(&seeds).Error
	if err != nil {
		return nil, err
	}
	return r.expandRecallableConflicts(ctx, userID, seeds, now)
}

// SearchRecallable uses the pgvector projection for semantic seed ranking, then
// expands every seeded conflict group from the relational source of truth.
func (r *MemoryRepository) SearchRecallable(ctx context.Context, userID int64, scopes map[string][]string, embeddingModel string, vector []float32, limit int, now time.Time) ([]model.AgentMemoryItem, error) {
	if r == nil || r.db == nil || userID <= 0 || len(scopes) == 0 || len(vector) == 0 || limit <= 0 {
		return []model.AgentMemoryItem{}, nil
	}
	vectorLiteral := formatMemoryVector(vector)
	query := r.db.WithContext(ctx).Model(&model.AgentMemoryItem{}).
		Select("agent_memory_items.*, 1 - (ame.embedding <=> ?::vector) AS semantic_score", vectorLiteral).
		Joins("JOIN "+memoryEmbeddingTable+" AS ame ON ame.memory_id = agent_memory_items.id").
		Where("agent_memory_items.user_id = ? AND agent_memory_items.status IN ? AND agent_memory_items.source_ref <> ''", userID, []string{model.MemoryStatusActive, model.MemoryStatusConflicted}).
		Where("agent_memory_items.expires_at IS NULL OR agent_memory_items.expires_at > ?", now).
		Where("ame.embedding_model = ?", strings.TrimSpace(embeddingModel))
	scopeQuery := r.db.Session(&gorm.Session{NewDB: true})
	first := true
	for scopeType, scopeIDs := range scopes {
		if len(scopeIDs) == 0 {
			continue
		}
		condition := r.db.Where("agent_memory_items.scope_type = ? AND agent_memory_items.scope_id IN ?", scopeType, scopeIDs)
		if first {
			scopeQuery, first = condition, false
		} else {
			scopeQuery = scopeQuery.Or(condition)
		}
	}
	if first {
		return []model.AgentMemoryItem{}, nil
	}
	var seeds []model.AgentMemoryItem
	if err := query.Where(scopeQuery).Order("semantic_score DESC, agent_memory_items.importance DESC, agent_memory_items.id ASC").Limit(limit).Find(&seeds).Error; err != nil {
		return nil, err
	}
	return r.expandRecallableConflicts(ctx, userID, seeds, now)
}

func (r *MemoryRepository) expandRecallableConflicts(ctx context.Context, userID int64, seeds []model.AgentMemoryItem, now time.Time) ([]model.AgentMemoryItem, error) {
	items := append([]model.AgentMemoryItem(nil), seeds...)
	seen := make(map[string]struct{}, len(items))
	conflictQuery := r.db.Session(&gorm.Session{NewDB: true})
	first := true
	for _, item := range seeds {
		seen[item.ID] = struct{}{}
		if item.Status != model.MemoryStatusConflicted {
			continue
		}
		condition := r.db.Where("scope_type = ? AND scope_id = ? AND kind = ?", item.ScopeType, item.ScopeID, item.Kind)
		if first {
			conflictQuery, first = condition, false
		} else {
			conflictQuery = conflictQuery.Or(condition)
		}
	}
	if !first {
		var siblings []model.AgentMemoryItem
		if err := r.db.WithContext(ctx).Where("user_id = ? AND status IN ? AND source_ref <> ''", userID, []string{model.MemoryStatusActive, model.MemoryStatusConflicted}).
			Where("expires_at IS NULL OR expires_at > ?", now).Where(conflictQuery).Find(&siblings).Error; err != nil {
			return nil, err
		}
		for _, sibling := range siblings {
			if _, ok := seen[sibling.ID]; ok {
				continue
			}
			seen[sibling.ID] = struct{}{}
			items = append(items, sibling)
		}
	}
	sort.SliceStable(items, func(i, j int) bool {
		left, right := items[i], items[j]
		if left.SemanticScore != nil || right.SemanticScore != nil {
			if left.SemanticScore == nil {
				return false
			}
			if right.SemanticScore == nil {
				return true
			}
			if *left.SemanticScore != *right.SemanticScore {
				return *left.SemanticScore > *right.SemanticScore
			}
		}
		if left.Importance != right.Importance {
			return left.Importance > right.Importance
		}
		return left.ID < right.ID
	})
	return items, nil
}

func (r *MemoryRepository) WithdrawForUser(ctx context.Context, userID int64, memoryID, sourceRef string) error {
	return r.transition(ctx, userID, memoryID, model.MemoryStatusWithdrawn, model.MemoryEventWithdrawn, sourceRef, false)
}

func (r *MemoryRepository) DeleteForUser(ctx context.Context, userID int64, memoryID, sourceRef string) error {
	return r.transition(ctx, userID, memoryID, model.MemoryStatusDeleted, model.MemoryEventDeleted, sourceRef, true)
}

func (r *MemoryRepository) transition(ctx context.Context, userID int64, memoryID, status, eventType, sourceRef string, softDelete bool) error {
	memoryID, sourceRef = strings.TrimSpace(memoryID), strings.TrimSpace(sourceRef)
	if r == nil || r.db == nil || userID <= 0 || memoryID == "" || sourceRef == "" {
		return gorm.ErrInvalidData
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var item model.AgentMemoryItem
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND user_id = ?", memoryID, userID).First(&item).Error; err != nil {
			return err
		}
		item.Status = status
		item.Version++
		updates := map[string]any{"status": status, "version": item.Version}
		if softDelete {
			updates["deleted_at"] = time.Now().UTC()
		}
		if err := tx.Model(&model.AgentMemoryItem{}).Where("id = ? AND user_id = ?", memoryID, userID).Updates(updates).Error; err != nil {
			return err
		}
		if tx.Migrator().HasTable(memoryEmbeddingTable) {
			if err := tx.Exec("DELETE FROM "+memoryEmbeddingTable+" WHERE memory_id = ? AND user_id = ?", memoryID, userID).Error; err != nil {
				return err
			}
		}
		return createMemoryEvent(tx, &item, eventType, sourceRef)
	})
}

func (r *MemoryRepository) SetEmbeddingRef(ctx context.Context, userID int64, memoryID, ref string) error {
	if r == nil || r.db == nil || userID <= 0 || strings.TrimSpace(memoryID) == "" || strings.TrimSpace(ref) == "" {
		return gorm.ErrInvalidData
	}
	result := r.db.WithContext(ctx).Model(&model.AgentMemoryItem{}).
		Where("id = ? AND user_id = ?", memoryID, userID).
		Update("embedding_ref", strings.TrimSpace(ref))
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// EnsureEmbeddingSchema creates the optional pgvector projection. It is kept
// outside AllModels because embeddings are rebuildable and use a PostgreSQL-
// specific vector column.
func (r *MemoryRepository) EnsureEmbeddingSchema(ctx context.Context, dimension int) error {
	if r == nil || r.db == nil || dimension <= 0 {
		return gorm.ErrInvalidData
	}
	if err := r.db.WithContext(ctx).Exec("CREATE EXTENSION IF NOT EXISTS vector").Error; err != nil {
		return err
	}
	statement := fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
		memory_id VARCHAR(36) PRIMARY KEY,
		user_id BIGINT NOT NULL,
		scope_type VARCHAR(30) NOT NULL,
		scope_id VARCHAR(100) NOT NULL,
		embedding_model TEXT NOT NULL,
		embedding_dim INTEGER NOT NULL,
		embedding vector(%d) NOT NULL,
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	)`, memoryEmbeddingTable, dimension)
	if err := r.db.WithContext(ctx).Exec(statement).Error; err != nil {
		return err
	}
	return r.db.WithContext(ctx).Exec(fmt.Sprintf(
		"CREATE INDEX IF NOT EXISTS %s_scope_idx ON %s (user_id, scope_type, scope_id)", memoryEmbeddingTable, memoryEmbeddingTable)).Error
}

func (r *MemoryRepository) UpsertEmbedding(ctx context.Context, item model.AgentMemoryItem, modelName string, vector []float32) (string, error) {
	if r == nil || r.db == nil || item.ID == "" || item.UserID <= 0 || strings.TrimSpace(modelName) == "" || len(vector) == 0 {
		return "", gorm.ErrInvalidData
	}
	vectorLiteral := formatMemoryVector(vector)
	statement := fmt.Sprintf(`INSERT INTO %s
		(memory_id, user_id, scope_type, scope_id, embedding_model, embedding_dim, embedding, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?::vector, NOW())
		ON CONFLICT (memory_id) DO UPDATE SET
		user_id = EXCLUDED.user_id, scope_type = EXCLUDED.scope_type, scope_id = EXCLUDED.scope_id,
		embedding_model = EXCLUDED.embedding_model, embedding_dim = EXCLUDED.embedding_dim,
		embedding = EXCLUDED.embedding, updated_at = NOW()`, memoryEmbeddingTable)
	if err := r.db.WithContext(ctx).Exec(statement, item.ID, item.UserID, item.ScopeType, item.ScopeID, modelName, len(vector), vectorLiteral).Error; err != nil {
		return "", err
	}
	return memoryEmbeddingTable + ":" + item.ID, nil
}

func formatMemoryVector(vector []float32) string {
	parts := make([]string, len(vector))
	for i, value := range vector {
		parts[i] = strconv.FormatFloat(float64(value), 'g', -1, 32)
	}
	return "[" + strings.Join(parts, ",") + "]"
}

func (r *MemoryRepository) FindForUser(ctx context.Context, userID int64, memoryID string) (*model.AgentMemoryItem, error) {
	var item model.AgentMemoryItem
	err := r.db.WithContext(ctx).Where("id = ? AND user_id = ?", strings.TrimSpace(memoryID), userID).First(&item).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &item, err
}

func (r *MemoryRepository) ListForUser(ctx context.Context, userID int64, scopeType, scopeID string) ([]model.AgentMemoryItem, error) {
	if r == nil || r.db == nil || userID <= 0 {
		return nil, gorm.ErrInvalidData
	}
	query := r.db.WithContext(ctx).Where("user_id = ?", userID)
	if strings.TrimSpace(scopeType) != "" {
		query = query.Where("scope_type = ?", strings.TrimSpace(scopeType))
	}
	if strings.TrimSpace(scopeID) != "" {
		query = query.Where("scope_id = ?", strings.TrimSpace(scopeID))
	}
	var items []model.AgentMemoryItem
	err := query.Order("updated_at DESC, id ASC").Find(&items).Error
	return items, err
}
