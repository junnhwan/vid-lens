package investigator

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"vidlens/internal/domain"
	"vidlens/internal/media"
)

// Explicit retries reuse an observation's original frame identities. Running
// FFmpeg first would create new aliases and invalidate the inherited report.
func (s *session) inheritedWorkflowFrames(ctx context.Context, key string) ([]string, bool, error) {
	if !s.scopedWorkflow() {
		return nil, false, nil
	}
	op, err := s.engine.Store.Operation(ctx, s.claim.Run.ID, "workflow-inherited:"+key)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	var saved struct {
		FrameNotes []workflowFrameNote `json:"frame_notes"`
	}
	if op.Kind != "workflow_artifact" || op.Status != "completed" || workflowDecode(string(op.Result), &saved) != nil || len(saved.FrameNotes) == 0 {
		return nil, false, domain.Err(422, "WORKFLOW_RESULT_INVALID", "继承的取证观察不完整；旧成果保留，未重新取帧替换证据。")
	}
	all, err := s.workflowFrames(ctx, nil)
	if err != nil {
		return nil, false, err
	}
	ids, selected := []string{}, []workflowFrame{}
	for _, frame := range all {
		for _, note := range saved.FrameNotes {
			if frame.Alias == note.FrameAlias {
				ids = append(ids, frame.ID)
				selected = append(selected, frame)
				break
			}
		}
	}
	if err = validateWorkflowFrameNotes(saved.FrameNotes, selected); err != nil {
		return nil, false, domain.Err(422, "WORKFLOW_EVIDENCE_CHANGED", "继承观察的原图身份不再可用；未用新像素代替。"+err.Error())
	}
	return ids, true, nil
}

type workflowPassage struct {
	StartMS int64  `json:"start_ms"`
	EndMS   int64  `json:"end_ms"`
	Origin  string `json:"origin"`
	Text    string `json:"original_text_with_citation_markers"`
}

// Merge transport containers, never source text or identities. Every original
// cue retains its stable alias, even when cues outside the selected range are
// omitted. Original timestamps and segment IDs remain in the source snapshot.
func workflowPassages(transcript []domain.Segment, start, end int64) []workflowPassage {
	out := []workflowPassage{}
	for i, cue := range transcript {
		if cue.EndMS <= start || cue.StartMS >= end {
			continue
		}
		text := "[t" + strconv.Itoa(i+1) + "]" + cue.Text + "\n"
		if len(out) > 0 {
			last := &out[len(out)-1]
			if cue.Origin == last.Origin && cue.StartMS <= last.EndMS+3000 && utf8.RuneCountInString(last.Text)+utf8.RuneCountInString(text) <= 2400 {
				last.Text += text
				last.EndMS = max(last.EndMS, cue.EndMS)
				continue
			}
		}
		out = append(out, workflowPassage{StartMS: cue.StartMS, EndMS: cue.EndMS, Origin: cue.Origin, Text: text})
	}
	return out
}

type workflowFrame struct {
	Alias  string `json:"alias"`
	ID     string `json:"id"`
	TimeMS int64  `json:"time_ms"`
	SHA256 string `json:"sha256"`
}

func (s *session) workflowFrames(ctx context.Context, selected []string) ([]workflowFrame, error) {
	frames := []workflowFrame{}
	for i, id := range s.cp.CitationFrames {
		if selected != nil && !slices.Contains(selected, id) {
			continue
		}
		a, err := s.engine.Store.InternalFrame(ctx, s.claim, id)
		if err != nil {
			return nil, err
		}
		if a.TimeMS == nil || a.State != "ready" {
			return nil, media.ErrMediaMissing
		}
		frames = append(frames, workflowFrame{Alias: fmt.Sprint("f", i+1), ID: id, TimeMS: *a.TimeMS, SHA256: a.SHA256})
	}
	return frames, nil
}

func (s *session) workflowEvidence(ctx context.Context, start, end int64, frameIDs []string) (map[string]any, error) {
	frames, err := s.workflowFrames(ctx, frameIDs)
	if err != nil {
		return nil, err
	}
	out := map[string]any{"snapshot_id": s.snapshot.ID, "text_hash": s.snapshot.TextHash, "video_hash": s.snapshot.VideoHash, "duration_ms": s.snapshot.DurationMS, "source": s.snapshot.Source, "original_passages": workflowPassages(s.transcript, start, end), "delivered_pixels": frames, "detail_level": s.spec.DetailLevel, "purpose": s.spec.Purpose}
	if s.spec.DetailPolicy != nil {
		out["detail_instruction"] = s.spec.DetailPolicy.Instruction
	}
	return out, nil
}

// A response may both observe and cite its delivered pixels. These input
// identities are available for validating that response before observations
// become durable; publication still uses the observed-only citation catalog.
func (s *session) workflowInputCatalog(ctx context.Context, frameIDs []string) (map[string]domain.Citation, error) {
	catalog, err := s.citationCatalog(ctx)
	if err != nil {
		return nil, err
	}
	frames, err := s.workflowFrames(ctx, frameIDs)
	if err != nil {
		return nil, err
	}
	for _, frame := range frames {
		catalog[frame.Alias] = domain.Citation{ID: frame.Alias, Kind: "video_frame", Label: "视频画面", SnapshotID: s.snapshot.ID, FrameID: frame.ID, TimeMS: domain.Ptr(frame.TimeMS)}
	}
	return workflowDeliveredCatalog(catalog, frameIDs), nil
}

// The model selects an evidence question and source range. The server owns
// FFmpeg's window ceiling and a fixed three-frame sampling cost, including
// comparisons whose source ranges span more than one minute.
func workflowInspectionWindows(question workflowInvestigation) []media.Window {
	span := question.EndMS - question.StartMS
	if span <= 60000 {
		return []media.Window{{StartMS: question.StartMS, EndMS: question.EndMS, Count: 3, Mode: "detail"}}
	}
	intersections := []media.Window{}
	for i := 0; i < 3; i++ {
		t := question.StartMS + span*int64(2*i+1)/6
		intersections = append(intersections, media.Window{StartMS: t, EndMS: min(question.EndMS, t+1), Count: 1, Mode: "detail"})
	}
	return intersections
}

type workflowFrameNote struct {
	FrameAlias   string   `json:"alias"`
	VisibleFacts []string `json:"visible_facts" jsonschema:"minItems=1,maxItems=8" jsonschema_description:"Each fact is nonempty and at most 500 characters; concise visible facts, not full-page transcription."`
	Uncertain    []string `json:"uncertain"`
}

// Current tools use the delivered-pixel field name. Previously saved plans and
// frozen calls used frame_alias; accept that exact spelling without relaxing
// unknown-field or evidence-identity checks.
func (note *workflowFrameNote) UnmarshalJSON(data []byte) error {
	var wire struct {
		Alias        string   `json:"alias"`
		LegacyAlias  string   `json:"frame_alias"`
		VisibleFacts []string `json:"visible_facts"`
		Uncertain    []string `json:"uncertain"`
	}
	if err := workflowDecode(string(data), &wire); err != nil {
		return err
	}
	if wire.Alias != "" && wire.LegacyAlias != "" {
		return fmt.Errorf("图片观察不能同时指定alias与frame_alias")
	}
	alias := wire.Alias
	if alias == "" {
		alias = wire.LegacyAlias
	}
	*note = workflowFrameNote{FrameAlias: alias, VisibleFacts: wire.VisibleFacts, Uncertain: wire.Uncertain}
	return nil
}

const workflowFrameNoteInstructions = `frame_notes每项使用alias（与delivered_pixels相同）、visible_facts、uncertain。覆盖本轮全部真实图片；每张1–8条可见事实，每条1–500字，简述事实和作用，不整页抄录。模糊内容放uncertain；无图则frame_notes为[]。`

func validateWorkflowFrameNotes(notes []workflowFrameNote, frames []workflowFrame) error {
	seen := map[string]bool{}
	for _, note := range notes {
		if seen[note.FrameAlias] || len(note.VisibleFacts) == 0 || len(note.VisibleFacts) > 8 {
			return fmt.Errorf("图片%s的观察缺失、重复或超长", note.FrameAlias)
		}
		found := false
		for _, f := range frames {
			found = found || f.Alias == note.FrameAlias
		}
		if !found {
			return fmt.Errorf("未交付图片%s", note.FrameAlias)
		}
		for _, fact := range note.VisibleFacts {
			if strings.TrimSpace(fact) == "" || utf8.RuneCountInString(fact) > 500 {
				return fmt.Errorf("图片%s观察必须简短且非空", note.FrameAlias)
			}
		}
		seen[note.FrameAlias] = true
	}
	if len(seen) != len(frames) {
		return fmt.Errorf("必须记录全部%d张已交付图片的观察", len(frames))
	}
	return nil
}

func (s *session) saveWorkflowFrameNotes(ctx context.Context, key string, notes []workflowFrameNote) error {
	frames, err := s.workflowFrames(ctx, nil)
	if err != nil {
		return err
	}
	for _, note := range notes {
		for _, f := range frames {
			if f.Alias != note.FrameAlias {
				continue
			}
			o := observation{InspectionID: "workflow:" + key, QuestionID: "workflow:" + key + ":" + f.Alias, Question: "记录实际交付画面的可见内容", FrameIDs: []string{f.ID}, VisibleFacts: note.VisibleFacts, Unresolved: note.Uncertain, Status: "supported"}
			if err = s.engine.Store.RecordObservation(ctx, s.claim, "workflow:"+key+":"+f.Alias, o.InspectionID, o); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *session) prepareWorkflowFrames(ctx context.Context) error {
	if len(s.cp.OverviewIDs) > 0 {
		return nil
	}
	bins := min(s.profile.OverviewBins, 12)
	if s.snapshot.DurationMS < 60000 {
		bins = min(bins, 4)
	}
	ids := []string{}
	for i := 0; i < max(1, bins); i++ {
		t := s.snapshot.DurationMS * int64(2*i+1) / int64(2*max(1, bins))
		result, err := s.engine.Media.Inspect(ctx, s.claim, fmt.Sprint("workflow-overview:", i), "取得覆盖全片的真实画面", media.Window{StartMS: max(int64(0), t-1), EndMS: min(s.snapshot.DurationMS, t+1), Count: 1, Mode: "overview"}, nil)
		if err != nil {
			return err
		}
		ids = append(ids, result.FrameIDs...)
	}
	s.cp.OverviewIDs, s.cp.CitationFrames = ids, append([]string{}, ids...)
	return s.persist(ctx, s.cp.Messages)
}

type workflowItem struct {
	Text        string   `json:"text"`
	CitationIDs []string `json:"citation_ids"`
}

type workflowBlock struct {
	Type        string            `json:"type" jsonschema:"enum=paragraph,enum=quote,enum=list,enum=code,enum=table,enum=figure"`
	Origin      string            `json:"origin" jsonschema:"enum=video,enum=background_explanation"`
	Text        string            `json:"text,omitempty"`
	CitationIDs []string          `json:"citation_ids,omitempty"`
	Ordered     bool              `json:"ordered,omitempty"`
	Items       []workflowItem    `json:"items,omitempty"`
	Language    *string           `json:"language,omitempty"`
	Columns     []string          `json:"columns,omitempty"`
	Rows        []domain.TableRow `json:"rows,omitempty"`
	FrameIDs    []string          `json:"frame_ids,omitempty"`
	Caption     string            `json:"caption,omitempty"`
}

type workflowSection struct {
	ID     string          `json:"id"`
	Blocks []workflowBlock `json:"blocks"`
}

type workflowBodyV1 struct {
	Sections []workflowSection `json:"sections"`
}

type workflowBody struct {
	Sections    []workflowSection   `json:"sections"`
	Limitations []domain.Limitation `json:"limitations"`
}

func workflowCanonicalSection(in workflowSection, planned sectionPlan, catalog map[string]domain.Citation) (domain.Section, error) {
	out := domain.Section{ID: planned.ID, Title: planned.Title, Blocks: []domain.Block{}}
	if in.ID != planned.ID || len(in.Blocks) == 0 || len(in.Blocks) > 100 {
		return out, fmt.Errorf("章节%s缺失、身份不匹配或正文超限", planned.ID)
	}
	inline := func(text string, ids []string) []domain.Inline {
		content := []domain.Inline{{Type: "text", Text: text}}
		for _, id := range ids {
			content = append(content, domain.Inline{Type: "citation", CitationID: id})
		}
		return content
	}
	for i, raw := range in.Blocks {
		b := domain.Block{ID: fmt.Sprintf("%s_b%d", in.ID, i+1), Type: raw.Type, Origin: raw.Origin, Text: raw.Text, Language: raw.Language, Columns: raw.Columns, Rows: raw.Rows, FrameIDs: append([]string{}, raw.FrameIDs...), Caption: raw.Caption, CitationIDs: append([]string{}, raw.CitationIDs...)}
		refs := [][]string{raw.CitationIDs}
		switch raw.Type {
		case "paragraph", "quote":
			if strings.TrimSpace(raw.Text) == "" {
				return out, fmt.Errorf("%s正文为空", b.ID)
			}
			b.Content, b.Text, b.CitationIDs = inline(raw.Text, raw.CitationIDs), "", nil
		case "list":
			b.Ordered = domain.Ptr(raw.Ordered)
			refs = nil
			for j, item := range raw.Items {
				if strings.TrimSpace(item.Text) == "" {
					return out, fmt.Errorf("%s列表项为空", b.ID)
				}
				b.Items = append(b.Items, domain.ListItem{ID: fmt.Sprintf("%s_i%d", b.ID, j+1), Content: inline(item.Text, item.CitationIDs)})
				refs = append(refs, item.CitationIDs)
			}
		case "table":
			refs = nil
			for _, row := range raw.Rows {
				refs = append(refs, row.CitationIDs)
			}
		case "figure":
			b.Layout = "single"
			if len(b.FrameIDs) > 1 {
				b.Layout = "sequence"
			}
			for j, alias := range b.FrameIDs {
				c, ok := catalog[alias]
				if !ok || c.Kind != "video_frame" {
					return out, fmt.Errorf("%s未交付真实图片%s", b.ID, alias)
				}
				b.FrameIDs[j] = c.FrameID
				if !slices.Contains(b.CitationIDs, alias) {
					b.CitationIDs = append(b.CitationIDs, alias)
				}
			}
			refs = [][]string{b.CitationIDs}
		case "code":
		default:
			return out, fmt.Errorf("%s正文类型无效", b.ID)
		}
		for _, ids := range refs {
			if raw.Origin == "video" && len(ids) == 0 {
				return out, fmt.Errorf("%s每个断言、列表项和表格行须各自绑定原文或真实图片", b.ID)
			}
			for _, id := range ids {
				if _, ok := catalog[id]; !ok {
					return out, fmt.Errorf("%s引用%s未在本批交付", b.ID, id)
				}
			}
		}
		out.Blocks = append(out.Blocks, b)
	}
	return out, nil
}
