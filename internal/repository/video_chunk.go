package repository

import (
	"context"
	"errors"
	"math"
	"sort"
	"strings"
	"unicode"

	"gorm.io/gorm"
	"vid-lens/internal/model"
)

type VideoChunkRepository struct {
	db *gorm.DB
}

type VideoChunkSearchResult struct {
	Chunk model.VideoChunk
	Score float64
	Rank  int
}

func NewVideoChunkRepository(db *gorm.DB) *VideoChunkRepository {
	return &VideoChunkRepository{db: db}
}

func (r *VideoChunkRepository) ReplaceTaskChunks(taskID int64, embeddingModel string, chunks []model.VideoChunk) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("task_id = ? AND embedding_model = ?", taskID, embeddingModel).
			Delete(&model.VideoChunk{}).Error; err != nil {
			return err
		}
		if len(chunks) == 0 {
			return nil
		}
		return tx.Create(&chunks).Error
	})
}

func (r *VideoChunkRepository) ListAllByTaskID(taskID int64) ([]model.VideoChunk, error) {
	var chunks []model.VideoChunk
	err := r.db.Where("task_id = ?", taskID).
		Order("user_id asc, embedding_model asc, chunk_index asc").
		Find(&chunks).Error
	return chunks, err
}

// ListForReindex returns a stable ID-ordered page for resumable vector
// rebuilding. Zero-valued filters are intentionally ignored.
func (r *VideoChunkRepository) ListForReindex(ctx context.Context, afterID int64, limit int, userID, taskID int64, embeddingModel string) ([]model.VideoChunk, error) {
	if limit <= 0 {
		limit = 100
	}
	query := r.db.WithContext(ctx).Where("id > ?", afterID)
	if userID > 0 {
		query = query.Where("user_id = ?", userID)
	}
	if taskID > 0 {
		query = query.Where("task_id = ?", taskID)
	}
	if embeddingModel = strings.TrimSpace(embeddingModel); embeddingModel != "" {
		query = query.Where("embedding_model = ?", embeddingModel)
	}
	var chunks []model.VideoChunk
	err := query.Order("id ASC").Limit(limit).Find(&chunks).Error
	return chunks, err
}

func (r *VideoChunkRepository) ListByTaskID(userID, taskID int64, embeddingModel string) ([]model.VideoChunk, error) {
	var chunks []model.VideoChunk
	err := r.db.Where("user_id = ? AND task_id = ? AND embedding_model = ?", userID, taskID, embeddingModel).
		Order("chunk_index asc").
		Find(&chunks).Error
	return chunks, err
}

func (r *VideoChunkRepository) ListByIDs(userID int64, taskIDs []int64, embeddingModel string, chunkIDs []int64) ([]model.VideoChunk, error) {
	if len(taskIDs) == 0 || len(chunkIDs) == 0 {
		return []model.VideoChunk{}, nil
	}
	var chunks []model.VideoChunk
	err := r.db.Where("user_id = ? AND task_id IN ? AND embedding_model = ? AND id IN ?", userID, taskIDs, embeddingModel, chunkIDs).
		Order("task_id asc, chunk_index asc").Find(&chunks).Error
	return chunks, err
}

func (r *VideoChunkRepository) FindByIdentity(userID, taskID, chunkID int64, evidenceID string) (*model.VideoChunk, error) {
	query := r.db.Where("user_id = ? AND task_id = ?", userID, taskID)
	switch {
	case chunkID > 0 && strings.TrimSpace(evidenceID) != "":
		query = query.Where("id = ? AND vector_id = ?", chunkID, strings.TrimSpace(evidenceID))
	case chunkID > 0:
		query = query.Where("id = ?", chunkID)
	case strings.TrimSpace(evidenceID) != "":
		query = query.Where("vector_id = ?", strings.TrimSpace(evidenceID))
	default:
		return nil, nil
	}
	var chunk model.VideoChunk
	err := query.First(&chunk).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &chunk, err
}

func (r *VideoChunkRepository) ListByIndexRange(userID, taskID int64, embeddingModel string, start, end int) ([]model.VideoChunk, error) {
	var chunks []model.VideoChunk
	err := r.db.Where(
		"user_id = ? AND task_id = ? AND embedding_model = ? AND chunk_index >= ? AND chunk_index <= ?",
		userID, taskID, embeddingModel, start, end,
	).
		Order("chunk_index asc").
		Find(&chunks).Error
	return chunks, err
}

func (r *VideoChunkRepository) ListVisualByTimeRange(userID, taskID int64, embeddingModel string, startMS, endMS int64, limit int) ([]model.VideoChunk, error) {
	if limit <= 0 || limit > 24 {
		limit = 24
	}
	var chunks []model.VideoChunk
	err := r.db.Where(
		"user_id = ? AND task_id = ? AND embedding_model = ? AND modality IN ? AND start_ms < ? AND end_ms > ? AND time_range_status <> ?",
		userID, taskID, embeddingModel,
		[]string{model.ChunkModalityVisualOCR, model.ChunkModalityVisualCaption}, endMS, startMS, model.ChunkTimeRangeUnknown,
	).Order("start_ms asc, modality asc, chunk_index asc").Limit(limit).Find(&chunks).Error
	return chunks, err
}

func (r *VideoChunkRepository) SearchByBM25(userID, taskID int64, embeddingModel string, terms []string, limit int) ([]VideoChunkSearchResult, error) {
	return r.SearchTasksByBM25(context.Background(), userID, []int64{taskID}, embeddingModel, terms, limit)
}

// SearchTasksByBM25 uses one authorized collection as the corpus, so scores
// share document-frequency and length statistics across videos.
func (r *VideoChunkRepository) SearchTasksByBM25(ctx context.Context, userID int64, taskIDs []int64, embeddingModel string, terms []string, limit int) ([]VideoChunkSearchResult, error) {
	if userID <= 0 || len(taskIDs) == 0 {
		return nil, nil
	}
	corpus, err := r.LoadBM25Corpus(ctx, userID, taskIDs, embeddingModel)
	if err != nil {
		return nil, err
	}
	return corpus.Search(ctx, terms, taskIDs, limit)
}

// BM25Corpus is immutable and request-scoped; target searches retain collection statistics.
type BM25Corpus struct {
	chunks    []model.VideoChunk
	freqs     []map[string]int
	lengths   []float64
	docFreq   map[string]int
	avgLength float64
}

func (r *VideoChunkRepository) LoadBM25Corpus(ctx context.Context, userID int64, taskIDs []int64, embeddingModel string) (*BM25Corpus, error) {
	var chunks []model.VideoChunk
	if userID > 0 && len(taskIDs) > 0 {
		if err := r.db.WithContext(ctx).Where("user_id = ? AND task_id IN ? AND embedding_model = ?", userID, taskIDs, embeddingModel).Order("task_id ASC, chunk_index ASC, id ASC").Find(&chunks).Error; err != nil {
			return nil, err
		}
	}
	return NewBM25Corpus(ctx, chunks)
}

func NewBM25Corpus(ctx context.Context, chunks []model.VideoChunk) (*BM25Corpus, error) {
	c := &BM25Corpus{chunks: chunks, docFreq: map[string]int{}}
	total := 0.0
	for _, chunk := range chunks {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		tokens := tokenizeBM25Text(chunk.Content)
		freq := map[string]int{}
		for _, token := range tokens {
			freq[token]++
		}
		for term := range freq {
			c.docFreq[term]++
		}
		c.freqs = append(c.freqs, freq)
		length := float64(max(1, len(tokens)))
		c.lengths = append(c.lengths, length)
		total += length
	}
	c.avgLength = 1
	if len(chunks) > 0 {
		c.avgLength = total / float64(len(chunks))
	}
	return c, nil
}

func (c *BM25Corpus) Search(ctx context.Context, terms []string, taskIDs []int64, limit int) ([]VideoChunkSearchResult, error) {
	if c == nil {
		return nil, nil
	}
	terms = normalizeSearchTerms(terms)
	allowed := map[int64]bool{}
	for _, id := range taskIDs {
		allowed[id] = true
	}
	if limit <= 0 {
		limit = 20
	}
	if limit > 50 {
		limit = 50
	}
	const k1, b = 1.5, 0.75
	results := make([]VideoChunkSearchResult, 0)
	n := float64(len(c.chunks))
	for i, chunk := range c.chunks {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !allowed[chunk.TaskID] {
			continue
		}
		score := 0.0
		for _, term := range terms {
			tf := float64(c.freqs[i][term])
			if tf == 0 {
				continue
			}
			df := float64(c.docFreq[term])
			idf := math.Log(1 + (n-df+0.5)/(df+0.5))
			score += idf * (tf * (k1 + 1)) / (tf + k1*(1-b+b*c.lengths[i]/c.avgLength))
		}
		if score > 0 {
			results = append(results, VideoChunkSearchResult{Chunk: chunk, Score: score})
		}
	}
	sort.SliceStable(results, func(i, j int) bool { return results[i].Score > results[j].Score })
	if len(results) > limit {
		results = results[:limit]
	}
	for i := range results {
		results[i].Rank = i + 1
	}
	return results, nil
}

func (r *VideoChunkRepository) ListEmbeddingModelsByTask(userID, taskID int64) ([]string, error) {
	var models []string
	err := r.db.Model(&model.VideoChunk{}).
		Where("user_id = ? AND task_id = ?", userID, taskID).
		Distinct("embedding_model").
		Order("embedding_model asc").
		Pluck("embedding_model", &models).Error
	return models, err
}

func (r *VideoChunkRepository) DeleteByTaskID(taskID int64) error {
	return r.db.Where("task_id = ?", taskID).Delete(&model.VideoChunk{}).Error
}

func normalizeSearchTerms(terms []string) []string {
	seen := make(map[string]bool, len(terms))
	out := make([]string, 0, len(terms))
	for _, term := range terms {
		term = strings.ToLower(strings.TrimSpace(term))
		if term == "" || seen[term] {
			continue
		}
		seen[term] = true
		out = append(out, term)
	}
	return out
}

// tokenizeBM25Text creates deterministic word tokens for Latin text and 2-4
// character n-grams for contiguous Han text. Unlike substring counting this
// prevents an ASCII query such as "red" from matching "redis", while still
// providing reproducible lexical statistics without depending on a database-specific
// deployment-specific Chinese full-text parser.
func tokenizeBM25Text(text string) []string {
	text = strings.ToLower(strings.TrimSpace(text))
	if text == "" {
		return nil
	}
	var tokens []string
	var latin, han []rune
	flushLatin := func() {
		if len(latin) > 0 {
			tokens = append(tokens, string(latin))
		}
		latin = latin[:0]
	}
	flushHan := func() {
		if len(han) == 0 {
			return
		}
		if len(han) == 1 {
			tokens = append(tokens, string(han))
			han = han[:0]
			return
		}
		for n := 2; n <= 4; n++ {
			if len(han) < n {
				continue
			}
			for i := 0; i+n <= len(han); i++ {
				tokens = append(tokens, string(han[i:i+n]))
			}
		}
		han = han[:0]
	}
	for _, r := range text {
		switch {
		case unicode.Is(unicode.Han, r):
			flushLatin()
			han = append(han, r)
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			flushHan()
			latin = append(latin, r)
		default:
			flushLatin()
			flushHan()
		}
	}
	flushLatin()
	flushHan()
	return tokens
}

type ChunkEvidenceManifestEntry struct {
	UserID         int64  `json:"user_id" yaml:"user_id"`
	TaskID         int64  `json:"task_id" yaml:"task_id"`
	ChunkID        int64  `json:"chunk_id" yaml:"chunk_id"`
	ChunkIndex     int    `json:"chunk_index" yaml:"chunk_index"`
	EvidenceID     string `json:"evidence_id" yaml:"evidence_id"`
	ContentHash    string `json:"content_hash" yaml:"content_hash"`
	Content        string `json:"content" yaml:"content"`
	EmbeddingModel string `json:"embedding_model" yaml:"embedding_model"`
}

func (r *VideoChunkRepository) ListEvidenceManifest(userID, taskID int64, embeddingModel string) ([]ChunkEvidenceManifestEntry, error) {
	var chunks []model.VideoChunk
	if err := r.db.Where("user_id = ? AND task_id = ? AND embedding_model = ?", userID, taskID, embeddingModel).Order("chunk_index ASC, id ASC").Find(&chunks).Error; err != nil {
		return nil, err
	}
	return chunkEvidenceManifest(chunks), nil
}

// ListAllEvidenceManifest returns every relational RAG source row in a stable
// scope order for the migration-wide projection audit.
func (r *VideoChunkRepository) ListAllEvidenceManifest(ctx context.Context) ([]ChunkEvidenceManifestEntry, error) {
	var chunks []model.VideoChunk
	if err := r.db.WithContext(ctx).
		Order("user_id ASC, task_id ASC, embedding_model ASC, chunk_index ASC, id ASC").
		Find(&chunks).Error; err != nil {
		return nil, err
	}
	return chunkEvidenceManifest(chunks), nil
}

func chunkEvidenceManifest(chunks []model.VideoChunk) []ChunkEvidenceManifestEntry {
	out := make([]ChunkEvidenceManifestEntry, 0, len(chunks))
	for _, chunk := range chunks {
		out = append(out, ChunkEvidenceManifestEntry{UserID: chunk.UserID, TaskID: chunk.TaskID, ChunkID: chunk.ID, ChunkIndex: chunk.ChunkIndex, EvidenceID: chunk.VectorID, ContentHash: chunk.ContentHash, Content: chunk.Content, EmbeddingModel: chunk.EmbeddingModel})
	}
	return out
}
