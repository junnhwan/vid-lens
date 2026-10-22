package investigator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
	"github.com/cloudwego/eino/schema"
	"github.com/eino-contrib/jsonschema"
	"github.com/jackc/pgx/v5"
	"vidlens/internal/domain"
	"vidlens/internal/store"
)

const loopSaveTool = "save_sections"
const loopFinishTool = "finish_article"
const loopReviewTool = "report_article_review"
const loopWriteBytes = 128 << 10

type loopSourcedText struct {
	Text    string   `json:"text"`
	Sources []string `json:"sources"`
}
type loopList struct {
	Ordered bool              `json:"ordered"`
	Items   []loopSourcedText `json:"items"`
}
type loopCode struct {
	Text     string `json:"text"`
	Language string `json:"language"`
}
type loopRow struct {
	Cells   []string `json:"cells"`
	Sources []string `json:"sources"`
}
type loopTable struct {
	Columns []string  `json:"columns"`
	Rows    []loopRow `json:"rows"`
}
type loopFigure struct {
	Frames       []string `json:"frames" jsonschema:"description=Delivered fN aliases, in actual time order"`
	Caption      string   `json:"caption"`
	VisibleFacts []string `json:"visible_facts"`
	Uncertainty  []string `json:"uncertainty"`
}

// One constructor selects the canonical block. IDs, origin, layout, citation
// metadata and rendering nodes are exclusively constructed by the program.
type loopContent struct {
	Text       *string     `json:"text,omitempty"`
	Quote      *string     `json:"quote,omitempty"`
	List       *loopList   `json:"list,omitempty"`
	Code       *loopCode   `json:"code,omitempty"`
	Table      *loopTable  `json:"table,omitempty"`
	Figure     *loopFigure `json:"figure,omitempty"`
	Background *string     `json:"background,omitempty"`
	Sources    []string    `json:"sources,omitempty"`
}
type loopPixelObservation struct {
	Frame        string   `json:"frame"`
	VisibleFacts []string `json:"visible_facts"`
	Uncertainty  []string `json:"uncertainty"`
}

type loopSection struct {
	Key          string                  `json:"key,omitempty" jsonschema:"description=Unique temporary key for creation only; program returns a stable section_id"`
	SectionID    string                  `json:"section_id,omitempty" jsonschema:"description=Existing stable identity for replacement only"`
	Title        string                  `json:"title"`
	Content      []loopContent           `json:"content"`
	Observations []loopPixelObservation  `json:"observations,omitempty"`
	Limitations  []loopSectionLimitation `json:"limitations,omitempty"`
	rejected     string
}
type loopSectionLimitation struct {
	Code    string   `json:"code"`
	Message string   `json:"message"`
	Sources []string `json:"sources"`
}

func (s *loopSection) UnmarshalJSON(raw []byte) error {
	type wire loopSection
	var value wire
	err := loopDecode(raw, &value)
	*s = loopSection(value)
	if err != nil {
		s.rejected = err.Error()
	}
	return nil // Whole sections are independently rejected after envelope parsing.
}

type loopSaveInput struct {
	Finish          bool          `json:"finish,omitempty" jsonschema:"description=After this write, request candidate review if all chapters were accepted and source coverage is complete"`
	ExpectedVersion int           `json:"expected_version"`
	Title           *string       `json:"title,omitempty"`
	Sections        []loopSection `json:"sections" jsonschema:"maxItems=40,description=At most 40 chapters and 128 KiB for the complete write action; obey the smaller current_call section/output limit, split complete actions when needed"`
	DeleteIDs       []string      `json:"delete_ids,omitempty"`
	Order           []string      `json:"order,omitempty" jsonschema:"description=Atomic reorder: every retained stable section_id exactly once; separate from section writes"`
}
type loopStateInput struct {
	SectionIDs []string `json:"section_ids,omitempty" jsonschema:"description=Optional existing section IDs to read in full; empty uses the current evidence workspace"`
}
type loopCannotCompleteInput struct {
	Reason      string   `json:"reason"`
	EvidenceIDs []string `json:"evidence_ids"`
}

type loopFinishInput struct {
	ExpectedVersion int `json:"expected_version"`
}
type loopReadInput struct {
	SourceIDs []string `json:"source_ids" jsonschema:"description=Original tN aliases; empty reads the next unseen range"`
	Query     string   `json:"query,omitempty"`
}
type loopViewInput struct {
	FrameIDs []string     `json:"frame_ids" jsonschema:"description=Existing fN aliases; empty requests a new bounded window"`
	Question string       `json:"question"`
	StartMS  int64        `json:"start_ms,omitempty" jsonschema:"minimum=0,description=New window start within the source; use no window when reloading frame_ids"`
	EndMS    int64        `json:"end_ms,omitempty" jsonschema:"minimum=1,description=New window end within source duration; 0 < end_ms - start_ms <= 60000 milliseconds"`
	Count    int          `json:"count,omitempty" jsonschema:"minimum=1,maximum=6,description=1 to 6 actual frames; omitted defaults to 4"`
	Crop     *domain.Crop `json:"crop,omitempty"`
}
type loopReceipt struct {
	Accepted []string          `json:"accepted"`
	Created  map[string]string `json:"created"`
	Rejected []loopRejection   `json:"rejected"`
}
type loopRejection struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}
type loopFrameNote struct {
	VisibleFacts []string `json:"visible_facts"`
	Uncertainty  []string `json:"uncertainty"`
}
type articleLoopDraft struct {
	Article      domain.Article           `json:"article"`
	Notes        map[string]loopFrameNote `json:"notes"`
	BatchReceipt *loopReceipt             `json:"batch_receipt,omitempty"`
}

// Reject duplicate keys at every depth before decoding sections. No brace,
// null-placeholder, string-array or partial-JSON normalization is performed.
func loopUniqueJSON(raw []byte) error {
	d := json.NewDecoder(strings.NewReader(string(raw)))
	path := []string{"$"}
	var visit func() error
	visit = func() error {
		tok, err := d.Token()
		if err != nil {
			return err
		}
		delim, ok := tok.(json.Delim)
		if !ok {
			return nil
		}
		switch delim {
		case '{':
			keys := map[string]bool{}
			for d.More() {
				t, e := d.Token()
				if e != nil {
					return e
				}
				k, ok := t.(string)
				if !ok {
					return fmt.Errorf("invalid JSON key at %s", strings.Join(path, ""))
				}
				path = append(path, "."+k)
				if keys[k] {
					return fmt.Errorf("duplicate JSON key at %s", strings.Join(path, ""))
				}
				keys[k] = true
				if e = visit(); e != nil {
					return e
				}
				path = path[:len(path)-1]
			}
			t, e := d.Token()
			if e != nil || t != json.Delim('}') {
				return errors.New("incomplete object")
			}
		case '[':
			for index := 0; d.More(); index++ {
				path = append(path, fmt.Sprint("[", index, "]"))
				if e := visit(); e != nil {
					return e
				}
				path = path[:len(path)-1]
			}
			t, e := d.Token()
			if e != nil || t != json.Delim(']') {
				return errors.New("incomplete array")
			}
		default:
			return errors.New("invalid JSON delimiter")
		}
		return nil
	}
	if err := visit(); err != nil {
		return err
	}
	if _, err := d.Token(); err != io.EOF {
		return errors.New("expected one complete JSON value")
	}
	return nil
}
func loopDecode(raw []byte, out any) error {
	if err := loopUniqueJSON(raw); err != nil {
		return err
	}
	if len(raw) == 0 || strings.TrimSpace(string(raw))[0] != '{' {
		return errors.New("expected complete JSON object")
	}
	d := json.NewDecoder(strings.NewReader(string(raw)))
	d.DisallowUnknownFields()
	return d.Decode(out)
}
func loopArguments(msg *schema.Message) (string, []byte, error) {
	if msg == nil || len(msg.ToolCalls) != 1 || strings.TrimSpace(msg.Content) != "" {
		return "", nil, errors.New("one named tool call is required; ordinary text is not completion")
	}
	c := msg.ToolCalls[0]
	raw := []byte(c.Function.Arguments)
	if err := loopUniqueJSON(raw); err != nil {
		return c.Function.Name, raw, err
	}
	return c.Function.Name, raw, nil
}

func (s *session) loopTools(review bool) ([]*schema.ToolInfo, error) {
	s.tools = map[string]tool.EnhancedInvokableTool{}
	tools := []tool.EnhancedInvokableTool{}
	add := func(t tool.EnhancedInvokableTool, err error) error {
		if err != nil {
			return err
		}
		tools = append(tools, t)
		return nil
	}
	if review {
		var reviewTool tool.EnhancedInvokableTool
		var err error
		if s.cp.ExecutionContract != nil && s.cp.ExecutionContract.Version == domain.ArticleLoopV2 {
			reviewTool, err = utils.InferEnhancedTool(loopReviewTool, "独立综合核验AC02–07：先记录每个实际像素的独立可见事实与不确定性，再对照visual_claims、原文和全片主题，提交完成状态及具体blocking/advisory问题。无依据的具体图注或视觉断言为AC05 blocking。不要返回身份哈希或逐段supported。", func(context.Context, domain.ArticleGroundedReviewReport) (*schema.ToolResult, error) { return nil, nil })
		} else {
			reviewTool, err = utils.InferEnhancedTool(loopReviewTool, "独立综合核验。逐项执行AC02–07，完整性、未核验范围、主题到章节映射和具体blocking/advisory问题。没有问题不等于完成；不要返回身份哈希。", func(context.Context, domain.ArticleReviewReport) (*schema.ToolResult, error) { return nil, nil })
		}
		if err := add(reviewTool, err); err != nil {
			return nil, err
		}
	} else {
		if err := add(utils.InferEnhancedTool(loopSaveTool, "创建或替换独立章节，无需先提交提纲。每个content对象仅一个text/quote/list/code/table/figure/background构造；事实就近sources，列表每项和表格每行有sources；图片有visible_facts和uncertainty。合法章节局部接纳，回执有最新版本与创建身份。", func(context.Context, loopSaveInput) (*schema.ToolResult, error) { return nil, nil })); err != nil {
			return nil, err
		}
		if err := add(utils.InferEnhancedTool(loopFinishTool, "请求冻结当前稿件并进入独立核验；不是批准或发布。", func(context.Context, loopFinishInput) (*schema.ToolResult, error) { return nil, nil })); err != nil {
			return nil, err
		}
		if err := add(utils.InferEnhancedTool("read_evidence", "读取原文或按词定位，空选择读取下一未读范围；来源索引不是原文。", func(context.Context, loopReadInput) (*schema.ToolResult, error) { return nil, nil })); err != nil {
			return nil, err
		}
		if err := add(utils.InferEnhancedTool("view_frames", "实际查看已有图或有明确问题的窗口/裁剪；已有帧优先。新窗口须在当前视频内且0<end_ms-start_ms<=60000毫秒，每次1–6帧；重新加载frame_ids不能同时指定窗口或裁剪。", func(context.Context, loopViewInput) (*schema.ToolResult, error) { return nil, nil })); err != nil {
			return nil, err
		}
		if err := add(utils.InferEnhancedTool("read_article_state", "重新读取权威稿件版本、待办和预算。", func(context.Context, loopStateInput) (*schema.ToolResult, error) { return nil, nil })); err != nil {
			return nil, err
		}

		if err := add(utils.InferEnhancedTool("stop_article", "关键证据缺失或来源不支持完整图文时，说明具体原因和实际证据，请求明确停止并保留成果；不能用无关图片凑成功。", func(context.Context, loopCannotCompleteInput) (*schema.ToolResult, error) { return nil, nil })); err != nil {
			return nil, err
		}
	}
	infos := []*schema.ToolInfo{}
	for _, t := range tools {
		info, err := t.Info(context.Background())
		if err != nil {
			return nil, err
		}
		v2 := s.cp.ExecutionContract != nil && s.cp.ExecutionContract.Version == domain.ArticleLoopV2
		if v2 && info.Name == "stop_article" {
			info.Desc = "关键内容确实缺少证据、无法忠实成文时停止并保留成果。缺少可用解释配图不构成停止原因，允许少图或无图完成文章。"
		}
		if v2 && info.Name == "view_frames" {
			info.Desc += " 概览之后最多两个新窗口；只为具体来源线索或必要视觉问题取帧，不盲目找配图。无有用画面时继续正文，允许无图完成。"
		}
		if v2 && info.Name == loopSaveTool {
			info.Desc += " 可在一次原子修订中修改正文、删除和排序；组合行动任一部分无效则整次不保存。order必须包含操作后所有保留章节ID，不能遗漏未修改章节；同一章不能同时替换和删除。"
		}
		if info.Name == loopSaveTool || v2 {
			// The semantic input type owns the shape; the canonical domain owns
			// the accepted limitation vocabulary. Publish that same vocabulary
			// in the actual wire schema, rather than rejecting hidden enum values.
			sc, err := info.ParamsOneOf.ToJSONSchema()
			if err != nil {
				return nil, err
			}
			var root map[string]any
			if err = json.Unmarshal(domain.JSON(sc), &root); err != nil {
				return nil, err
			}
			if info.Name == loopSaveTool {
				loopLimitationSchema(root)
				if v2 {
					loopSemanticSchema(root)
				}
			}
			if v2 {
				props, _ := root["properties"].(map[string]any)
				if props == nil {
					props = map[string]any{}
					root["properties"] = props
				}
				if info.Name == loopSaveTool {
					if order, ok := props["order"].(map[string]any); ok {
						order["description"] = "All retained stable section IDs exactly once after this action, including unchanged chapters. May combine with replacements/deletions atomically. For new chapters use a separate later reorder after receiving stable IDs."
					}
				}
				props["activity_title"] = map[string]any{"type": "string", "maxLength": 60, "description": "面向用户的本次具体动作小标题，放在第一个字段；点明正在处理的主题、章节或画面，不宣称保存成功或核验通过。"}
				required, _ := root["required"].([]any)
				root["required"] = append(required, "activity_title")
			}
			var projected jsonschema.Schema
			if err = json.Unmarshal(domain.JSON(root), &projected); err != nil {
				return nil, err
			}
			info.ParamsOneOf = schema.NewParamsOneOfByJSONSchema(&projected)
		}
		s.tools[info.Name] = t
		infos = append(infos, info)
	}
	return infos, nil
}

// Express the same sum type the canonical constructor checks. Merely listing
// optional fields invited complete but conflicting text/background objects and
// source arrays in places that never accepted them.
func loopSemanticSchema(node map[string]any) {
	props, _ := node["properties"].(map[string]any)
	if props != nil {
		node["additionalProperties"] = false
	}
	if props["key"] != nil && props["section_id"] != nil && props["content"] != nil {
		node["oneOf"] = []any{
			map[string]any{"required": []string{"key"}, "not": map[string]any{"required": []string{"section_id"}}},
			map[string]any{"required": []string{"section_id"}, "not": map[string]any{"required": []string{"key"}}},
		}
		for _, name := range []string{"key", "section_id"} {
			props[name].(map[string]any)["minLength"] = 1
		}
	}
	if props["background"] != nil && props["figure"] != nil && props["quote"] != nil {
		constructors := []string{}
		for name := range props {
			if name != "sources" {
				constructors = append(constructors, name)
			}
		}
		slices.Sort(constructors) // Stable wire identity across recovery.
		variants := []any{}
		for _, name := range constructors {
			required := []string{name}
			forbidden := []any{}
			for _, other := range constructors {
				if other != name {
					forbidden = append(forbidden, map[string]any{"required": []string{other}})
				}
			}
			if name == "text" || name == "quote" || name == "code" {
				required = append(required, "sources")
			} else {
				forbidden = append(forbidden, map[string]any{"required": []string{"sources"}})
			}
			variants = append(variants, map[string]any{"required": required, "not": map[string]any{"anyOf": forbidden}})
		}
		node["oneOf"] = variants
	}
	if sources, ok := props["sources"].(map[string]any); ok {
		sources["minItems"] = 1
	}
	for _, value := range node {
		switch value := value.(type) {
		case map[string]any:
			loopSemanticSchema(value)
		case []any:
			for _, item := range value {
				if child, ok := item.(map[string]any); ok {
					loopSemanticSchema(child)
				}
			}
		}
	}
}

func loopLimitationSchema(node map[string]any) {
	props, _ := node["properties"].(map[string]any)
	if props["code"] != nil && props["message"] != nil && props["sources"] != nil {
		props["code"].(map[string]any)["enum"] = domain.LimitationCodes()
	}
	for _, value := range node {
		switch value := value.(type) {
		case map[string]any:
			loopLimitationSchema(value)
		case []any:
			for _, item := range value {
				if child, ok := item.(map[string]any); ok {
					loopLimitationSchema(child)
				}
			}
		}
	}
}
func (s *session) loadLoopDraft(ctx context.Context) (store.Draft, articleLoopDraft, error) {
	saved, err := s.engine.Store.ArticleDraft(ctx, s.claim.Run.ID)
	d := articleLoopDraft{Article: domain.Article{SchemaVersion: 1, SnapshotID: s.snapshot.ID, Source: s.snapshot.Source, DetailLevel: s.spec.DetailLevel, DetailPolicyRevision: s.spec.DetailPolicyRevision, Quality: "standard", Sections: []domain.Section{}, Citations: map[string]domain.Citation{}, Limitations: []domain.Limitation{}}, Notes: map[string]loopFrameNote{}}
	if err == nil && saved.Version > 0 {
		err = json.Unmarshal(saved.Data, &d)
	}
	if d.Notes == nil {
		d.Notes = map[string]loopFrameNote{}
	}
	return saved, d, err
}
func (s *session) loopCatalog(ctx context.Context) (map[string]domain.Citation, error) {
	out := map[string]domain.Citation{}
	for i, t := range s.transcript {
		alias := fmt.Sprint("t", i+1)
		if !slices.Contains(s.cp.ArticleLoop.ReadSources, alias) {
			continue
		}
		start, end := max(int64(0), t.StartMS), min(s.snapshot.DurationMS, t.EndMS)
		if start >= end {
			continue
		}
		out[alias] = domain.Citation{ID: alias, Kind: "video_text", Label: "视频原文", SnapshotID: s.snapshot.ID, SegmentIDs: []string{t.ID}, TextOrigin: t.Origin, Range: &domain.Range{StartMS: start, EndMS: end}}
	}
	for i, id := range s.cp.ArticleLoop.Frames {
		if !slices.Contains(s.cp.ArticleLoop.SeenFrames, id) {
			continue
		}
		a, err := s.engine.Store.InternalFrame(ctx, s.claim, id)
		if err != nil {
			return nil, err
		}
		if a.TimeMS == nil || a.State != "ready" {
			return nil, invalid("图片不可用")
		}
		alias := fmt.Sprint("f", i+1)
		out[alias] = domain.Citation{ID: alias, Kind: "video_frame", Label: "视频画面", SnapshotID: s.snapshot.ID, FrameID: id, TimeMS: a.TimeMS}
	}
	return out, nil
}
func loopCanonicalSection(in loopSection, id string, catalog map[string]domain.Citation, transcript []domain.Segment) (domain.Section, map[string]domain.Citation, map[string]loopFrameNote, []domain.Limitation, error) {
	out := domain.Section{ID: id, Title: strings.TrimSpace(in.Title), Blocks: []domain.Block{}}
	citations := map[string]domain.Citation{}
	notes := map[string]loopFrameNote{}
	limits := []domain.Limitation{}
	fail := func(reason string) (domain.Section, map[string]domain.Citation, map[string]loopFrameNote, []domain.Limitation, error) {
		return out, nil, nil, nil, errors.New(reason)
	}
	if in.rejected != "" {
		return fail(in.rejected)
	}
	if out.Title == "" || len(in.Content) == 0 || len(in.Content) > 400 {
		return fail("title and substantive content required; at most 400 blocks")
	}
	refs := func(ids []string) error {
		if len(ids) == 0 {
			return errors.New("video facts require nearby sources")
		}
		for _, ref := range ids {
			c, ok := catalog[ref]
			if !ok {
				return fmt.Errorf("source %s was not delivered", ref)
			}
			citations[ref] = c
		}
		return nil
	}
	inline := func(text string, ids []string) []domain.Inline {
		v := []domain.Inline{{Type: "text", Text: text}}
		for _, ref := range ids {
			v = append(v, domain.Inline{Type: "citation", CitationID: ref})
		}
		return v
	}
	for i, c := range in.Content {
		count := 0
		for _, present := range []bool{c.Text != nil, c.Quote != nil, c.List != nil, c.Code != nil, c.Table != nil, c.Figure != nil, c.Background != nil} {
			if present {
				count++
			}
		}
		if count != 1 {
			return fail(fmt.Sprintf("/content/%d: exactly one semantic constructor required", i))
		}
		b := domain.Block{ID: fmt.Sprintf("%s_b%d", id, i+1), Origin: "video", CitationIDs: append([]string{}, c.Sources...)}
		switch {
		case c.Text != nil || c.Quote != nil || c.Background != nil:
			value := ""
			b.Type = "paragraph"
			if c.Text != nil {
				value = *c.Text
			}
			if c.Quote != nil {
				value = *c.Quote
				b.Type = "quote"
			}
			if c.Background != nil {
				value = *c.Background
				b.Origin = "background_explanation"
			}
			if strings.TrimSpace(value) == "" {
				return fail("empty text")
			}
			if b.Origin == "video" {
				if e := refs(c.Sources); e != nil {
					return fail(fmt.Sprintf("/content/%d/sources: %s", i, e))
				}
			} else if len(c.Sources) > 0 {
				return fail("background explanation must be separate from video claims")
			}
			if c.Quote != nil {
				// Exact quotes are checked against the continuous original, not the model's summary.
				originals := []string{}
				segments := []string{}
				lastOrdinal := -1
				var first, last domain.Citation
				for n, ref := range c.Sources {
					cit := catalog[ref]
					if cit.Kind != "video_text" {
						return fail("quote needs original text sources")
					}
					found := false
					for _, t := range transcript {
						if slices.Contains(cit.SegmentIDs, t.ID) {
							if n > 0 && t.Ordinal != lastOrdinal+1 {
								return fail("quote source is not continuous")
							}
							lastOrdinal = t.Ordinal
							originals = append(originals, t.Text)
							segments = append(segments, t.ID)
							found = true
						}
					}
					if !found {
						return fail("quote source unavailable")
					}
					if n == 0 {
						first = cit
					}
					last = cit
				}
				if !strings.Contains(domain.NormalizeQuote(strings.Join(originals, "\n")), domain.NormalizeQuote(value)) {
					return fail("quote is not a continuous verbatim original")
				}
				q := first
				q.ID = "q_" + store.Hash(map[string]any{"text": value, "sources": c.Sources})[:16]
				q.SegmentIDs = segments
				q.Range = &domain.Range{StartMS: first.Range.StartMS, EndMS: last.Range.EndMS}
				q.Quote = domain.Ptr(value)
				citations[q.ID] = q
				b.CitationIDs = []string{q.ID}
			}
			b.Content = inline(value, b.CitationIDs)
		case c.List != nil:
			b.Type = "list"
			b.Ordered = domain.Ptr(c.List.Ordered)
			b.CitationIDs = nil
			if len(c.List.Items) == 0 || len(c.Sources) > 0 {
				return fail("list items need their own sources")
			}
			for j, item := range c.List.Items {
				if strings.TrimSpace(item.Text) == "" {
					return fail("empty list item")
				}
				if e := refs(item.Sources); e != nil {
					return fail(fmt.Sprintf("/content/%d/list/items/%d/sources: %s", i, j, e))
				}
				b.Items = append(b.Items, domain.ListItem{ID: fmt.Sprintf("%s_i%d", b.ID, j+1), Content: inline(item.Text, item.Sources)})
			}
		case c.Code != nil:
			if e := refs(c.Sources); e != nil {
				return fail(fmt.Sprintf("/content/%d/sources: %s", i, e))
			}
			if strings.TrimSpace(c.Code.Text) == "" {
				return fail("empty code")
			}
			b.Type = "code"
			b.Text = c.Code.Text
			b.Language = domain.Ptr(c.Code.Language)
		case c.Table != nil:
			b.Type = "table"
			b.Columns = c.Table.Columns
			b.CitationIDs = nil
			if len(b.Columns) == 0 || len(c.Table.Rows) == 0 || len(c.Sources) > 0 {
				return fail("table rows need their own sources")
			}
			for j, row := range c.Table.Rows {
				if len(row.Cells) != len(b.Columns) {
					return fail("table columns mismatch")
				}
				if e := refs(row.Sources); e != nil {
					return fail(fmt.Sprintf("/content/%d/table/rows/%d/sources: %s", i, j, e))
				}
				b.Rows = append(b.Rows, domain.TableRow{Cells: row.Cells, CitationIDs: row.Sources})
			}
		case c.Figure != nil:
			f := c.Figure
			b.Type = "figure"
			b.Layout = "single"
			b.Caption = f.Caption
			b.CitationIDs = append([]string{}, f.Frames...)
			if len(f.Frames) > 1 {
				b.Layout = "sequence"
			}
			if len(f.Frames) == 0 || strings.TrimSpace(f.Caption) == "" || len(f.VisibleFacts) == 0 || len(c.Sources) > 0 {
				return fail("figure needs actual frames, caption and visible facts")
			}
			if e := refs(f.Frames); e != nil {
				return fail(fmt.Sprintf("/content/%d/figure/frames: %s", i, e))
			}
			last := int64(-1)
			seen := map[string]bool{}
			for _, ref := range f.Frames {
				cit := catalog[ref]
				if cit.Kind != "video_frame" || cit.TimeMS == nil || *cit.TimeMS < last || seen[ref] {
					return fail("figure identities/time order invalid")
				}
				last = *cit.TimeMS
				seen[ref] = true
				b.FrameIDs = append(b.FrameIDs, cit.FrameID)
				notes[cit.FrameID] = loopFrameNote{VisibleFacts: f.VisibleFacts, Uncertainty: f.Uncertainty}
			}
		}
		out.Blocks = append(out.Blocks, b)
	}
	for _, observation := range in.Observations {
		cit, ok := catalog[observation.Frame]
		if !ok || cit.Kind != "video_frame" || len(observation.VisibleFacts) == 0 {
			return fail("observation needs delivered pixels and visible facts")
		}
		notes[cit.FrameID] = loopFrameNote{VisibleFacts: observation.VisibleFacts, Uncertainty: observation.Uncertainty}
	}

	for _, l := range in.Limitations {
		if strings.TrimSpace(l.Message) == "" {
			return fail("limitation message must describe the specific scoped uncertainty")
		}
		if !slices.Contains(domain.LimitationCodes(), l.Code) {
			return fail(fmt.Sprintf("limitation code %q must be one of %v", l.Code, domain.LimitationCodes()))
		}
		for _, ref := range l.Sources {
			cit, ok := catalog[ref]
			if !ok {
				return fail("limitation source not delivered")
			}
			citations[ref] = cit
		}
		limits = append(limits, domain.Limitation{Code: l.Code, Message: l.Message, SectionIDs: []string{id}, CitationIDs: l.Sources})
	}
	return out, citations, notes, limits, nil
}

func (s *session) saveLoopSections(ctx context.Context, key string, in loopSaveInput) (any, bool, error) {
	// Replay the original receipt before comparing to today's version.
	if old, err := s.engine.Store.Operation(ctx, s.claim.Run.ID, "article-draft:"+key); err == nil && old.Status == "completed" {
		if old.ArgsHash != store.Hash(in) {
			return nil, false, domain.ErrFenced
		}
		var saved store.Draft
		var d articleLoopDraft
		if err = json.Unmarshal(old.Result, &saved); err != nil {
			return nil, false, err
		}
		if err = json.Unmarshal(saved.Data, &d); err != nil {
			return nil, false, err
		}
		return map[string]any{"version": saved.Version, "receipt": d.BatchReceipt, "noop": saved.Noop}, !saved.Noop, nil
	} else if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, false, err
	}
	saved, d, err := s.loadLoopDraft(ctx)
	if err != nil {
		return nil, false, err
	}
	if saved.Version != in.ExpectedVersion {
		return nil, false, domain.Err(409, "DRAFT_VERSION_CONFLICT", "草稿版本已变化；有效执行者需要重新读取。")
	}
	if len(in.Sections) > 40 || len(domain.JSON(in)) > loopWriteBytes {
		return map[string]any{"error": "resource_limit", "limit": "1–40 sections and 128 KiB per write; split this action"}, false, nil
	}
	catalog, err := s.loopCatalog(ctx)
	if err != nil {
		return nil, false, err
	}
	receipt := &loopReceipt{Accepted: []string{}, Created: map[string]string{}, Rejected: []loopRejection{}}
	seen := map[string]bool{}
	for _, sec := range in.Sections {
		identity := sec.SectionID
		if identity == "" {
			identity = "key:" + sec.Key
		}
		if identity == "key:" {
			continue
		}
		if seen[identity] || sec.SectionID != "" && sec.Key != "" {
			return map[string]any{"error": "ambiguous_section_identity", "identity": identity}, false, nil
		}
		seen[identity] = true
	}
	structural := len(in.DeleteIDs) > 0 || len(in.Order) > 0
	if structural && len(in.Sections) > 0 && s.cp.ExecutionContract.Version != domain.ArticleLoopV2 {
		return map[string]any{"error": "structural edits must be one atomic action separate from section writes"}, false, nil
	}
	for _, sec := range in.Sections {
		if sec.SectionID != "" && slices.Contains(in.DeleteIDs, sec.SectionID) {
			return map[string]any{"error": "cannot delete and replace the same section", "id": sec.SectionID}, false, nil
		}
	}
	for i, insec := range in.Sections {
		id := insec.SectionID
		if insec.rejected != "" || insec.SectionID == "" && insec.Key == "" {
			reason := insec.rejected
			if reason == "" {
				reason = "creation key or stable section_id is required"
			}
			receipt.Rejected = append(receipt.Rejected, loopRejection{Path: fmt.Sprintf("/sections/%d", i), Reason: reason})
			continue
		}
		index := -1
		for j, sec := range d.Article.Sections {
			if sec.ID == id {
				index = j
			}
		}
		reject := func(e error) {
			receipt.Rejected = append(receipt.Rejected, loopRejection{Path: fmt.Sprintf("/sections/%d", i), Reason: e.Error()})
		}
		if id != "" && index < 0 {
			reject(errors.New("unknown stable section_id"))
			continue
		}
		if id == "" {
			id = "s_" + store.Hash(map[string]string{"run": s.claim.Run.ID, "key": insec.Key})[:20]
			for _, sec := range d.Article.Sections {
				if sec.ID == id {
					index = -2
				}
			}
			if index == -2 {
				reject(errors.New("creation key already exists; use stable section_id"))
				continue
			}
		}
		sec, cits, notes, limits, e := loopCanonicalSection(insec, id, catalog, s.transcript)
		if e == nil {
			e = s.validateBlocks(ctx, sec.Blocks, cits, true)
		}
		if e != nil {
			reject(e)
			continue
		}
		if index >= 0 {
			d.Article.Sections[index] = sec
		} else {
			d.Article.Sections = append(d.Article.Sections, sec)
			receipt.Created[insec.Key] = id
		}
		kept := []domain.Limitation{}
		for _, l := range d.Article.Limitations {
			if !slices.Contains(l.SectionIDs, id) {
				kept = append(kept, l)
			}
		}
		d.Article.Limitations = append(kept, limits...)
		for alias, cit := range cits {
			d.Article.Citations[alias] = cit
		}
		for frame, note := range notes {
			d.Notes[frame] = note
		}
		receipt.Accepted = append(receipt.Accepted, id)
	}
	if in.Title != nil {
		if strings.TrimSpace(*in.Title) == "" {
			receipt.Rejected = append(receipt.Rejected, loopRejection{Path: "/title", Reason: "empty title"})
		} else {
			d.Article.Title = *in.Title
		}
	}
	if len(in.DeleteIDs) > 0 || len(in.Order) > 0 {
		if len(receipt.Rejected) > 0 {
			return map[string]any{"error": "atomic revision rejected; no changes saved", "rejected": receipt.Rejected}, false, nil
		}
		available := map[string]domain.Section{}
		for _, sec := range d.Article.Sections {
			available[sec.ID] = sec
		}
		for _, id := range in.DeleteIDs {
			if _, ok := available[id]; !ok {
				return map[string]any{"error": "unknown delete identity", "id": id}, false, nil
			}
			delete(available, id)
		}
		order := in.Order
		if len(order) == 0 {
			for _, sec := range d.Article.Sections {
				if _, ok := available[sec.ID]; ok {
					order = append(order, sec.ID)
				}
			}
		}
		if len(order) != len(available) {
			return map[string]any{"error": "order must contain every retained identity"}, false, nil
		}
		next := []domain.Section{}
		for _, id := range order {
			sec, ok := available[id]
			if !ok {
				return map[string]any{"error": "duplicate/unknown reorder identity"}, false, nil
			}
			next = append(next, sec)
			delete(available, id)
		}
		d.Article.Sections = next
		receipt.Accepted = slices.Clone(order)
	}
	if len(d.Article.Sections) > 40 {
		return map[string]any{"error": "resource_limit", "limit": "40 total sections"}, false, nil
	}
	// Keep exactly the content dependencies, including scoped limitations.
	used := map[string]bool{}
	sections := map[string]bool{}
	for _, sec := range d.Article.Sections {
		sections[sec.ID] = true
		for _, b := range sec.Blocks {
			for _, id := range blockCitationIDs(b) {
				used[id] = true
			}
		}
	}
	kept := []domain.Limitation{}
	for _, l := range d.Article.Limitations {
		ok := len(l.SectionIDs) == 0
		for _, id := range l.SectionIDs {
			ok = ok || sections[id]
		}
		if ok {
			kept = append(kept, l)
			for _, id := range l.CitationIDs {
				used[id] = true
			}
		}
	}
	d.Article.Limitations = kept
	d.Article.Quality = "standard"
	if len(kept) > 0 {
		d.Article.Quality = "with_limitations"
	}
	for id := range d.Article.Citations {
		if !used[id] {
			delete(d.Article.Citations, id)
		}
	}
	d.BatchReceipt = receipt
	if len(domain.JSON(d)) > 512<<10 {
		return map[string]any{"error": "resource_limit", "limit": "512 KiB retained draft; shorten/split without omitting required topics"}, false, nil
	}
	result, err := s.engine.Store.SaveArticleDraft(ctx, s.claim, key, in, saved.Version, d)
	return map[string]any{"version": result.Version, "receipt": receipt, "noop": result.Noop}, err == nil && !result.Noop, err
}
