package eval

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
	"unicode"
	"vid-lens/internal/model"
	"vid-lens/internal/transcript"
)

const TranscriptStatisticsVersion = "transcript-stats-v1"

type TranscriptDataset struct {
	SchemaVersion int              `json:"schema_version"`
	Split         string           `json:"split"`
	Cases         []TranscriptCase `json:"cases"`
}
type TranscriptWindow struct {
	ChunkIndex              int                          `json:"chunk_index"`
	SegmentKey              string                       `json:"segment_key"`
	SegmenterVersion        string                       `json:"segmenter_version"`
	WindowStartMS           int64                        `json:"window_start_ms"`
	WindowEndMS             int64                        `json:"window_end_ms"`
	CoreStartMS             int64                        `json:"core_start_ms"`
	CoreEndMS               int64                        `json:"core_end_ms"`
	Status                  string                       `json:"status"`
	Content                 string                       `json:"content"`
	Words                   []model.TranscriptionSegment `json:"words,omitempty"`
	ExpectAlignmentRejected bool                         `json:"expect_alignment_rejected,omitempty"`
}
type TranscriptCase struct {
	ID           string               `json:"id"`
	Kind         string               `json:"kind"` // synthetic or media
	Tags         []string             `json:"tags"`
	MediaSHA256  string               `json:"media_sha256,omitempty"`
	TaskID       int64                `json:"task_id,omitempty"`
	DurationMS   int64                `json:"duration_ms"`
	Windows      []TranscriptWindow   `json:"windows"`
	ExpectedText *string              `json:"expected_text,omitempty"` // constructed fixtures only
	Sentences    []TranscriptSentence `json:"sentences,omitempty"`
}
type TranscriptSentence struct {
	ID         string                `json:"id"`
	Text       string                `json:"text"`
	SourceIDs  []string              `json:"source_ids"`
	StartMS    *int64                `json:"start_ms"`
	EndMS      *int64                `json:"end_ms"`
	TimeStatus string                `json:"time_status"`
	Annotation *TranscriptAnnotation `json:"annotation"`
}
type TranscriptAnnotation struct {
	Text             string  `json:"text"`
	StartAllowedMS   []int64 `json:"start_allowed_ms"`
	EndAllowedMS     []int64 `json:"end_allowed_ms"`
	ReviewedBy       string  `json:"reviewed_by"`
	ReviewedDate     string  `json:"reviewed_date"`
	SourceChecked    bool    `json:"source_checked"`
	CompletePlayback *bool   `json:"complete_playback"`
}
type TranscriptErrorDistribution struct {
	Count int      `json:"count"`
	P50   *float64 `json:"p50_ms"`
	P95   *float64 `json:"p95_ms"`
	Max   *float64 `json:"max_ms"`
}
type TranscriptCaseResult struct {
	ID                  string   `json:"id"`
	Text                string   `json:"text"`
	Violations          []string `json:"structural_violations"`
	SyntheticInsertions int      `json:"synthetic_insertions"`
	SyntheticDeletions  int      `json:"synthetic_deletions"`
	AlignmentRejections int      `json:"alignment_rejections"`
	MappedCharacters    int      `json:"mapped_characters"`
	TotalCharacters     int      `json:"total_characters"`
}
type TranscriptReport struct {
	ToolVersion            string                      `json:"tool_version"`
	Normalization          string                      `json:"normalization"`
	Identity               map[string]string           `json:"identity"`
	DatasetSHA256          string                      `json:"dataset_sha256"`
	StructurePassed        bool                        `json:"structure_passed"`
	Cases                  []TranscriptCaseResult      `json:"cases"`
	HumanQualityStatus     string                      `json:"human_quality_status"`
	SentenceCount          int                         `json:"sentence_count"`
	Reviewed               int                         `json:"reviewed_sentences"`
	Unmatched              int                         `json:"unmatched_sentences"`
	Unknown                int                         `json:"unknown_sentences"`
	StartError             TranscriptErrorDistribution `json:"sentence_start_error"`
	EndError               TranscriptErrorDistribution `json:"sentence_end_error"`
	PlaybackReviewed       int                         `json:"playback_reviewed"`
	PlaybackUnknown        int                         `json:"playback_unknown"`
	PlaybackSuccessRate    *float64                    `json:"complete_playback_success_rate"`
	CER                    *float64                    `json:"cer"`
	WER                    *float64                    `json:"wer"`
	MappedCharacters       int                         `json:"mapped_characters"`
	TotalCharacters        int                         `json:"total_characters"`
	ExactCharacterCoverage *float64                    `json:"exact_character_coverage"`
}

func EvaluateTranscript(dataset TranscriptDataset) (TranscriptReport, error) {
	r := TranscriptReport{ToolVersion: TranscriptStatisticsVersion, Normalization: "Unicode letters/numbers; lower case; punctuation and whitespace ignored for CER; Unicode word runs for WER; nearest-rank percentiles", StructurePassed: true, HumanQualityStatus: "not_audited", Cases: []TranscriptCaseResult{}}
	if dataset.SchemaVersion != 1 || dataset.Split != "dev" || len(dataset.Cases) == 0 {
		return r, fmt.Errorf("requires schema_version=1, split=dev and nonempty cases")
	}
	ids := map[string]bool{}
	var starts, ends []float64
	cerEdits, cerChars, werEdits, werWords, replayOK := 0, 0, 0, 0, 0
	for _, c := range dataset.Cases {
		if c.ID == "" || ids[c.ID] || c.DurationMS <= 0 || (c.Kind != "synthetic" && c.Kind != "media") {
			return r, fmt.Errorf("invalid or duplicate case %q", c.ID)
		}
		ids[c.ID] = true
		if c.Kind == "media" && (len(c.MediaSHA256) != 64 || c.ExpectedText != nil) {
			return r, fmt.Errorf("media case %s requires hash and separate human annotations", c.ID)
		}
		if c.Kind == "media" {
			if _, err := hex.DecodeString(c.MediaSHA256); err != nil {
				return r, fmt.Errorf("invalid media hash %s", c.ID)
			}
		}
		result := TranscriptCaseResult{ID: c.ID, Violations: []string{}}
		rows := []model.VideoTranscriptionChunk{}
		aligned := map[int]bool{}
		for i, window := range c.Windows {
			if window.ChunkIndex < 0 || i > 0 && window.ChunkIndex <= c.Windows[i-1].ChunkIndex || window.WindowStartMS < 0 || window.WindowEndMS <= window.WindowStartMS || window.WindowEndMS > c.DurationMS || window.CoreStartMS < window.WindowStartMS || window.CoreEndMS > window.WindowEndMS || window.CoreEndMS <= window.CoreStartMS || window.SegmentKey == "" || window.SegmenterVersion == "" {
				return r, fmt.Errorf("invalid window %s/%d", c.ID, i)
			}
			if window.Status != "completed" && window.Status != "failed" && window.Status != "pending" {
				return r, fmt.Errorf("invalid window status %s/%d", c.ID, i)
			}
			row := model.VideoTranscriptionChunk{ChunkIndex: window.ChunkIndex, SegmentKey: window.SegmentKey, SegmenterVersion: window.SegmenterVersion, WindowStartMS: window.WindowStartMS, WindowEndMS: window.WindowEndMS, CoreStartMS: window.CoreStartMS, CoreEndMS: window.CoreEndMS, Status: window.Status, Content: window.Content}
			if len(window.Words) > 0 {
				b, _ := json.Marshal(window.Words)
				row.TimedSegments = string(b)
				err := transcript.ValidateAlignedWords(row, window.Words)
				if err != nil {
					result.AlignmentRejections++
				}
				if (err != nil) != window.ExpectAlignmentRejected {
					result.Violations = append(result.Violations, fmt.Sprintf("unexpected_alignment_validation:%d", i))
				}
				aligned[i] = err == nil
			} else if window.ExpectAlignmentRejected {
				return r, fmt.Errorf("rejection case has no alignment %s/%d", c.ID, i)
			}
			rows = append(rows, row)
		}
		assembly := transcript.Assemble(rows)
		result.Text = assembly.Content
		var rebuilt strings.Builder
		for _, contribution := range assembly.Contributions {
			rebuilt.WriteString(contribution.Content)
			row := rows[contribution.PartIndex]
			source := []rune(strings.TrimSpace(row.Content))
			if contribution.StartRune < 0 || contribution.EndRune > len(source) || contribution.EndRune < contribution.StartRune {
				result.Violations = append(result.Violations, "source_range_invalid")
				continue
			}
			retained := string(source[contribution.StartRune:contribution.EndRune])
			if !strings.HasSuffix(contribution.Content, retained) {
				result.Violations = append(result.Violations, "source_text_changed")
			}
			chars := len(transcriptCharacters(retained))
			result.TotalCharacters += chars
			if aligned[contribution.PartIndex] {
				result.MappedCharacters += chars
			}
		}
		if rebuilt.String() != assembly.Content {
			result.Violations = append(result.Violations, "source_rebuild_mismatch")
		}
		if c.ExpectedText != nil {
			if c.Kind != "synthetic" {
				return r, fmt.Errorf("expected_text is only for synthetic cases")
			}
			if assembly.Content != *c.ExpectedText {
				result.Violations = append(result.Violations, "constructed_text_mismatch")
			}
			_, result.SyntheticInsertions, result.SyntheticDeletions = transcriptEditCounts(transcriptCharacters(*c.ExpectedText), transcriptCharacters(assembly.Content))
		}
		sentenceIDs := map[string]bool{}
		for _, sentence := range c.Sentences {
			if sentence.ID == "" || sentenceIDs[sentence.ID] || len(sentence.SourceIDs) == 0 || sentence.Text == "" {
				return r, fmt.Errorf("invalid sentence %s/%s", c.ID, sentence.ID)
			}
			sentenceIDs[sentence.ID] = true
			r.SentenceCount++
			if sentence.StartMS != nil && sentence.EndMS != nil && (*sentence.StartMS < 0 || *sentence.EndMS <= *sentence.StartMS || *sentence.EndMS > c.DurationMS) {
				result.Violations = append(result.Violations, "sentence_time_invalid:"+sentence.ID)
			}
			a := sentence.Annotation
			if a == nil || a.ReviewedBy == "" {
				r.Unknown++
				r.PlaybackUnknown++
				continue
			}
			if c.Kind != "media" || !a.SourceChecked || a.Text == "" || !transcriptValidRange(a.StartAllowedMS, c.DurationMS) || !transcriptValidRange(a.EndAllowedMS, c.DurationMS) || a.StartAllowedMS[0] >= a.EndAllowedMS[1] {
				return r, fmt.Errorf("incomplete human annotation %s/%s", c.ID, sentence.ID)
			}
			if _, err := time.Parse("2006-01-02", a.ReviewedDate); err != nil {
				return r, fmt.Errorf("invalid review date %s/%s", c.ID, sentence.ID)
			}
			r.Reviewed++
			edits, _, _ := transcriptEditCounts(transcriptCharacters(a.Text), transcriptCharacters(sentence.Text))
			cerEdits += edits
			cerChars += len(transcriptCharacters(a.Text))
			edits, _, _ = transcriptEditCounts(transcriptWords(a.Text), transcriptWords(sentence.Text))
			werEdits += edits
			werWords += len(transcriptWords(a.Text))
			if strings.Join(transcriptCharacters(a.Text), "") != strings.Join(transcriptCharacters(sentence.Text), "") {
				r.Unmatched++
			} else if sentence.StartMS == nil || sentence.EndMS == nil || sentence.TimeStatus != "exact" {
				r.Unknown++
			} else {
				starts = append(starts, transcriptRangeError(*sentence.StartMS, a.StartAllowedMS))
				ends = append(ends, transcriptRangeError(*sentence.EndMS, a.EndAllowedMS))
			}
			if a.CompletePlayback == nil {
				r.PlaybackUnknown++
			} else {
				r.PlaybackReviewed++
				if *a.CompletePlayback {
					replayOK++
				}
			}
		}
		r.TotalCharacters += result.TotalCharacters
		r.MappedCharacters += result.MappedCharacters
		if len(result.Violations) > 0 {
			r.StructurePassed = false
		}
		r.Cases = append(r.Cases, result)
	}
	if r.Reviewed > 0 {
		r.HumanQualityStatus = "partial_audit"
		if r.Unknown == 0 && r.Unmatched == 0 && r.PlaybackUnknown == 0 {
			r.HumanQualityStatus = "audited"
		}
	}
	r.StartError, r.EndError = transcriptDistribution(starts), transcriptDistribution(ends)
	r.CER, r.WER = transcriptRatio(cerEdits, cerChars), transcriptRatio(werEdits, werWords)
	r.PlaybackSuccessRate = transcriptRatio(replayOK, r.PlaybackReviewed)
	r.ExactCharacterCoverage = transcriptRatio(r.MappedCharacters, r.TotalCharacters)
	return r, nil
}
func transcriptValidRange(x []int64, duration int64) bool {
	return len(x) == 2 && x[0] >= 0 && x[1] >= x[0] && x[1] <= duration
}
func transcriptRangeError(x int64, bounds []int64) float64 {
	if x < bounds[0] {
		return float64(bounds[0] - x)
	}
	if x > bounds[1] {
		return float64(x - bounds[1])
	}
	return 0
}
func transcriptRatio(a, b int) *float64 {
	if b == 0 {
		return nil
	}
	n := float64(a) / float64(b)
	return &n
}
func transcriptDistribution(x []float64) TranscriptErrorDistribution {
	d := TranscriptErrorDistribution{Count: len(x)}
	if len(x) == 0 {
		return d
	}
	sort.Float64s(x)
	p50, p95, max := x[int(math.Ceil(float64(len(x))*.5))-1], x[int(math.Ceil(float64(len(x))*.95))-1], x[len(x)-1]
	d.P50, d.P95, d.Max = &p50, &p95, &max
	return d
}
func transcriptCharacters(s string) []string {
	var x []string
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			x = append(x, string(unicode.ToLower(r)))
		}
	}
	return x
}
func transcriptWords(s string) []string {
	return strings.FieldsFunc(strings.ToLower(s), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsNumber(r) })
}

// Levenshtein with deterministic substitution, deletion, insertion tie order.
func transcriptEditCounts(a, b []string) (int, int, int) {
	for len(a) > 0 && len(b) > 0 && a[0] == b[0] {
		a, b = a[1:], b[1:]
	}
	for len(a) > 0 && len(b) > 0 && a[len(a)-1] == b[len(b)-1] {
		a, b = a[:len(a)-1], b[:len(b)-1]
	}
	type cell struct{ edits, insertions, deletions int }
	previous := make([]cell, len(b)+1)
	for j := range previous {
		previous[j] = cell{edits: j, insertions: j}
	}
	for i, source := range a {
		current := make([]cell, len(b)+1)
		current[0] = cell{edits: i + 1, deletions: i + 1}
		for j, target := range b {
			best := previous[j]
			if source != target {
				best.edits++
			}
			del := previous[j+1]
			del.edits++
			del.deletions++
			ins := current[j]
			ins.edits++
			ins.insertions++
			if del.edits < best.edits {
				best = del
			}
			if ins.edits < best.edits {
				best = ins
			}
			current[j+1] = best
		}
		previous = current
	}
	v := previous[len(b)]
	return v.edits, v.insertions, v.deletions
}
