package store

import (
	"context"
	"errors"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"vidlens/internal/domain"
)

const maxExampleShowcases = 24

const exampleTitleSQL = `COALESCE(NULLIF(a.content->>'title', ''), NULLIF(a.content#>>'{source,title}', ''), '未命名视频')`
const exampleDurationSQL = `CASE WHEN COALESCE(a.content#>>'{source,duration_ms}', '') ~ '^[0-9]+$' THEN (a.content#>>'{source,duration_ms}')::bigint ELSE NULL END`

var exampleArticleID = regexp.MustCompile(`^[a-f0-9]{32}$`)

type AdminExample struct {
	ArticleID   string    `json:"article_id"`
	Title       string    `json:"title"`
	Platform    string    `json:"platform"`
	URL         string    `json:"url"`
	DurationMS  *int64    `json:"duration_ms"`
	PublishedAt time.Time `json:"published_at"`
	Shown       bool      `json:"shown"`
	Position    int       `json:"position"`
}

type ExampleCard struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Platform    string `json:"platform"`
	DurationMS  *int64 `json:"duration_ms"`
	Cover       string `json:"cover"`
}

type PublicExampleQuestion struct {
	ID             string        `json:"id"`
	Label          string        `json:"label"`
	Question       string        `json:"question"`
	ContextFrameID *string       `json:"context_frame_id"`
	Answer         domain.Answer `json:"answer"`
	Steps          []struct{}    `json:"steps"`
	RecordedAt     time.Time     `json:"recorded_at"`
	DurationMS     int64         `json:"duration_ms"`
}

type PublicExample struct {
	ID              string                  `json:"id"`
	SchemaVersion   int                     `json:"schema_version"`
	DetailLevel     string                  `json:"detail_level,omitempty"`
	Article         domain.Article          `json:"article"`
	Frames          []domain.Frame          `json:"frames"`
	Media           publicExampleMedia      `json:"media"`
	GenerationModel *domain.GenerationModel `json:"generation_model,omitempty"`
	Purpose         string                  `json:"purpose,omitempty"`
	QAAvailable     bool                    `json:"qa_available"`
	GenerationSteps []struct{}              `json:"generation_steps"`
	Questions       []PublicExampleQuestion `json:"questions"`
}

type publicExampleMedia struct {
	Status                string `json:"status"`
	CanInspectNow         bool   `json:"can_inspect_now"`
	CanRestoreForQuestion bool   `json:"can_restore_for_question"`
}

type showcaseArticle struct {
	Content    domain.Article          `json:"content"`
	Model      *domain.GenerationModel `json:"generation_model"`
	SnapshotID string                  `json:"snapshot_id"`
	Purpose    string                  `json:"purpose"`
}

type showcaseTurn struct {
	ID        string                 `json:"id"`
	Question  string                 `json:"question"`
	Context   domain.QuestionContext `json:"context"`
	Answer    *domain.Answer         `json:"answer"`
	CreatedAt time.Time              `json:"created_at"`
	UpdatedAt time.Time              `json:"updated_at"`
}

func (s *Store) AdminExamples(ctx context.Context) ([]AdminExample, error) {
	var items []AdminExample
	e := s.read(ctx, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := many[AdminExample](ctx, tx, `
			SELECT jsonb_build_object(
				'article_id', a.id,
				'title', `+exampleTitleSQL+`,
				'platform', COALESCE(a.content#>>'{source,platform}', ''),
				'url', COALESCE(a.content#>>'{source,canonical_url}', ''),
				'duration_ms', `+exampleDurationSQL+`,
				'published_at', a.published_at,
				'shown', es.article_id IS NOT NULL,
				'position', COALESCE(es.position, 0)
			)
			FROM articles a
			LEFT JOIN example_showcases es ON es.article_id = a.id
			ORDER BY CASE WHEN es.article_id IS NULL THEN 1 ELSE 0 END, es.position, a.published_at DESC`)
		items = rows
		return err
	})
	if items == nil {
		items = []AdminExample{}
	}
	return items, e
}

func (s *Store) ReplaceExampleShowcases(ctx context.Context, ids []string) error {
	if ids == nil {
		ids = []string{}
	}
	if len(ids) > maxExampleShowcases {
		return domain.Err(400, "INVALID_ARGUMENT", "公开示例最多 24 篇。")
	}
	seen := map[string]struct{}{}
	for _, id := range ids {
		if !exampleArticleID.MatchString(id) {
			return domain.Err(400, "INVALID_ARGUMENT", "示例选择无效。")
		}
		if _, ok := seen[id]; ok {
			return domain.Err(400, "INVALID_ARGUMENT", "同一篇图文只能选择一次。")
		}
		seen[id] = struct{}{}
	}
	return s.Tx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var n int
		if e := tx.QueryRow(ctx, "SELECT count(*) FROM articles WHERE id = ANY($1::text[])", ids).Scan(&n); e != nil {
			return e
		}
		if n != len(ids) {
			return domain.Err(404, "RESOURCE_NOT_FOUND", "有一篇图文已经不存在，请刷新后再保存。")
		}
		if _, e := tx.Exec(ctx, "DELETE FROM example_showcases"); e != nil {
			return e
		}
		for i, id := range ids {
			if _, e := tx.Exec(ctx, "INSERT INTO example_showcases(article_id, position) VALUES($1, $2)", id, i+1); e != nil {
				return e
			}
		}
		return nil
	})
}

func (s *Store) PublicExamples(ctx context.Context) ([]ExampleCard, error) {
	var cards []ExampleCard
	e := s.read(ctx, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := many[struct {
			ID           string         `json:"id"`
			Title        string         `json:"title"`
			Platform     string         `json:"platform"`
			DurationMS   *int64         `json:"duration_ms"`
			SectionTitle string         `json:"section_title"`
			SnapshotID   string         `json:"snapshot_id"`
			Content      domain.Article `json:"content"`
		}](ctx, tx, `
			SELECT jsonb_build_object(
				'id', a.id,
				'title', `+exampleTitleSQL+`,
				'platform', COALESCE(a.content#>>'{source,platform}', ''),
				'duration_ms', `+exampleDurationSQL+`,
				'section_title', COALESCE(a.content#>>'{sections,0,title}', ''),
				'snapshot_id', a.snapshot_id,
				'content', a.content
			)
			FROM example_showcases es
			JOIN articles a ON a.id = es.article_id
			ORDER BY es.position`)
		if err != nil {
			return err
		}
		cards = make([]ExampleCard, 0, len(rows))
		for _, row := range rows {
			cover := ""
			frameID, err := firstReadyFrame(ctx, tx, row.SnapshotID, ContentFrameIDs(row.Content.Sections, row.Content.Citations))
			if err != nil {
				return err
			}
			if frameID != "" {
				cover = exampleFrameURL(row.ID, frameID)
			}
			description := strings.TrimSpace(row.SectionTitle)
			if description == "" {
				description = "已发布的图文"
			}
			cards = append(cards, ExampleCard{
				ID: row.ID, Title: row.Title, Description: clipRunes(description, 72),
				Platform: row.Platform, DurationMS: row.DurationMS, Cover: cover,
			})
		}
		return nil
	})
	if cards == nil {
		cards = []ExampleCard{}
	}
	return cards, e
}

func (s *Store) PublicExample(ctx context.Context, id string) (PublicExample, error) {
	var out PublicExample
	if !exampleArticleID.MatchString(id) {
		return out, domain.Err(404, "RESOURCE_NOT_FOUND", "这篇示例没有公开。")
	}
	e := s.read(ctx, func(ctx context.Context, tx pgx.Tx) error {
		row, turns, err := loadShowcase(ctx, tx, id)
		if err != nil {
			return err
		}
		frames, byID, err := showcaseFrames(ctx, tx, row, turns, id)
		if err != nil {
			return err
		}
		article := scrubArticle(row.Content)
		if article.Sections == nil {
			article.Sections = []domain.Section{}
		}
		if article.Citations == nil {
			article.Citations = map[string]domain.Citation{}
		}
		if article.Limitations == nil {
			article.Limitations = []domain.Limitation{}
		}
		questions := make([]PublicExampleQuestion, 0, len(turns))
		for _, turn := range turns {
			if strings.TrimSpace(turn.Question) == "" || turn.Answer == nil {
				continue
			}
			answer := projectAnswer(*turn.Answer, byID)
			var contextFrame *string
			label := "文字追问"
			if turn.Context.FrameID != nil {
				if frame, ok := byID[*turn.Context.FrameID]; ok {
					contextFrame = domain.Ptr(frame.ID)
					label = "看图追问"
				}
			}
			duration := turn.UpdatedAt.Sub(turn.CreatedAt).Milliseconds()
			if duration < 0 {
				duration = 0
			}
			questions = append(questions, PublicExampleQuestion{
				ID: turn.ID, Label: label, Question: turn.Question, ContextFrameID: contextFrame,
				Answer: answer, Steps: []struct{}{}, RecordedAt: turn.CreatedAt, DurationMS: duration,
			})
		}
		status := "unavailable"
		if len(frames) > 0 {
			status = "ready"
		}
		model := row.Model
		if model != nil {
			copy := *model
			copy.RevisionID = ""
			copy.Description = ""
			model = &copy
		}
		out = PublicExample{
			ID: id, SchemaVersion: 1, DetailLevel: article.DetailLevel, Article: article, Frames: frames,
			Media:           publicExampleMedia{Status: status, CanInspectNow: len(frames) > 0},
			GenerationModel: model, Purpose: strings.TrimSpace(row.Purpose), QAAvailable: false,
			GenerationSteps: []struct{}{}, Questions: questions,
		}
		return nil
	})
	return out, e
}

func (s *Store) PublicExampleFrame(ctx context.Context, articleID, frameID string) (domain.Asset, error) {
	var asset domain.Asset
	if !exampleArticleID.MatchString(articleID) || !exampleArticleID.MatchString(frameID) {
		return asset, domain.Err(404, "RESOURCE_NOT_FOUND", "这张图片没有公开。")
	}
	e := s.read(ctx, func(ctx context.Context, tx pgx.Tx) error {
		row, turns, err := loadShowcase(ctx, tx, articleID)
		if err != nil {
			return err
		}
		if !frameAllowed(row, turns, frameID) {
			return domain.Err(404, "RESOURCE_NOT_FOUND", "这张图片没有公开。")
		}
		found, err := one[domain.Asset](ctx, tx, "SELECT to_jsonb(m) FROM media_assets m WHERE m.id=$1 AND m.snapshot_id=$2 AND m.kind='frame' AND m.state='ready'", frameID, row.SnapshotID)
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Err(404, "RESOURCE_NOT_FOUND", "这张图片没有公开。")
		}
		asset = found
		return err
	})
	return asset, e
}

func loadShowcase(ctx context.Context, q Querier, id string) (showcaseArticle, []showcaseTurn, error) {
	row, e := one[showcaseArticle](ctx, q, `
		SELECT jsonb_build_object(
			'content', a.content,
			'generation_model', a.generation_model,
			'snapshot_id', a.snapshot_id,
			'purpose', COALESCE((
				SELECT g.generation_spec->>'purpose'
				FROM generation_requests g
				WHERE g.article_id = a.id AND g.generation_spec IS NOT NULL
				ORDER BY g.created_at DESC
				LIMIT 1
			), '')
		)
		FROM articles a
		JOIN example_showcases s ON s.article_id = a.id
		WHERE a.id = $1`, id)
	if errors.Is(e, pgx.ErrNoRows) {
		return row, nil, domain.Err(404, "RESOURCE_NOT_FOUND", "这篇示例没有公开。")
	}
	if e != nil {
		return row, nil, e
	}
	turns, e := many[showcaseTurn](ctx, q, `
		SELECT jsonb_build_object(
			'id', t.id,
			'question', t.question,
			'context', t.context,
			'answer', t.answer,
			'created_at', t.created_at,
			'updated_at', t.updated_at
		)
		FROM qa_turns t
		WHERE t.article_id = $1 AND t.status = 'completed' AND t.answer IS NOT NULL
		ORDER BY t.created_at
		LIMIT 8`, id)
	return row, turns, e
}

func showcaseFrames(ctx context.Context, q Querier, row showcaseArticle, turns []showcaseTurn, articleID string) ([]domain.Frame, map[string]domain.Frame, error) {
	ids := referencedFrameIDs(row.Content, turns)
	assets, e := readyFrames(ctx, q, row.SnapshotID, ids)
	if e != nil {
		return nil, nil, e
	}
	frames := make([]domain.Frame, 0, len(assets))
	byID := map[string]domain.Frame{}
	for _, asset := range assets {
		frame := publicFrame(asset, articleID)
		frames = append(frames, frame)
		byID[frame.ID] = frame
	}
	return frames, byID, nil
}

func referencedFrameIDs(article domain.Article, turns []showcaseTurn) []string {
	ids := ContentFrameIDs(article.Sections, article.Citations)
	for _, turn := range turns {
		if turn.Context.FrameID != nil {
			ids = appendFrameID(ids, *turn.Context.FrameID)
		}
		if turn.Answer == nil {
			continue
		}
		for _, frame := range turn.Answer.Frames {
			ids = appendFrameID(ids, frame.ID)
		}
		for _, block := range turn.Answer.Blocks {
			for _, id := range block.FrameIDs {
				ids = appendFrameID(ids, id)
			}
		}
		ids = append(ids, ContentFrameIDs(nil, turn.Answer.Citations)...)
	}
	return ids
}

func frameAllowed(row showcaseArticle, turns []showcaseTurn, frameID string) bool {
	for _, id := range referencedFrameIDs(row.Content, turns) {
		if id == frameID {
			return true
		}
	}
	return false
}

func appendFrameID(ids []string, id string) []string {
	if id == "" {
		return ids
	}
	for _, existing := range ids {
		if existing == id {
			return ids
		}
	}
	return append(ids, id)
}

func readyFrames(ctx context.Context, q Querier, snapshotID string, ids []string) ([]domain.Asset, error) {
	if len(ids) == 0 {
		return []domain.Asset{}, nil
	}
	return many[domain.Asset](ctx, q, "SELECT to_jsonb(m) FROM media_assets m WHERE m.snapshot_id=$1 AND m.kind='frame' AND m.state='ready' AND m.id = ANY($2::text[]) ORDER BY m.time_ms NULLS LAST, m.id", snapshotID, ids)
}

func firstReadyFrame(ctx context.Context, q Querier, snapshotID string, ids []string) (string, error) {
	assets, e := readyFrames(ctx, q, snapshotID, ids)
	if e != nil || len(assets) == 0 {
		return "", e
	}
	return assets[0].ID, nil
}

func publicFrame(asset domain.Asset, articleID string) domain.Frame {
	frame := domain.Frame{
		ID: asset.ID, Width: asset.Width, Height: asset.Height, MIMEType: asset.MIME, Caption: asset.Caption,
		Crop: asset.Crop, Availability: "ready", URLs: map[string]*string{},
	}
	if asset.TimeMS != nil {
		frame.TimeMS = *asset.TimeMS
	}
	if asset.RequestedTimeMS != nil {
		frame.RequestedTimeMS = *asset.RequestedTimeMS
	}
	full := exampleFrameURL(articleID, asset.ID)
	frame.URLs["full"] = &full
	thumb := full
	frame.URLs["thumbnail"] = &thumb
	return frame
}

func exampleFrameURL(articleID, frameID string) string {
	return "/api/v1/examples/" + url.PathEscape(articleID) + "/frames/" + url.PathEscape(frameID) + "/content"
}

func scrubArticle(article domain.Article) domain.Article {
	article.SnapshotID = ""
	article.Source.SourceID = ""
	article.Source.SnapshotID = nil
	for id, citation := range article.Citations {
		citation.SnapshotID = ""
		citation.ContentSHA256 = ""
		article.Citations[id] = citation
	}
	return article
}

func projectAnswer(answer domain.Answer, frames map[string]domain.Frame) domain.Answer {
	kept := make([]domain.Frame, 0, len(answer.Frames))
	for _, frame := range answer.Frames {
		if pub, ok := frames[frame.ID]; ok {
			kept = append(kept, pub)
		}
	}
	answer.Frames = kept
	if answer.Blocks == nil {
		answer.Blocks = []domain.Block{}
	}
	if answer.Citations == nil {
		answer.Citations = map[string]domain.Citation{}
	}
	for id, citation := range answer.Citations {
		citation.SnapshotID = ""
		citation.ContentSHA256 = ""
		answer.Citations[id] = citation
	}
	if answer.Limitations == nil {
		answer.Limitations = []domain.Limitation{}
	}
	return answer
}

func clipRunes(value string, limit int) string {
	value = strings.TrimSpace(value)
	if utf8.RuneCountInString(value) <= limit {
		return value
	}
	return string([]rune(value)[:limit])
}
