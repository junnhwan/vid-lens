package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"vid-lens/internal/model"
	"vid-lens/internal/repository"
)

const MemorySnapshotSchemaVersion = "memory-snapshot/v1"

type MemoryScope struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

type MemorySnapshotRequest struct {
	UserID    int64
	Query     string
	Scopes    []MemoryScope
	TopK      int
	MaxChars  int
	MaxTokens int
}

type MemorySnapshotItem struct {
	ID           string    `json:"id"`
	ScopeType    string    `json:"scope_type"`
	ScopeID      string    `json:"scope_id"`
	Kind         string    `json:"kind"`
	Content      string    `json:"content"`
	SourceType   string    `json:"source_type"`
	SourceRef    string    `json:"source_ref"`
	Importance   float64   `json:"importance"`
	EmbeddingRef string    `json:"embedding_ref,omitempty"`
	Status       string    `json:"status"`
	Version      int64     `json:"version"`
	CreatedAt    time.Time `json:"created_at"`
	Conflict     bool      `json:"conflict,omitempty"`
	Truncated    bool      `json:"truncated,omitempty"`
}

type MemoryConflict struct {
	ScopeType string   `json:"scope_type"`
	ScopeID   string   `json:"scope_id"`
	Kind      string   `json:"kind"`
	MemoryIDs []string `json:"memory_ids"`
}

type MemorySnapshotBudget struct {
	TopK       int `json:"top_k"`
	MaxChars   int `json:"max_chars"`
	MaxTokens  int `json:"max_tokens"`
	UsedChars  int `json:"used_chars"`
	UsedTokens int `json:"used_tokens"`
}

type MemorySnapshot struct {
	SchemaVersion string               `json:"schema_version"`
	Version       string               `json:"version"`
	MemoryIDs     []string             `json:"memory_ids"`
	Items         []MemorySnapshotItem `json:"items"`
	Conflicts     []MemoryConflict     `json:"conflicts"`
	Budget        MemorySnapshotBudget `json:"budget"`
}

type MemoryProvider interface {
	Snapshot(ctx context.Context, request MemorySnapshotRequest) (MemorySnapshot, error)
}

type MemoryRetrieveRequest struct {
	UserID int64
	Query  string
	Scopes []MemoryScope
	Limit  int
	Now    time.Time
}

type MemoryRetriever interface {
	Retrieve(ctx context.Context, request MemoryRetrieveRequest) ([]model.AgentMemoryItem, error)
}

type MemoryScopeAuthorizer interface {
	Authorize(ctx context.Context, userID int64, scope MemoryScope, write bool) error
}

type AgentMemoryConfig struct {
	TopK      int
	MaxChars  int
	MaxTokens int
	Now       func() time.Time
}

func DefaultAgentMemoryConfig() AgentMemoryConfig {
	return AgentMemoryConfig{TopK: 6, MaxChars: 2000, MaxTokens: 500, Now: time.Now}
}

type ScopedMemoryProvider struct {
	retriever  MemoryRetriever
	authorizer MemoryScopeAuthorizer
	config     AgentMemoryConfig
}

func NewScopedMemoryProvider(retriever MemoryRetriever, authorizer MemoryScopeAuthorizer, config AgentMemoryConfig) *ScopedMemoryProvider {
	defaults := DefaultAgentMemoryConfig()
	if config.TopK <= 0 {
		config.TopK = defaults.TopK
	}
	if config.MaxChars <= 0 {
		config.MaxChars = defaults.MaxChars
	}
	if config.MaxTokens <= 0 {
		config.MaxTokens = defaults.MaxTokens
	}
	if config.Now == nil {
		config.Now = defaults.Now
	}
	return &ScopedMemoryProvider{retriever: retriever, authorizer: authorizer, config: config}
}

func (p *ScopedMemoryProvider) Snapshot(ctx context.Context, request MemorySnapshotRequest) (MemorySnapshot, error) {
	if p == nil || p.retriever == nil || p.authorizer == nil {
		return MemorySnapshot{}, errors.New("memory provider 未配置")
	}
	if request.UserID <= 0 {
		return MemorySnapshot{}, errors.New("memory user_id 必须大于 0")
	}
	scopes, err := normalizeMemoryScopes(request.Scopes)
	if err != nil {
		return MemorySnapshot{}, err
	}
	for _, scope := range scopes {
		if err := p.authorizer.Authorize(ctx, request.UserID, scope, false); err != nil {
			return MemorySnapshot{}, err
		}
	}
	topK := boundedPositive(request.TopK, p.config.TopK)
	maxChars := boundedPositive(request.MaxChars, p.config.MaxChars)
	maxTokens := boundedPositive(request.MaxTokens, p.config.MaxTokens)
	now := p.config.Now().UTC()
	items, err := p.retriever.Retrieve(ctx, MemoryRetrieveRequest{
		UserID: request.UserID, Query: strings.TrimSpace(request.Query), Scopes: scopes, Limit: topK * 4, Now: now,
	})
	if err != nil {
		return MemorySnapshot{}, err
	}
	items = filterAndSortMemoryItems(items, request.UserID, scopes, now)
	snapshot := buildMemorySnapshot(items, scopes, topK, maxChars, maxTokens)
	return snapshot, nil
}

func boundedPositive(requested, configured int) int {
	if configured <= 0 {
		return requested
	}
	if requested <= 0 || requested > configured {
		return configured
	}
	return requested
}

func normalizeMemoryScopes(scopes []MemoryScope) ([]MemoryScope, error) {
	result := make([]MemoryScope, 0, len(scopes))
	seen := make(map[string]struct{}, len(scopes))
	for _, scope := range scopes {
		scope.Type, scope.ID = strings.TrimSpace(scope.Type), strings.TrimSpace(scope.ID)
		if !validMemoryScopeType(scope.Type) || scope.ID == "" {
			return nil, fmt.Errorf("memory scope 无效: %s/%s", scope.Type, scope.ID)
		}
		key := scope.Type + "\x00" + scope.ID
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, scope)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Type == result[j].Type {
			return result[i].ID < result[j].ID
		}
		return result[i].Type < result[j].Type
	})
	return result, nil
}

func validMemoryScopeType(scopeType string) bool {
	switch scopeType {
	case model.MemoryScopeUser, model.MemoryScopeVideo, model.MemoryScopeKnowledgeBase, model.MemoryScopeRun:
		return true
	default:
		return false
	}
}

func filterAndSortMemoryItems(items []model.AgentMemoryItem, userID int64, scopes []MemoryScope, now time.Time) []model.AgentMemoryItem {
	allowed := make(map[string]struct{}, len(scopes))
	for _, scope := range scopes {
		allowed[scope.Type+"\x00"+scope.ID] = struct{}{}
	}
	filtered := make([]model.AgentMemoryItem, 0, len(items))
	for _, item := range items {
		if item.UserID != userID || strings.TrimSpace(item.SourceRef) == "" || item.DeletedAt.Valid {
			continue
		}
		if item.Status != model.MemoryStatusActive && item.Status != model.MemoryStatusConflicted {
			continue
		}
		if item.ExpiresAt != nil && !item.ExpiresAt.After(now) {
			continue
		}
		if _, ok := allowed[item.ScopeType+"\x00"+item.ScopeID]; !ok {
			continue
		}
		filtered = append(filtered, item)
	}
	sort.SliceStable(filtered, func(i, j int) bool {
		left, right := filtered[i], filtered[j]
		if memoryScopePriority(left.ScopeType) != memoryScopePriority(right.ScopeType) {
			return memoryScopePriority(left.ScopeType) < memoryScopePriority(right.ScopeType)
		}
		if left.Importance != right.Importance {
			return left.Importance > right.Importance
		}
		if !left.CreatedAt.Equal(right.CreatedAt) {
			return left.CreatedAt.After(right.CreatedAt)
		}
		return left.ID < right.ID
	})
	return filtered
}

func memoryScopePriority(scopeType string) int {
	switch scopeType {
	case model.MemoryScopeRun:
		return 0
	case model.MemoryScopeVideo, model.MemoryScopeKnowledgeBase:
		return 1
	case model.MemoryScopeUser:
		return 2
	default:
		return 3
	}
}

func buildMemorySnapshot(items []model.AgentMemoryItem, scopes []MemoryScope, topK, maxChars, maxTokens int) MemorySnapshot {
	conflictGroups := groupMemoryConflicts(items)
	selected := make([]MemorySnapshotItem, 0, topK)
	used := make(map[string]struct{})
	usedChars, usedTokens := 0, 0
	for _, item := range items {
		if len(selected) >= topK {
			break
		}
		if _, ok := used[item.ID]; ok {
			continue
		}
		groupKey := memoryConflictKey(item)
		group := conflictGroups[groupKey]
		if len(group) > 1 {
			if len(selected)+len(group) > topK || !memoryGroupFits(group, usedChars, usedTokens, maxChars, maxTokens) {
				for _, member := range group {
					used[member.ID] = struct{}{}
				}
				continue
			}
			for _, member := range group {
				entry := memorySnapshotItem(member, true, member.Content, false)
				selected = append(selected, entry)
				used[member.ID] = struct{}{}
				chars := utf8.RuneCountInString(entry.Content)
				usedChars += chars
				usedTokens += approximateMemoryTokens(chars)
			}
			continue
		}
		content, truncated, ok := fitMemoryContent(item.Content, usedChars, usedTokens, maxChars, maxTokens)
		if !ok {
			continue
		}
		entry := memorySnapshotItem(item, false, content, truncated)
		selected = append(selected, entry)
		used[item.ID] = struct{}{}
		chars := utf8.RuneCountInString(entry.Content)
		usedChars += chars
		usedTokens += approximateMemoryTokens(chars)
	}

	conflicts := make([]MemoryConflict, 0)
	selectedByID := make(map[string]struct{}, len(selected))
	ids := make([]string, 0, len(selected))
	for _, item := range selected {
		selectedByID[item.ID] = struct{}{}
		ids = append(ids, item.ID)
	}
	for _, group := range conflictGroups {
		if len(group) < 2 {
			continue
		}
		allSelected := true
		groupIDs := make([]string, 0, len(group))
		for _, item := range group {
			if _, ok := selectedByID[item.ID]; !ok {
				allSelected = false
				break
			}
			groupIDs = append(groupIDs, item.ID)
		}
		if allSelected {
			sort.Strings(groupIDs)
			conflicts = append(conflicts, MemoryConflict{ScopeType: group[0].ScopeType, ScopeID: group[0].ScopeID, Kind: group[0].Kind, MemoryIDs: groupIDs})
		}
	}
	sort.Slice(conflicts, func(i, j int) bool {
		left, right := conflicts[i], conflicts[j]
		return left.ScopeType+left.ScopeID+left.Kind < right.ScopeType+right.ScopeID+right.Kind
	})
	snapshot := MemorySnapshot{
		SchemaVersion: MemorySnapshotSchemaVersion,
		MemoryIDs:     ids,
		Items:         selected,
		Conflicts:     conflicts,
		Budget:        MemorySnapshotBudget{TopK: topK, MaxChars: maxChars, MaxTokens: maxTokens, UsedChars: usedChars, UsedTokens: usedTokens},
	}
	snapshot.Version = stableMemorySnapshotVersion(scopes, selected)
	return snapshot
}

func groupMemoryConflicts(items []model.AgentMemoryItem) map[string][]model.AgentMemoryItem {
	grouped := make(map[string][]model.AgentMemoryItem)
	contents := make(map[string]map[string]struct{})
	for _, item := range items {
		key := memoryConflictKey(item)
		grouped[key] = append(grouped[key], item)
		if contents[key] == nil {
			contents[key] = make(map[string]struct{})
		}
		contents[key][strings.ToLower(strings.TrimSpace(item.Content))] = struct{}{}
	}
	for key := range grouped {
		if len(contents[key]) < 2 {
			delete(grouped, key)
		}
	}
	return grouped
}

func memoryConflictKey(item model.AgentMemoryItem) string {
	return item.ScopeType + "\x00" + item.ScopeID + "\x00" + item.Kind
}

func memoryGroupFits(group []model.AgentMemoryItem, usedChars, usedTokens, maxChars, maxTokens int) bool {
	for _, item := range group {
		chars := utf8.RuneCountInString(strings.TrimSpace(item.Content))
		usedChars += chars
		usedTokens += approximateMemoryTokens(chars)
	}
	return usedChars <= maxChars && usedTokens <= maxTokens
}

func fitMemoryContent(content string, usedChars, usedTokens, maxChars, maxTokens int) (string, bool, bool) {
	content = strings.TrimSpace(content)
	remainingChars := maxChars - usedChars
	remainingTokenChars := (maxTokens - usedTokens) * 4
	if remainingTokenChars < remainingChars {
		remainingChars = remainingTokenChars
	}
	if remainingChars <= 0 {
		return "", false, false
	}
	runes := []rune(content)
	if len(runes) <= remainingChars {
		return content, false, true
	}
	return strings.TrimSpace(string(runes[:remainingChars])), true, true
}

func approximateMemoryTokens(chars int) int {
	if chars <= 0 {
		return 0
	}
	return (chars + 3) / 4
}

func memorySnapshotItem(item model.AgentMemoryItem, conflict bool, content string, truncated bool) MemorySnapshotItem {
	return MemorySnapshotItem{
		ID: item.ID, ScopeType: item.ScopeType, ScopeID: item.ScopeID, Kind: item.Kind,
		Content: content, SourceType: item.SourceType, SourceRef: item.SourceRef,
		Importance: item.Importance, EmbeddingRef: item.EmbeddingRef, Status: item.Status,
		Version: item.Version, CreatedAt: item.CreatedAt.UTC(), Conflict: conflict, Truncated: truncated,
	}
}

func stableMemorySnapshotVersion(scopes []MemoryScope, items []MemorySnapshotItem) string {
	hash := sha256.New()
	_, _ = hash.Write([]byte(MemorySnapshotSchemaVersion))
	for _, scope := range scopes {
		_, _ = hash.Write([]byte("\nS:" + scope.Type + ":" + scope.ID))
	}
	for _, item := range items {
		_, _ = hash.Write([]byte(fmt.Sprintf("\nM:%s:%d:%t:%s", item.ID, item.Version, item.Truncated, item.Content)))
	}
	return MemorySnapshotSchemaVersion + ":" + hex.EncodeToString(hash.Sum(nil))
}

func (s MemorySnapshot) PromptContext() string {
	if len(s.Items) == 0 {
		return ""
	}
	var builder strings.Builder
	builder.WriteString("历史记忆仅用于理解用户偏好、术语和检索提示；它不是当前视频事实证据，当前视频引用片段始终优先。\n")
	for _, item := range s.Items {
		fmt.Fprintf(&builder, "- [memory:%s scope=%s/%s kind=%s source=%s] %s", item.ID, item.ScopeType, item.ScopeID, item.Kind, item.SourceRef, item.Content)
		if item.Conflict {
			builder.WriteString("（与同范围记忆冲突，需保留不确定性）")
		}
		builder.WriteByte('\n')
	}
	return strings.TrimSpace(builder.String())
}

type RepositoryMemoryRetriever struct{ repository *repository.MemoryRepository }

func NewRepositoryMemoryRetriever(memory *repository.MemoryRepository) *RepositoryMemoryRetriever {
	return &RepositoryMemoryRetriever{repository: memory}
}

func (r *RepositoryMemoryRetriever) Retrieve(ctx context.Context, request MemoryRetrieveRequest) ([]model.AgentMemoryItem, error) {
	if r == nil || r.repository == nil {
		return nil, errors.New("memory repository 未配置")
	}
	scopes := make(map[string][]string)
	for _, scope := range request.Scopes {
		scopes[scope.Type] = append(scopes[scope.Type], scope.ID)
	}
	return r.repository.ListRecallable(ctx, request.UserID, scopes, request.Limit, request.Now)
}

type RepositoryMemoryAuthorizer struct{ repos *repository.Repositories }

func NewRepositoryMemoryAuthorizer(repos *repository.Repositories) *RepositoryMemoryAuthorizer {
	return &RepositoryMemoryAuthorizer{repos: repos}
}

func (a *RepositoryMemoryAuthorizer) Authorize(_ context.Context, userID int64, scope MemoryScope, write bool) error {
	if a == nil || a.repos == nil || userID <= 0 {
		return errors.New("memory authorizer 未配置")
	}
	if write && a.repos.User != nil {
		user, err := a.repos.User.FindByID(userID)
		if err != nil {
			return err
		}
		if user.Role == model.RoleDemo {
			return errors.New("演示账号不能写入长期记忆")
		}
	}
	switch scope.Type {
	case model.MemoryScopeUser:
		if scope.ID != strconv.FormatInt(userID, 10) {
			return errors.New("无权访问此用户记忆")
		}
	case model.MemoryScopeVideo:
		id, err := strconv.ParseInt(scope.ID, 10, 64)
		if err != nil || id <= 0 || a.repos.Task == nil {
			return errors.New("video memory scope 无效")
		}
		tasks, err := a.repos.Task.ListByIDsForUser(userID, []int64{id})
		if err != nil {
			return err
		}
		if len(tasks) != 1 {
			return errors.New("无权访问此视频记忆")
		}
	case model.MemoryScopeKnowledgeBase:
		id, err := strconv.ParseInt(scope.ID, 10, 64)
		if err != nil || id <= 0 || a.repos.KnowledgeBase == nil {
			return errors.New("knowledge_base memory scope 无效")
		}
		kb, err := a.repos.KnowledgeBase.FindByIDForUser(userID, id)
		if err != nil {
			return err
		}
		if kb == nil {
			return errors.New("无权访问此知识库记忆")
		}
	case model.MemoryScopeRun:
		if strings.TrimSpace(scope.ID) == "" {
			return errors.New("run memory scope 无效")
		}
	default:
		return errors.New("不支持的 memory scope")
	}
	return nil
}

type MemoryCandidate struct {
	UserID       int64
	Scope        MemoryScope
	Kind         string
	Content      string
	SourceType   string
	SourceRef    string
	Importance   float64
	ExpiresAt    *time.Time
	EmbeddingRef string
}

type MemoryEnqueueResult struct {
	Accepted bool   `json:"accepted"`
	Reason   string `json:"reason,omitempty"`
}

type MemoryWriter interface {
	Enqueue(candidate MemoryCandidate) MemoryEnqueueResult
}

type memoryWriteStore interface {
	Append(ctx context.Context, item *model.AgentMemoryItem) (repository.MemoryAppendResult, error)
	SetEmbeddingRef(ctx context.Context, userID int64, memoryID, ref string) error
}

type MemoryProjector interface {
	Project(ctx context.Context, item model.AgentMemoryItem) (string, error)
}

type AsyncMemoryWriter struct {
	store      memoryWriteStore
	authorizer MemoryScopeAuthorizer
	projector  MemoryProjector
	queue      chan MemoryCandidate
	cancel     context.CancelFunc
	done       chan struct{}
	accepted   atomic.Uint64
	failed     atomic.Uint64
	dropped    atomic.Uint64
	lastError  atomic.Value
	closeOnce  sync.Once
}

func NewAsyncMemoryWriter(store memoryWriteStore, authorizer MemoryScopeAuthorizer, projector MemoryProjector, queueSize int) *AsyncMemoryWriter {
	if queueSize <= 0 {
		queueSize = 128
	}
	ctx, cancel := context.WithCancel(context.Background())
	w := &AsyncMemoryWriter{store: store, authorizer: authorizer, projector: projector, queue: make(chan MemoryCandidate, queueSize), cancel: cancel, done: make(chan struct{})}
	go w.run(ctx)
	return w
}

func (w *AsyncMemoryWriter) Enqueue(candidate MemoryCandidate) MemoryEnqueueResult {
	if err := validateMemoryCandidate(candidate); err != nil {
		return MemoryEnqueueResult{Reason: err.Error()}
	}
	if w == nil || w.store == nil || w.authorizer == nil {
		return MemoryEnqueueResult{Reason: "memory writer 未配置"}
	}
	select {
	case w.queue <- candidate:
		w.accepted.Add(1)
		return MemoryEnqueueResult{Accepted: true}
	default:
		w.dropped.Add(1)
		return MemoryEnqueueResult{Reason: "memory writer queue full"}
	}
}

func (w *AsyncMemoryWriter) run(ctx context.Context) {
	defer close(w.done)
	for {
		select {
		case <-ctx.Done():
			return
		case candidate := <-w.queue:
			if err := w.write(ctx, candidate); err != nil {
				w.failed.Add(1)
				w.lastError.Store(err.Error())
			}
		}
	}
}

func (w *AsyncMemoryWriter) write(ctx context.Context, candidate MemoryCandidate) error {
	if err := w.authorizer.Authorize(ctx, candidate.UserID, candidate.Scope, true); err != nil {
		return err
	}
	item := &model.AgentMemoryItem{
		UserID: candidate.UserID, ScopeType: candidate.Scope.Type, ScopeID: candidate.Scope.ID,
		Kind: strings.TrimSpace(candidate.Kind), Content: strings.TrimSpace(candidate.Content),
		SourceType: strings.TrimSpace(candidate.SourceType), SourceRef: strings.TrimSpace(candidate.SourceRef),
		Importance: candidate.Importance, EmbeddingRef: strings.TrimSpace(candidate.EmbeddingRef),
		Status: model.MemoryStatusActive, Version: 1, ExpiresAt: candidate.ExpiresAt,
	}
	result, err := w.store.Append(ctx, item)
	if err != nil {
		return err
	}
	if w.projector == nil || strings.TrimSpace(result.Item.EmbeddingRef) != "" {
		return nil
	}
	ref, err := w.projector.Project(ctx, result.Item)
	if err != nil {
		return err
	}
	if strings.TrimSpace(ref) == "" {
		return nil
	}
	return w.store.SetEmbeddingRef(ctx, result.Item.UserID, result.Item.ID, ref)
}

func validateMemoryCandidate(candidate MemoryCandidate) error {
	candidate.Kind = strings.TrimSpace(candidate.Kind)
	candidate.Content = strings.TrimSpace(candidate.Content)
	candidate.SourceType = strings.TrimSpace(candidate.SourceType)
	candidate.SourceRef = strings.TrimSpace(candidate.SourceRef)
	if candidate.UserID <= 0 || !validMemoryScopeType(candidate.Scope.Type) || strings.TrimSpace(candidate.Scope.ID) == "" {
		return errors.New("memory candidate scope 无效")
	}
	if candidate.Kind == "" || candidate.Content == "" || candidate.SourceRef == "" || candidate.SourceType == "" {
		return errors.New("memory candidate 必须包含 kind、content、source_type 和 source_ref")
	}
	if candidate.Importance < 0 || candidate.Importance > 1 {
		return errors.New("memory importance 必须在 0..1")
	}
	if candidate.SourceType == "assistant_response" || candidate.SourceType == "agent_answer" {
		return errors.New("未经验证的 Agent 回答不能写入长期记忆")
	}
	if candidate.Scope.Type == model.MemoryScopeVideo || candidate.Scope.Type == model.MemoryScopeKnowledgeBase {
		switch candidate.SourceType {
		case "verified_claim", "user_confirmation", "manual":
		default:
			return errors.New("video/knowledge_base memory 仅接受 verified_claim、user_confirmation 或 manual 来源")
		}
	}
	return nil
}

func (w *AsyncMemoryWriter) Stats() (accepted, failed, dropped uint64, lastError string) {
	if w == nil {
		return 0, 0, 0, ""
	}
	if value := w.lastError.Load(); value != nil {
		lastError, _ = value.(string)
	}
	return w.accepted.Load(), w.failed.Load(), w.dropped.Load(), lastError
}

func (w *AsyncMemoryWriter) Close(ctx context.Context) error {
	if w == nil {
		return nil
	}
	w.closeOnce.Do(w.cancel)
	select {
	case <-w.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

type MemoryEmbedding struct {
	Model  string
	Vector []float32
}

type MemoryEmbedder interface {
	EmbedMemory(ctx context.Context, userID int64, content string) (MemoryEmbedding, error)
}

type MemoryEmbedderFunc func(ctx context.Context, userID int64, content string) (MemoryEmbedding, error)

func (f MemoryEmbedderFunc) EmbedMemory(ctx context.Context, userID int64, content string) (MemoryEmbedding, error) {
	return f(ctx, userID, content)
}

type MemoryEmbeddingStore interface {
	UpsertEmbedding(ctx context.Context, item model.AgentMemoryItem, modelName string, vector []float32) (string, error)
}

type RepositoryMemoryProjector struct {
	embedder MemoryEmbedder
	store    MemoryEmbeddingStore
}

func NewRepositoryMemoryProjector(embedder MemoryEmbedder, store MemoryEmbeddingStore) *RepositoryMemoryProjector {
	return &RepositoryMemoryProjector{embedder: embedder, store: store}
}

func (p *RepositoryMemoryProjector) Project(ctx context.Context, item model.AgentMemoryItem) (string, error) {
	if p == nil || p.embedder == nil || p.store == nil {
		return "", errors.New("memory embedding projector 未配置")
	}
	embedding, err := p.embedder.EmbedMemory(ctx, item.UserID, item.Content)
	if err != nil {
		return "", err
	}
	return p.store.UpsertEmbedding(ctx, item, embedding.Model, embedding.Vector)
}

type MemoryExtractionRequest struct {
	UserID    int64
	UserText  string
	SourceRef string
}

type MemoryExtractor interface {
	Extract(ctx context.Context, request MemoryExtractionRequest) ([]MemoryCandidate, error)
}

type MemoryCapture interface {
	EnqueueExtraction(request MemoryExtractionRequest) MemoryEnqueueResult
}

type AsyncMemoryCapture struct {
	extractor MemoryExtractor
	writer    MemoryWriter
	queue     chan MemoryExtractionRequest
	cancel    context.CancelFunc
	done      chan struct{}
	closeOnce sync.Once
}

func NewAsyncMemoryCapture(extractor MemoryExtractor, writer MemoryWriter, queueSize int) *AsyncMemoryCapture {
	if queueSize <= 0 {
		queueSize = 128
	}
	ctx, cancel := context.WithCancel(context.Background())
	c := &AsyncMemoryCapture{extractor: extractor, writer: writer, queue: make(chan MemoryExtractionRequest, queueSize), cancel: cancel, done: make(chan struct{})}
	go c.run(ctx)
	return c
}

func (c *AsyncMemoryCapture) EnqueueExtraction(request MemoryExtractionRequest) MemoryEnqueueResult {
	if c == nil || c.extractor == nil || c.writer == nil || request.UserID <= 0 || strings.TrimSpace(request.SourceRef) == "" {
		return MemoryEnqueueResult{Reason: "memory extraction request 无效"}
	}
	select {
	case c.queue <- request:
		return MemoryEnqueueResult{Accepted: true}
	default:
		return MemoryEnqueueResult{Reason: "memory extractor queue full"}
	}
}

func (c *AsyncMemoryCapture) run(ctx context.Context) {
	defer close(c.done)
	for {
		select {
		case <-ctx.Done():
			return
		case request := <-c.queue:
			candidates, err := c.extractor.Extract(ctx, request)
			if err != nil {
				continue
			}
			for _, candidate := range candidates {
				_ = c.writer.Enqueue(candidate)
			}
		}
	}
}

func (c *AsyncMemoryCapture) Close(ctx context.Context) error {
	if c == nil {
		return nil
	}
	c.closeOnce.Do(c.cancel)
	select {
	case <-c.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// ExplicitPreferenceExtractor is deliberately conservative: it only captures
// explicit first-party response preferences from the user message and never
// reads the Agent answer or creates video/KB memories.
type ExplicitPreferenceExtractor struct{}

func (ExplicitPreferenceExtractor) Extract(_ context.Context, request MemoryExtractionRequest) ([]MemoryCandidate, error) {
	text := strings.TrimSpace(request.UserText)
	if text == "" {
		return nil, nil
	}
	lower := strings.ToLower(text)
	hints := []string{"请以后", "以后请", "我喜欢", "我偏好", "请用中文", "请用英文", "prefer", "always answer"}
	matched := false
	for _, hint := range hints {
		if strings.Contains(lower, strings.ToLower(hint)) {
			matched = true
			break
		}
	}
	if !matched {
		return nil, nil
	}
	if utf8.RuneCountInString(text) > 500 {
		text = string([]rune(text)[:500])
	}
	return []MemoryCandidate{{
		UserID: request.UserID,
		Scope:  MemoryScope{Type: model.MemoryScopeUser, ID: strconv.FormatInt(request.UserID, 10)},
		Kind:   "response_preference", Content: text, SourceType: "user_message", SourceRef: strings.TrimSpace(request.SourceRef), Importance: 0.7,
	}}, nil
}
