package investigator

// The source index is a projection of the immutable transcript. Notes are
// navigation aids, never a replacement for original text or actual pixels.
import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
	"github.com/cloudwego/eino/schema"
	"vidlens/internal/domain"
	"vidlens/internal/store"
)

const sourceIndexProtocol = domain.SourceIndexProtocolV1
const sourceContextName = "vidlens_source_index"
const sourceContextInstructions = `来源以时间索引、分段材料卡和按需原文提供。source_index中的摘要是派生导航，不是原文或画面证据；引用只能使用原始tN/fN。source_survey_pending时先通读current_chunk的全部原文，仅调用record_source_notes保存本段主要主题、数字/条件、转折和必要细节及实际evidence_ids；无需配图，也不要开始写文章，服务器随后交付下一段。所有段落完成后再据全片材料卡规划文章；不能只选与purpose匹配的片段。写具体断言、精确引述或遇到含糊信息时用read_source回读对应chunk_ids/segment_ids及邻居。read_source有next_offset时跟随分页，has_more不是已读完。图片分批交付，先保存本批观察，后续概览自动到达；已存档图片可按frame_id重载，未送达的图不能描述。`

type sourcePassage struct {
	domain.Segment
	Alias      string `json:"citation_alias"`
	TextOffset int    `json:"text_offset"`
	TextEnd    int    `json:"text_end"`
}

type sourceChunk struct {
	ID                string          `json:"id"`
	StartMS           int64           `json:"start_ms"`
	EndMS             int64           `json:"end_ms"`
	Passages          []sourcePassage `json:"-"`
	NoteMaxCharacters int             `json:"-"`
}

type sourceNote struct {
	ChunkID     string   `json:"chunk_id"`
	Summary     string   `json:"summary" jsonschema:"description=30-1600 characters in Chinese, preserving the current chunk's main points, concrete numbers, conditions and transitions. Derived navigation only; no visual guesses."`
	EvidenceIDs []string `json:"evidence_ids" jsonschema:"description=1-12 original tN aliases actually present in current_chunk, supporting its key points. Never cite another chunk or a frame."`
}

type indexedReadInput struct {
	readInput
	ChunkIDs []string `json:"chunk_ids,omitempty"`
	Offset   int      `json:"offset,omitempty" jsonschema:"description=Zero for first page; copy next_offset with the same selection for subsequent pages. Counts source passages including slices of long cues."`
}

func (s *session) boundedSourceContext() bool {
	return s.cp.ExecutionContract != nil && (s.cp.ExecutionContract.Version == domain.ExecutionContractV4 || s.cp.ExecutionContract.Version == domain.ExecutionContractV5)
}

func (s *session) chunkedSource() bool {
	return s.boundedSourceContext() && utf8.RuneCount(domain.JSON(s.transcript)) > max(8000, min(60000, separateInputLimit(s.profile)/3))
}

func (s *session) sourceChunks() []sourceChunk {
	if s.chunks == nil {
		s.chunks = buildSourceChunks(s.transcript, max(3000, min(24000, separateInputLimit(s.profile)/4)))
		if s.adaptiveSourceCards() {
			for i := range s.chunks {
				characters := 0
				for _, passage := range s.chunks[i].Passages {
					characters += utf8.RuneCountInString(passage.Text)
				}
				// Keep every valid V4 card reusable. Dense originals may need a
				// larger navigation card, still bounded by the complete ledger.
				s.chunks[i].NoteMaxCharacters = max(1600, min(4000, characters/6))
			}
		}
	}
	return s.chunks
}

// Every original rune is represented exactly once. A long ASR cue is sliced
// without inventing finer timestamps or changing its canonical citation ID.
func buildSourceChunks(transcript []domain.Segment, limit int) []sourceChunk {
	chunks := []sourceChunk{}
	current := sourceChunk{}
	chars := 0
	flush := func() {
		if len(current.Passages) == 0 {
			return
		}
		current.ID = "c_" + store.Hash(current.Passages)[:20]
		chunks = append(chunks, current)
		current, chars = sourceChunk{}, 0
	}
	for i, segment := range transcript {
		text := []rune(segment.Text)
		for start := 0; start < len(text) || start == 0; {
			end := min(len(text), start+max(1, limit/2))
			part := sourcePassage{Segment: segment, Alias: fmt.Sprint("t", i+1), TextOffset: start, TextEnd: end}
			part.Text = string(text[start:end])
			cost := utf8.RuneCount(domain.JSON(part))
			if chars+cost > limit {
				flush()
			}
			if len(current.Passages) == 0 {
				current.StartMS, current.EndMS = part.StartMS, part.EndMS
			}
			current.StartMS = min(current.StartMS, part.StartMS)
			current.EndMS = max(current.EndMS, part.EndMS)
			current.Passages = append(current.Passages, part)
			chars += cost
			if end == len(text) {
				break
			}
			start = end
		}
	}
	flush()
	return chunks
}

func (s *session) sourceSurveyPending() bool {
	return s.turn == nil && s.chunkedSource() && len(s.cp.SourceNotes) < len(s.sourceChunks())
}

func (s *session) sourceContext() map[string]any {
	out := map[string]any{"snapshot_id": s.snapshot.ID, "source": s.snapshot.Source, "duration_ms": s.snapshot.DurationMS, "text_time_precision": "Original cue or coarse ASR windows, never invented sentence timestamps."}
	if !s.chunkedSource() {
		out["transcript"] = s.transcript
		return out
	}
	chunks := s.sourceChunks()
	s.cp.SourceIndexProtocol = sourceIndexProtocol
	out["source_index_protocol"] = sourceIndexProtocol
	out["source_index"] = chunks
	out["derived_notes_not_evidence"] = s.cp.SourceNotes
	out["source_survey_pending"] = s.sourceSurveyPending()
	out["coverage"] = map[string]any{"total_chunks": len(chunks), "notes_saved": len(s.cp.SourceNotes), "original_segment_count": len(s.transcript)}
	if s.sourceSurveyPending() {
		current := chunks[len(s.cp.SourceNotes)]
		s.cp.SourceChunkDelivered = current.ID
		out["current_chunk"] = map[string]any{"id": current.ID, "start_ms": current.StartMS, "end_ms": current.EndMS, "passages": current.Passages}
		if s.adaptiveSourceCards() {
			out["source_card_constraints"] = s.sourceCardLimits(current)
		}
	} else if s.turn != nil {
		// Questions start with relevance-ranked original passages and can widen
		// the search. They never pay for a fresh whole-video summary survey.
		out["question_source_candidates"] = sourceSearch(s.transcript, s.turn.Question, nil, nil)
	}
	return out
}

func (s *session) sourceNotesTool(add func(tool.EnhancedInvokableTool, error) error) error {
	if !s.boundedSourceContext() {
		return nil
	}
	return add(utils.InferEnhancedTool("record_source_notes", "Save a derived navigation card for the delivered current_chunk. Process every chunk in chronological order before planning a complete article. This does not establish visual facts or replace source evidence.", func(ctx context.Context, in sourceNote) (*schema.ToolResult, error) {
		for _, note := range s.cp.SourceNotes {
			if note.ChunkID == in.ChunkID {
				return textResult(map[string]any{"saved": true, "already_saved": true, "immutable": true, "ignored_update": store.Hash(note) != store.Hash(in), "chunk_id": note.ChunkID, "next_action": "Saved cards are read-only. Process only the delivered current_chunk; a repeated card does not advance source coverage."}), nil
			}
		}
		if !s.sourceSurveyPending() {
			return toolError("全片材料卡已完成，无需再写。")
		}
		chunk := s.sourceChunks()[len(s.cp.SourceNotes)]
		if in.ChunkID != s.cp.SourceChunkDelivered || in.ChunkID != chunk.ID {
			return s.sourceNoteFeedback(in, chunk, "本段原文尚未交付；只处理当前current_chunk，等待下一次模型调用交付新段。"), nil
		}
		if err := validateSourceNote(in, chunk); err != nil {
			return s.sourceNoteFeedback(in, chunk, err.Error()), nil
		}
		if s.adaptiveSourceCards() && (utf8.RuneCountInString(in.Summary) > s.sourceCardLimits(chunk).Maximum || !s.sourceCardFitsNavigation(in)) {
			return s.sourceNoteFeedback(in, chunk, "当前材料卡超过全片导航剩余预算；保留原文，只压缩这张派生导航卡。"), nil
		}
		s.cp.SourceNotes = append(s.cp.SourceNotes, in)
		if err := s.persist(ctx, s.cp.Messages); err != nil {
			return nil, err
		}
		return textResult(map[string]any{"saved": true, "chunk_id": in.ChunkID, "remaining_chunks": len(s.sourceChunks()) - len(s.cp.SourceNotes), "next_action": "The server delivers the next chunk or overview pixels before the next model call."}), nil
	}))
}

func (s *session) sourceNoteFeedback(in sourceNote, chunk sourceChunk, reason string) *schema.ToolResult {
	aliases := []string{}
	for _, passage := range chunk.Passages {
		if !slices.Contains(aliases, passage.Alias) {
			aliases = append(aliases, passage.Alias)
		}
	}
	feedback := map[string]any{"error": reason, "current_chunk_id": chunk.ID, "supplied_chunk_id": in.ChunkID, "summary_characters": utf8.RuneCountInString(in.Summary), "evidence_count": len(in.EvidenceIDs), "summary_min_characters": 30, "summary_max_characters": 1600, "evidence_min_items": 1, "evidence_max_items": 12, "allowed_evidence_ids": aliases, "next_action": "Correct this current card in one call. Select up to 12 representative original aliases; do not rewrite saved cards or submit undelivered chunks."}
	if s.adaptiveSourceCards() {
		limits := s.sourceCardLimits(chunk)
		feedback["summary_max_characters"] = limits.Maximum
		feedback["summary_target_characters"] = limits.Target
		feedback["summary_excess_characters"] = max(0, utf8.RuneCountInString(in.Summary)-limits.Maximum)
		feedback["navigation_budget"] = limits
		feedback["navigation_candidate_characters"] = utf8.RuneCount(domain.JSON(append(append([]sourceNote{}, s.cp.SourceNotes...), in)))
		feedback["next_action"] = fmt.Sprintf("只改当前卡：将摘要重写至约%d字、最多%d字，保留主题、关键数字/条件和转折，合并重复解释；原文完整保留，细节可用read_source回读。只选1–12个不重复的本段代表性引用，不重写旧卡，不提交未交付段。", limits.Target, limits.Maximum)
	}
	return textResult(feedback)
}

func validateSourceNote(note sourceNote, chunk sourceChunk) error {
	limit := sourceNoteMaximum(chunk)
	if note.ChunkID != chunk.ID || utf8.RuneCountInString(strings.TrimSpace(note.Summary)) < 30 || utf8.RuneCountInString(note.Summary) > limit || len(note.EvidenceIDs) < 1 || len(note.EvidenceIDs) > 12 {
		return fmt.Errorf("请为当前chunk_id保存30–%d字材料卡及1–12个该段原文引用。", limit)
	}
	for i, id := range note.EvidenceIDs {
		if slices.Contains(note.EvidenceIDs[:i], id) {
			return fmt.Errorf("材料卡引用%s重复；只选择不重复的代表性原文引用。", id)
		}
		if !slices.ContainsFunc(chunk.Passages, func(p sourcePassage) bool { return p.Alias == id }) {
			return fmt.Errorf("材料卡引用%s不属于当前交付的原文。", id)
		}
	}
	return nil
}

func sourceTerms(query string) []string {
	terms := strings.FieldsFunc(strings.ToLower(query), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsNumber(r) })
	out := []string{}
	for _, term := range terms {
		runes := []rune(term)
		if len(runes) < 2 {
			continue
		}
		if slices.ContainsFunc(runes, func(r rune) bool { return unicode.Is(unicode.Han, r) }) && len(runes) > 2 {
			for i := 0; i+1 < len(runes); i++ {
				out = append(out, string(runes[i:i+2]))
			}
		} else {
			out = append(out, term)
		}
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// BM25-style term-frequency saturation and inverse document frequency avoid
// the old first-twelve-OR-matches bias. All selection stays inside this snapshot.
func sourceSearch(transcript []domain.Segment, query string, start, end *int64) []sourcePassage {
	terms := sourceTerms(query)
	type hit struct {
		index int
		score float64
	}
	texts := make([]string, len(transcript))
	for i, seg := range transcript {
		texts[i] = strings.ToLower(seg.Text)
	}
	df := make(map[string]int, len(terms))
	for _, term := range terms {
		for _, text := range texts {
			if strings.Contains(text, term) {
				df[term]++
			}
		}
	}
	hits := []hit{}
	for i, seg := range transcript {
		if start != nil && seg.EndMS <= *start || end != nil && seg.StartMS >= *end {
			continue
		}
		score := 0.0
		for _, term := range terms {
			frequency := strings.Count(texts[i], term)
			if frequency > 0 {
				score += math.Log(1+(float64(len(transcript)-df[term])+0.5)/(float64(df[term])+0.5)) * float64(frequency) / float64(frequency+1)
			}
		}
		if score > 0 || len(terms) == 0 {
			hits = append(hits, hit{i, score})
		}
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].score > hits[j].score })
	out := []sourcePassage{}
	chars := 0
	for _, h := range hits[:min(12, len(hits))] {
		seg := transcript[h.index]
		text := []rune(seg.Text)
		seg.Text = string(text[:min(1200, len(text))])
		p := sourcePassage{Segment: seg, Alias: fmt.Sprint("t", h.index+1), TextEnd: len([]rune(seg.Text))}
		cost := utf8.RuneCount(domain.JSON(p))
		if chars+cost > 16000 {
			break
		}
		out = append(out, p)
		chars += cost
	}
	return out
}

func (s *session) indexedSourceTools(add func(tool.EnhancedInvokableTool, error) error) error {
	if err := add(utils.InferEnhancedTool("find_evidence", "Rank candidate original transcript positions by distinctive keywords and/or time in this video. Try translated terms/synonyms when recall is poor; candidates are not confirmed evidence. Read exact text and neighbors with read_source.", func(ctx context.Context, in findInput) (*schema.ToolResult, error) {
		return s.step(ctx, publicTitle(in.ActivityTitle, "正在定位视频原文"), s.phase(), nil, func() (any, []string, error) {
			return map[string]any{"snapshot_id": s.snapshot.ID, "candidates": sourceSearch(s.transcript, in.Query, in.StartMS, in.EndMS), "retrieval": "lexical_tf_idf", "note": "Candidate slices only. read_source provides complete paginated text with chronological neighbors."}, nil, nil
		})
	})); err != nil {
		return err
	}
	return add(utils.InferEnhancedTool("read_source", "Read exact original passages by chunk_ids, segment_ids (canonical IDs or tN aliases), or time range, with adjacent segments. Follow next_offset using the same selection. Long cues retain original timing and use text_offset/text_end slices; all text remains retrievable.", func(ctx context.Context, in indexedReadInput) (*schema.ToolResult, error) {
		return s.step(ctx, publicTitle(in.ActivityTitle, "正在回读视频原文"), s.phase(), nil, func() (any, []string, error) { return s.sourcePage(in), nil, nil })
	}))
}

func (s *session) sourcePage(in indexedReadInput) map[string]any {
	indices := map[string]bool{}
	selected := map[string]bool{}
	for _, chunk := range s.sourceChunks() {
		if slices.Contains(in.ChunkIDs, chunk.ID) {
			for _, p := range chunk.Passages {
				selected[p.ID] = true
			}
		}
	}
	for i, seg := range s.transcript {
		match := selected[seg.ID] || slices.Contains(in.SegmentIDs, seg.ID) || slices.Contains(in.SegmentIDs, fmt.Sprint("t", i+1))
		if len(in.ChunkIDs) == 0 && len(in.SegmentIDs) == 0 && in.StartMS != nil && in.EndMS != nil {
			match = seg.StartMS < *in.EndMS && seg.EndMS > *in.StartMS
		}
		if match {
			for j := max(0, i-1); j <= min(len(s.transcript)-1, i+1); j++ {
				indices[s.transcript[j].ID] = true
			}
		}
	}
	passages := []sourcePassage{}
	for _, chunk := range s.sourceChunks() {
		for _, p := range chunk.Passages {
			if indices[p.ID] {
				passages = append(passages, p)
			}
		}
	}
	out := map[string]any{"snapshot_id": s.snapshot.ID, "segments": []sourcePassage{}, "has_more": false, "total_passages": len(passages)}
	if len(passages) == 0 {
		out["error"] = "没有匹配的原文；复制source_index中的chunk_ids、实际segment_ids/tN或有效时间范围。"
		out["valid_chunks"] = s.sourceChunks()
		return out
	}
	if in.Offset < 0 || in.Offset >= len(passages) && len(passages) > 0 {
		out["error"] = "offset超出选择范围，请从0或上一页next_offset继续。"
		return out
	}
	page := []sourcePassage{}
	chars := 0
	next := in.Offset
	for ; next < len(passages); next++ {
		cost := utf8.RuneCount(domain.JSON(passages[next]))
		if len(page) > 0 && chars+cost > 18000 {
			break
		}
		page = append(page, passages[next])
		chars += cost
	}
	out["segments"], out["has_more"] = page, next < len(passages)
	if next < len(passages) {
		out["next_offset"] = next
	}
	return out
}

// Forget old raw read bodies while preserving paired tool exchanges. The
// latest two reads remain exact; archived reads explicitly require reloading.
func projectIndexedSourceReads(messages []*schema.Message) []*schema.Message {
	reads := 0
	out := append([]*schema.Message{}, messages...)
	for i := len(out) - 1; i >= 0; i-- {
		m := out[i]
		if m.Role != schema.Tool || (m.ToolName != "read_source" && m.ToolName != "find_evidence") {
			continue
		}
		reads++
		if reads <= 2 {
			continue
		}
		copy := *m
		copy.UserInputMultiContent = nil
		copy.Content = `{"archived_source_read":true,"note":"Original text remains available through read_source; old result is not evidence in this context."}`
		out[i] = &copy
	}
	return out
}

func compactSourceCatalog(catalog map[string]domain.Citation, messages []*schema.Message, extra any) map[string]any {
	// Match complete JSON string tokens, never t1 as a prefix of t10. Exclude
	// the old catalog capsule to prevent its aliases retaining themselves.
	var raw strings.Builder
	for _, m := range messages {
		if m.Name != "vidlens_draft_state" && m.Name != "vidlens_answer_references" {
			raw.WriteString(sourceMessageText(m))
		}
	}
	raw.Write(domain.JSON(extra))
	visible := raw.String()
	out := map[string]any{}
	for alias, c := range catalog {
		if c.Kind != "video_text" || strings.Contains(visible, `"`+alias+`"`) || len(c.SegmentIDs) == 1 && strings.Contains(visible, `"`+c.SegmentIDs[0]+`"`) {
			out[alias] = c
		}
	}
	return out
}

func sourceMessageText(m *schema.Message) string {
	text := m.Content
	for _, p := range m.UserInputMultiContent {
		if p.Type == schema.ChatMessagePartTypeText {
			text += p.Text
		}
	}
	return text
}

func sourceAliases(value any) []string {
	var aliases []string
	var walk func(any)
	walk = func(v any) {
		switch x := v.(type) {
		case string:
			aliases = append(aliases, x)
		case []any:
			for _, y := range x {
				walk(y)
			}
		case map[string]any:
			for _, y := range x {
				walk(y)
			}
		}
	}
	var decoded any
	_ = json.Unmarshal(domain.JSON(value), &decoded)
	walk(decoded)
	slices.Sort(aliases)
	return slices.Compact(aliases)
}
