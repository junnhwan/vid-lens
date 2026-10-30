package store

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"vidlens/internal/config"
	"vidlens/internal/domain"
)

type ConversationSummary struct {
	domain.Conversation
	Title     string    `json:"title"`
	UpdatedAt time.Time `json:"updated_at"`
	TurnCount int       `json:"turn_count"`
}

type ConversationPage struct {
	SchemaVersion int                   `json:"schema_version"`
	Items         []ConversationSummary `json:"items"`
	HasMore       bool                  `json:"has_more"`
	NextBefore    *string               `json:"next_before"`
}

func conversationAccess(ctx context.Context, q Querier, session, request string) (domain.Request, error) {
	r, err := loadRequest(ctx, q, request)
	if err != nil {
		return r, NotFound(err)
	}
	if r.SessionID != session {
		return r, domain.Err(404, "RESOURCE_NOT_FOUND", "资源不存在。")
	}
	if r.Status != "completed" || r.ArticleID == nil || r.SnapshotID == nil {
		return r, domain.Err(409, "ARTICLE_NOT_READY", "文章尚未发布。")
	}
	if r.Reuse == "example_question" {
		var shown bool
		if err = q.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM example_showcases WHERE article_id=$1)", *r.ArticleID).Scan(&shown); err != nil {
			return r, err
		}
		if !shown {
			return r, domain.Err(404, "RESOURCE_NOT_FOUND", "这篇示例没有公开。")
		}
	}
	return r, nil
}

// Creating a thread has its own idempotent write. No question or model work is
// created, and every thread remains bound to its owner's immutable video.
func (s *Store) CreateConversation(ctx context.Context, session, request, key string) (Accepted, error) {
	var accepted Accepted
	err := s.Tx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		r, err := conversationAccess(ctx, tx, session, request)
		if err != nil {
			return err
		}
		scope := "conversation:" + session + ":" + request
		var found bool
		if accepted, found, err = replay(ctx, tx, scope, key, Hash(struct{}{})); err != nil || found {
			return err
		}
		id := domain.ID()
		if _, err = tx.Exec(ctx, "INSERT INTO qa_conversations(id,session_id,request_id,article_id,snapshot_id) VALUES($1,$2,$3,$4,$5)", id, session, request, *r.ArticleID, *r.SnapshotID); err != nil {
			return err
		}
		p, err := one[config.Profile](ctx, tx, "SELECT profile FROM runtime_config_revisions WHERE id=$1", r.ConfigID)
		if err != nil {
			return err
		}
		accepted, err = remember(ctx, tx, scope, key, Hash(struct{}{}), id, p.IdempotencySeconds)
		return err
	})
	return accepted, err
}

// Creation order is stable while old threads receive new replies. The cursor is
// an owned conversation identity, never a cross-visitor list offset.
func (s *Store) Conversations(ctx context.Context, session, request, before string, limit int) (ConversationPage, error) {
	page := ConversationPage{SchemaVersion: 1, Items: []ConversationSummary{}}
	limit = max(1, min(100, limit))
	err := s.read(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if _, err := conversationAccess(ctx, tx, session, request); err != nil {
			return err
		}
		var cursor *domain.Conversation
		if before != "" {
			c, err := one[domain.Conversation](ctx, tx, "SELECT to_jsonb(c) FROM qa_conversations c WHERE id=$1 AND session_id=$2 AND request_id=$3", before, session, request)
			if err != nil {
				return NotFound(err)
			}
			cursor = &c
		}
		var at *time.Time
		if cursor != nil {
			at = &cursor.CreatedAt
		}
		items, err := many[ConversationSummary](ctx, tx, `SELECT to_jsonb(c)-'session_id'-'next_turn_no' || jsonb_build_object(
          'title',COALESCE((SELECT left(question,48) FROM qa_turns WHERE conversation_id=c.id AND (expires_at IS NULL OR expires_at>now()) ORDER BY turn_no LIMIT 1),'新会话'),
          'updated_at',COALESCE(t.updated_at,c.created_at),
          'turn_count',(SELECT count(*) FROM qa_turns WHERE conversation_id=c.id AND (expires_at IS NULL OR expires_at>now())))
          FROM qa_conversations c LEFT JOIN qa_turns t ON t.id=c.last_turn_id
          WHERE c.session_id=$1 AND c.request_id=$2 AND ($3::timestamptz IS NULL OR (c.created_at,c.id)<($3,$4))
          ORDER BY c.created_at DESC,c.id DESC LIMIT $5`, session, request, at, before, limit+1)
		if err != nil {
			return err
		}
		if len(items) > limit {
			page.HasMore = true
			items = items[:limit]
			page.NextBefore = &items[len(items)-1].ID
		}
		for i := range items {
			items[i].SessionID = ""
			items[i].NextTurnNo = 0
		}
		page.Items = items
		return nil
	})
	return page, err
}
