package mq

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"unicode"

	"vid-lens/internal/model"
	"vid-lens/internal/processing"
	"vid-lens/internal/repository"
	"vid-lens/internal/textsource"
	"vid-lens/internal/transcript"
)

const asrSourceParserVersion = "asr-observation-v1"

// publishAutomaticASRSource releases the ASR lease and freezes a summary input
// in the same transaction as its immutable, observed transcript source.
func (c *Consumer) publishAutomaticASRSource(ctx context.Context, task *model.VideoTask, intent processing.Intent, token, content string, retainedRows ...[]model.VideoTranscriptionChunk) error {
	if err := requireProcessingLease(ctx); err != nil {
		return err
	}
	var rows []model.VideoTranscriptionChunk
	var err error
	if len(retainedRows) > 0 {
		rows = retainedRows[0]
	} else {
		rows, err = c.repo.TranscriptionChunk.ListByTaskID(task.ID)
		if err != nil {
			return err
		}
	}
	snapshot, err := asrTextSourceSnapshot(task, content, rows)
	if err != nil {
		return err
	}
	var prepared *repository.InitialTaskDispatch
	lease := &repository.TaskProcessingLeaseRequest{TaskID: task.ID, JobType: model.TaskJobTypeTranscribe, Token: token, Now: c.currentTime()}
	_, err = c.repo.PublishTextSource(ctx, repository.PublishTextSourceRequest{UserID: task.UserID, TaskID: task.ID, ExpectedActiveSourceID: task.ActiveTextSourceID, Snapshot: snapshot, Lease: lease}, func(tx *repository.Repositories, source *model.VideoTextSource) error {
		completed, err := tx.CompleteTaskProcessing(repository.TaskProcessingCompleteRequest{TaskID: task.ID, JobType: model.TaskJobTypeTranscribe, JobStage: model.TaskStageTranscribing, Token: token, TaskStatus: model.TaskStatusCompleted, TaskStage: model.TaskStageNone, Now: c.currentTime()})
		if err != nil {
			return err
		}
		if !completed {
			return ErrProcessingLeaseLost
		}
		if intent.Options.AutoSummary {
			dispatch, err := c.prepareAutomaticSummary(tx, task, intent, source)
			if err != nil {
				return err
			}
			prepared = &dispatch
		}
		return nil
	})
	if err != nil {
		return err
	}
	if prepared != nil {
		c.publishSourceNext(ctx, *prepared, model.TaskJobTypeSummary)
	}
	return nil
}

type asrSourceSpan struct {
	start, end     int
	startMS, endMS *int64
	method         string
	native         []textsource.NativeTiming
}

// asrTextSourceSnapshot uses the same retained ranges as Assemble. Offsets only
// select real wording; timing always comes from a provider/alignment span or an
// observed audio window. Ambiguous repeated quotes never receive native timing.
func asrTextSourceSnapshot(task *model.VideoTask, content string, rows []model.VideoTranscriptionChunk) (textsource.Snapshot, error) {
	identity := textsource.Identity{Platform: "local", MediaFingerprint: task.FileMD5}
	if task.MediaIdentityJSON != "" {
		if err := json.Unmarshal([]byte(task.MediaIdentityJSON), &identity); err != nil {
			return textsource.Snapshot{}, fmt.Errorf("invalid frozen ASR media identity")
		}
	}
	if identity.MediaFingerprint == "" || identity.MediaFingerprint != task.FileMD5 {
		return textsource.Snapshot{}, fmt.Errorf("ASR media identity mismatch")
	}
	for i, row := range rows {
		if row.Status != model.TranscriptionChunkStatusCompleted || row.TaskID != task.ID || i > 0 && row.ChunkIndex <= rows[i-1].ChunkIndex {
			return textsource.Snapshot{}, fmt.Errorf("ASR observations are incomplete or unordered")
		}
	}
	assembled := transcript.Assemble(rows)
	if assembled.Content != content {
		return textsource.Snapshot{}, fmt.Errorf("ASR retained observations disagree with transcript")
	}
	snapshot := textsource.Snapshot{Kind: textsource.KindASR, Identity: identity, ParserVersion: asrSourceParserVersion, TrackKey: "asr"}
	seen := map[string]bool{}
	for _, contribution := range assembled.Contributions {
		row := rows[contribution.PartIndex]
		raw := strings.TrimSpace(row.Content)
		runes := []rune(raw)
		left, right := contribution.StartRune, contribution.EndRune
		if left < 0 || right > len(runes) || left >= right {
			return snapshot, fmt.Errorf("invalid ASR retained range")
		}
		retained := string(runes[left:right])
		if !strings.HasSuffix(contribution.Content, retained) {
			return snapshot, fmt.Errorf("ASR separator disagrees with retained range")
		}
		separator := strings.TrimSuffix(contribution.Content, retained)
		observationID := fmt.Sprintf("asr-window-%d", row.ChunkIndex)
		windowStart, windowEnd, windowMethod := asrObservedWindow(row)
		spans, nativeValid := asrNativeSpans(row, runes, windowStart, windowEnd)
		if !nativeValid {
			snapshot.Warnings = append(snapshot.Warnings, fmt.Sprintf("asr_window_%d_native_timing_unavailable_or_unmappable", row.ChunkIndex))
		}
		// Fill the actual text gaps between native intervals with window evidence.
		pieces := make([]asrSourceSpan, 0, len(spans)*2+1)
		cursor := 0
		for _, span := range spans {
			if span.start > cursor {
				pieces = append(pieces, asrSourceSpan{start: cursor, end: span.start, startMS: windowStart, endMS: windowEnd, method: windowMethod})
			}
			pieces = append(pieces, span)
			cursor = span.end
		}
		if cursor < len(runes) {
			pieces = append(pieces, asrSourceSpan{start: cursor, end: len(runes), startMS: windowStart, endMS: windowEnd, method: windowMethod})
		}
		for _, piece := range pieces {
			start, end := max(left, piece.start), min(right, piece.end)
			if end <= start {
				continue
			}
			for start < end && unicode.IsSpace(runes[start]) {
				separator += string(runes[start])
				start++
			}
			trail := ""
			for end > start && unicode.IsSpace(runes[end-1]) {
				trail = string(runes[end-1]) + trail
				end--
			}
			if end <= start {
				continue
			}
			text := string(runes[start:end])
			order := len(snapshot.Cues) + 1
			id := fmt.Sprintf("%s:%d:%d", observationID, start, end)
			ref := textsource.RawCueRef{ID: id, Order: order, ObservationID: observationID, ObservationOrder: row.ChunkIndex + 1, StartMS: windowStart, EndMS: windowEnd, TimingMethod: windowMethod, TextStart: &start, TextEnd: &end, NativeTimings: piece.native}
			if !seen[observationID] {
				ref.RawText = raw
				seen[observationID] = true
			}
			join := normalizeASRSeparator(separator)
			snapshot.Cues = append(snapshot.Cues, textsource.Cue{ID: id, Order: order, RawText: text, Text: text, StartMS: piece.startMS, EndMS: piece.endMS, TimingMethod: piece.method, JoinBefore: &join, RawRefs: []textsource.RawCueRef{ref}})
			if len(piece.native) == 0 && nativeValid {
				snapshot.Warnings = append(snapshot.Warnings, fmt.Sprintf("asr_window_%d_retained_gap_uses_window_timing", row.ChunkIndex))
			}
			separator = trail
		}
	}
	canonical, err := textsource.Canonicalize(snapshot, textsource.DefaultLimits())
	if err != nil {
		return canonical, err
	}
	if normalizeASRText(content) != canonical.CanonicalText {
		return canonical, fmt.Errorf("ASR cue projection changed canonical wording")
	}
	return canonical, nil
}

func normalizeASRText(text string) string {
	text = strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n")
	lines := strings.Split(text, "\n")
	for i := range lines {
		lines[i] = strings.TrimSpace(lines[i])
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func normalizeASRSeparator(separator string) string {
	separator = strings.ReplaceAll(strings.ReplaceAll(separator, "\r\n", "\n"), "\r", "\n")
	if strings.Contains(separator, "\n") {
		return strings.Map(func(r rune) rune {
			if r == '\n' {
				return r
			}
			return -1
		}, separator)
	}
	return separator
}

func asrObservedWindow(row model.VideoTranscriptionChunk) (*int64, *int64, string) {
	if row.WindowStartMS >= 0 && row.WindowEndMS > row.WindowStartMS {
		start, end := row.WindowStartMS, row.WindowEndMS
		return &start, &end, "asr_window"
	}
	if row.StartSecond >= 0 && row.EndSecond > row.StartSecond {
		start, end := int64(row.StartSecond)*1000, int64(row.EndSecond)*1000
		return &start, &end, "asr_window"
	}
	return nil, nil, textsource.TimingUnknown
}

func asrNativeSpans(row model.VideoTranscriptionChunk, runes []rune, windowStart, windowEnd *int64) ([]asrSourceSpan, bool) {
	var segments []model.TranscriptionSegment
	if windowStart == nil || json.Unmarshal([]byte(row.TimedSegments), &segments) != nil || len(segments) == 0 {
		return nil, false
	}
	raw := string(runes)
	cursor, lastTime := 0, int64(-1)
	spans := make([]asrSourceSpan, 0, len(segments))
	for i, segment := range segments {
		quote := strings.TrimSpace(segment.Text)
		if quote == "" {
			continue
		}
		start, end := segment.TextStart, segment.TextEnd
		positioned := end > start
		if positioned {
			if start < cursor || start < 0 || end > len(runes) || string(runes[start:end]) != segment.Text {
				continue
			}
		} else {
			pos := strings.Index(string(runes[cursor:]), quote)
			if pos < 0 {
				continue
			}
			start = cursor + len([]rune(string(runes[cursor:])[:pos]))
			end = start + len([]rune(quote))
		}
		cursor = end // invalid evidence still consumes its matched occurrence
		if !positioned && strings.Count(raw, quote) != 1 {
			continue
		}
		if segment.StartMS < *windowStart || segment.EndMS > *windowEnd || segment.EndMS <= segment.StartMS || segment.StartMS < lastTime {
			continue
		}
		lastTime = segment.StartMS
		method := segment.Method
		if method == "" || method == textsource.TimingUnknown {
			method = "asr_native"
		}
		native := textsource.NativeTiming{SegmentIndex: i, TextStart: start, TextEnd: end, StartMS: segment.StartMS, EndMS: segment.EndMS, Method: method}
		// Attach only punctuation/whitespace to a verified aligned word.
		if method == "forced_alignment" {
			previous := 0
			if len(spans) > 0 {
				previous = spans[len(spans)-1].end
			}
			if noASRLexical(runes[previous:start]) {
				start = previous
			}
			next := len(runes)
			if i+1 < len(segments) && segments[i+1].TextStart >= end && segments[i+1].TextStart <= len(runes) {
				next = segments[i+1].TextStart
			}
			if noASRLexical(runes[end:next]) {
				end = next
			}
		}
		startMS, endMS := segment.StartMS, segment.EndMS
		span := asrSourceSpan{start: start, end: end, startMS: &startMS, endMS: &endMS, method: method, native: []textsource.NativeTiming{native}}
		if len(spans) > 0 && method == "forced_alignment" {
			prior := &spans[len(spans)-1]
			ending := strings.TrimRight(string(runes[prior.start:prior.end]), " \t\r\n\"'”’）)")
			er := []rune(ending)
			terminal := len(er) > 0 && strings.ContainsRune("。！？!?；;.", er[len(er)-1])
			if prior.method == method && prior.end == start && startMS-*prior.endMS <= 2000 && !terminal {
				prior.end = end
				t := max(*prior.endMS, endMS)
				prior.endMS = &t
				prior.native = append(prior.native, native)
				continue
			}
		}
		spans = append(spans, span)
	}
	return spans, len(spans) > 0
}

func noASRLexical(runes []rune) bool {
	for _, r := range runes {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			return false
		}
	}
	return true
}
