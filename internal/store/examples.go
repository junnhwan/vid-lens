package store

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"vidlens/internal/domain"
)

const maxExampleShowcases = 24
const ExampleQuestionLimit = 8

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
	ProcessStatus   string                  `json:"process_status"`
	GenerationSteps []domain.Step           `json:"generation_steps"`
	Questions       []PublicExampleQuestion `json:"questions"`
}

type exampleProcess struct {
	Status string        `json:"status"`
	Steps  []domain.Step `json:"steps"`
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
		kept := map[string][]byte{}
		rows, e := tx.Query(ctx, "SELECT article_id, process FROM example_showcases WHERE process_frozen")
		if e != nil {
			return e
		}
		for rows.Next() {
			var id string
			var process []byte
			if e = rows.Scan(&id, &process); e != nil {
				rows.Close()
				return e
			}
			kept[id] = process
		}
		rows.Close()
		if e = rows.Err(); e != nil {
			return e
		}
		if _, e = tx.Exec(ctx, "DELETE FROM example_showcases"); e != nil {
			return e
		}
		for i, id := range ids {
			process := kept[id]
			if process == nil {
				if process, e = captureExampleProcess(ctx, tx, id); e != nil {
					return e
				}
			}
			if _, e = tx.Exec(ctx, "INSERT INTO example_showcases(article_id, position, process, process_frozen) VALUES($1, $2, $3, true)", id, i+1, process); e != nil {
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
		row, process, err := loadShowcase(ctx, tx, id)
		if err != nil {
			return err
		}
		frames, _, err := showcaseFrames(ctx, tx, row, id)
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
		var ready, restoring bool
		if err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM media_assets WHERE snapshot_id=$1 AND kind='video' AND state='ready' AND (expires_at IS NULL OR expires_at>now() OR EXISTS(SELECT 1 FROM asset_refs WHERE asset_id=media_assets.id AND (expires_at IS NULL OR expires_at>now()))))", row.SnapshotID).Scan(&ready); err != nil {
			return err
		}
		if err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM job_runs WHERE snapshot_id=$1 AND kind='source.restore' AND status IN ('queued','running','retry_wait'))", row.SnapshotID).Scan(&restoring); err != nil {
			return err
		}
		status := "expired"
		if ready {
			status = "ready"
		} else if restoring {
			status = "restoring"
		}
		var configID string
		if err = tx.QueryRow(ctx, "SELECT config_id FROM articles WHERE id=$1", id).Scan(&configID); err != nil {
			return err
		}
		qaReady := true
		if err = checkAvailable(ctx, tx, configID); err != nil {
			var problem *domain.Problem
			if !errors.As(err, &problem) {
				return err
			}
			qaReady = false
		}
		model := row.Model
		if model != nil {
			copy := *model
			copy.RevisionID = ""
			copy.Description = ""
			model = &copy
		}
		if process.Steps == nil {
			process.Steps = []domain.Step{}
		}
		if process.Status == "" {
			process.Status = "unavailable"
		}
		out = PublicExample{
			ID: id, SchemaVersion: 1, DetailLevel: article.DetailLevel, Article: article, Frames: frames,
			Media:           publicExampleMedia{Status: status, CanInspectNow: ready, CanRestoreForQuestion: true},
			GenerationModel: model, Purpose: strings.TrimSpace(row.Purpose), QAAvailable: qaReady,
			ProcessStatus: process.Status, GenerationSteps: process.Steps, Questions: []PublicExampleQuestion{},
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
		row, process, err := loadShowcase(ctx, tx, articleID)
		if err != nil {
			return err
		}
		if !frameAllowed(row, process, frameID) {
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

func loadShowcase(ctx context.Context, q Querier, id string) (showcaseArticle, exampleProcess, error) {
	var process exampleProcess
	row, e := one[showcaseArticle](ctx, q, `
		SELECT jsonb_build_object(
			'content', a.content,
			'generation_model', a.generation_model,
			'snapshot_id', a.snapshot_id,
			'purpose', COALESCE((
				SELECT g.generation_spec->>'purpose'
				FROM generation_requests g
				WHERE g.article_id = a.id AND g.generation_spec IS NOT NULL AND g.reuse <> 'example_question'
				ORDER BY g.created_at DESC
				LIMIT 1
			), '')
		)
		FROM articles a
		JOIN example_showcases s ON s.article_id = a.id
		WHERE a.id = $1`, id)
	if errors.Is(e, pgx.ErrNoRows) {
		return row, process, domain.Err(404, "RESOURCE_NOT_FOUND", "这篇示例没有公开。")
	}
	if e != nil {
		return row, process, e
	}
	var raw []byte
	if e = q.QueryRow(ctx, "SELECT process FROM example_showcases WHERE article_id=$1", id).Scan(&raw); e != nil {
		return row, process, e
	}
	if len(raw) > 0 {
		if e = json.Unmarshal(raw, &process); e != nil {
			return row, process, e
		}
	}
	return row, process, nil
}

func showcaseFrames(ctx context.Context, q Querier, row showcaseArticle, articleID string) ([]domain.Frame, map[string]domain.Frame, error) {
	ids := ContentFrameIDs(row.Content.Sections, row.Content.Citations)
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

func frameAllowed(row showcaseArticle, process exampleProcess, frameID string) bool {
	for _, id := range ContentFrameIDs(row.Content.Sections, row.Content.Citations) {
		if id == frameID {
			return true
		}
	}
	for _, step := range process.Steps {
		for _, id := range step.FrameIDs {
			if id == frameID {
				return true
			}
		}
		for _, frame := range step.Frames {
			if frame.ID == frameID {
				return true
			}
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

func clipRunes(value string, limit int) string {
	value = strings.TrimSpace(value)
	if utf8.RuneCountInString(value) <= limit {
		return value
	}
	return string([]rune(value)[:limit])
}

func unavailableExampleProcess() []byte {
	raw, err := json.Marshal(exampleProcess{Status: "unavailable", Steps: []domain.Step{}})
	if err != nil {
		return []byte(`{"status":"unavailable","steps":[]}`)
	}
	return raw
}

func captureExampleProcess(ctx context.Context, tx pgx.Tx, articleID string) ([]byte, error) {
	req, err := one[domain.Request](ctx, tx, `
		SELECT to_jsonb(r) FROM generation_requests r
		JOIN articles a ON a.id=$1
		WHERE r.article_id=a.id AND r.reuse<>'example_question'
		  AND EXISTS (SELECT 1 FROM jsonb_array_elements(r.segments) e WHERE e->>'run_id'=a.generation_run_id)
		ORDER BY r.created_at
		LIMIT 1`, articleID)
	if errors.Is(err, pgx.ErrNoRows) {
		return unavailableExampleProcess(), nil
	}
	if err != nil {
		return nil, err
	}
	segments, err := sharedSegments(ctx, tx, req)
	if err != nil {
		return nil, err
	}
	steps, history, _, err := stepsAt(ctx, tx, segments, req.ID)
	if err != nil {
		return nil, err
	}
	status := "unavailable"
	if len(steps) > 0 && history == "available" {
		status = "ready"
	} else if len(steps) > 0 {
		status = "partial"
	}
	return json.Marshal(exampleProcess{Status: status, Steps: projectExampleSteps(steps, articleID)})
}

func projectExampleSteps(steps []domain.Step, articleID string) []domain.Step {
	out := make([]domain.Step, len(steps))
	for i, step := range steps {
		step.RunID = "attempt-" + strconv.Itoa(step.SegmentNo)
		frames := make([]domain.Frame, len(step.Frames))
		for j, frame := range step.Frames {
			frames[j] = projectExampleFrame(frame, articleID)
		}
		step.Frames = frames
		if step.FrameIDs == nil {
			step.FrameIDs = []string{}
		}
		out[i] = step
	}
	return out
}

func projectExampleFrame(frame domain.Frame, articleID string) domain.Frame {
	frame.SnapshotID = ""
	frame.ReplacesFrameID = nil
	frame.ExpiresAt = nil
	frame.URLs = map[string]*string{"full": nil, "thumbnail": nil}
	if frame.Availability == "ready" {
		path := exampleFrameURL(articleID, frame.ID)
		frame.URLs["full"] = &path
		frame.URLs["thumbnail"] = &path
	}
	return frame
}

func (s *Store) FreezeExampleProcess(ctx context.Context, articleID string) error {
	if !exampleArticleID.MatchString(articleID) {
		return nil
	}
	return s.Tx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var frozen bool
		err := tx.QueryRow(ctx, "SELECT process_frozen FROM example_showcases WHERE article_id=$1", articleID).Scan(&frozen)
		if errors.Is(err, pgx.ErrNoRows) || frozen {
			return nil
		}
		if err != nil {
			return err
		}
		process, err := captureExampleProcess(ctx, tx, articleID)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, "UPDATE example_showcases SET process=$2, process_frozen=true WHERE article_id=$1 AND NOT process_frozen", articleID, process)
		return err
	})
}

func (s *Store) OpenExampleConversation(ctx context.Context, session, articleID string) (domain.Conversation, bool, error) {
	var out domain.Conversation
	var created bool
	if !exampleArticleID.MatchString(session) || !exampleArticleID.MatchString(articleID) {
		return out, false, domain.Err(404, "RESOURCE_NOT_FOUND", "这篇示例没有公开。")
	}
	err := s.Tx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var sourceID, snapshotID, configID, inputURL string
		var spec []byte
		err := tx.QueryRow(ctx, `
			SELECT a.snapshot_id, a.config_id, ss.source_id,
			       COALESCE(NULLIF(a.content#>>'{source,canonical_url}', ''), 'https://vidlens.invalid/' || a.id),
			       spec.generation_spec
			FROM example_showcases es
			JOIN articles a ON a.id = es.article_id
			JOIN source_snapshots ss ON ss.id = a.snapshot_id
			LEFT JOIN LATERAL (
				SELECT generation_spec FROM generation_requests g
				WHERE g.article_id = a.id AND g.reuse <> 'example_question' AND g.generation_spec IS NOT NULL
				ORDER BY g.created_at
				LIMIT 1
			) spec ON true
			WHERE es.article_id = $1`, articleID).Scan(&snapshotID, &configID, &sourceID, &inputURL, &spec)
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Err(404, "RESOURCE_NOT_FOUND", "这篇示例没有公开。")
		}
		if err != nil {
			return err
		}
		requestID, err := exampleQuestionRequest(ctx, tx, session, articleID, inputURL, sourceID, snapshotID, configID, spec)
		if err != nil {
			return err
		}
		found, err := one[domain.Conversation](ctx, tx, "SELECT to_jsonb(c) FROM qa_conversations c WHERE session_id=$1 AND request_id=$2", session, requestID)
		if err == nil {
			out = found
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		created = true
		out, err = one[domain.Conversation](ctx, tx, "INSERT INTO qa_conversations(id,session_id,request_id,article_id,snapshot_id) VALUES($1,$2,$3,$4,$5) RETURNING to_jsonb(qa_conversations)", domain.ID(), session, requestID, articleID, snapshotID)
		return err
	})
	return out, created, err
}

func exampleQuestionRequest(ctx context.Context, tx pgx.Tx, session, articleID, inputURL, sourceID, snapshotID, configID string, spec []byte) (string, error) {
	var requestID string
	err := tx.QueryRow(ctx, "SELECT id FROM generation_requests WHERE session_id=$1 AND article_id=$2 AND reuse='example_question'", session, articleID).Scan(&requestID)
	if err == nil {
		return requestID, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return "", err
	}
	requestID = domain.ID()
	_, err = tx.Exec(ctx, `INSERT INTO generation_requests(id,session_id,input_url,source_id,snapshot_id,article_id,config_id,status,reuse,segments,deadline_at,generation_spec)
		VALUES($1,$2,$3,$4,$5,$6,$7,'completed','example_question','[]'::jsonb,now()+interval '24 hours',$8)`,
		requestID, session, inputURL, sourceID, snapshotID, articleID, configID, spec)
	if err == nil {
		return requestID, nil
	}
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.Code == "23505" {
		err = tx.QueryRow(ctx, "SELECT id FROM generation_requests WHERE session_id=$1 AND article_id=$2 AND reuse='example_question'", session, articleID).Scan(&requestID)
		return requestID, err
	}
	return "", err
}
