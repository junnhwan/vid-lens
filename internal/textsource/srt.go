package textsource

import (
	"context"
	"fmt"
	"html"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"vid-lens/internal/pkg/subtitlelang"
)

// ParseOptions freezes selected track metadata separately from subtitle text.
// TrackComplete=false is explicit provider evidence of truncation. Sparse
// subtitle coverage, by itself, is never classified as incomplete.
type ParseOptions struct {
	Identity         Identity
	TrackKey         string
	Language         string
	SubtitleKind     string
	KindBasis        string
	DurationMS       *int64
	ExpectedLanguage string
	TrackComplete    *bool
	Limits           Limits
}

var timestampLine = regexp.MustCompile(`^([0-9]{1,6}):([0-9]{2}):([0-9]{2})[,.]([0-9]{3})\s+-->\s+([0-9]{1,6}):([0-9]{2}):([0-9]{2})[,.]([0-9]{3})$`)
var cueNumber = regexp.MustCompile(`^[0-9]+$`)
var limitedStyle = regexp.MustCompile(`(?i)</?(?:b|i|u)>|</?(?:font|span)(?:\s+[^<>]*)?>`)
var lineBreak = regexp.MustCompile(`(?i)<br\s*/?>`)

// ParseSRT parses UTF-8 SRT only (never XML danmaku), retaining raw text and
// declared millisecond ranges. Malformed cues fail the entire source: callers
// receive diagnostics and an error and must not publish the partial snapshot.
// Context cancellation and configurable resource budgets bound parse work.
func ParseSRT(ctx context.Context, raw []byte, options ParseOptions) (Snapshot, error) {
	l := options.Limits.defaults()
	s := Snapshot{Kind: KindSubtitle, Identity: options.Identity, TrackKey: options.TrackKey,
		Language: options.Language, SubtitleKind: options.SubtitleKind, KindBasis: options.KindBasis,
		ParserVersion: SRTParserVersion, Quality: QualityUnusable}
	if s.SubtitleKind == "" {
		s.SubtitleKind = "unknown"
	}
	fail := func(err error) (Snapshot, error) {
		s.Quality = QualityUnusable
		s.Warnings = append(s.Warnings, err.Error())
		return s, err
	}
	if len(raw) > l.MaxBytes {
		return fail(fmt.Errorf("subtitle exceeds byte limit"))
	}
	if !utf8.Valid(raw) {
		return fail(fmt.Errorf("subtitle must be UTF-8"))
	}
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(l.MaxParseDurationMS)*time.Millisecond)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return fail(err)
	}
	if options.DurationMS != nil && *options.DurationMS <= 0 {
		return fail(fmt.Errorf("known media duration must be positive"))
	}
	if options.TrackComplete != nil && !*options.TrackComplete {
		return fail(fmt.Errorf("provider reported truncated subtitle track"))
	}
	if !sameLanguage(s.Language, options.ExpectedLanguage) {
		return fail(fmt.Errorf("selected subtitle language does not match requested language"))
	}
	if s.Language == "" && options.ExpectedLanguage != "" {
		s.Warnings = append(s.Warnings, "subtitle language unknown; requested language cannot be verified")
	}
	s.RawHash = hashText(string(raw))
	text := strings.TrimPrefix(string(raw), "\ufeff")
	if strings.ContainsRune(text, 0) {
		return fail(fmt.Errorf("subtitle contains NUL bytes"))
	}
	text = strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n")
	lines := strings.Split(text, "\n")
	i, total := 0, 0
	var previousStart int64 = -1
	for i < len(lines) {
		if err := ctx.Err(); err != nil {
			return fail(err)
		}
		for i < len(lines) && strings.TrimSpace(lines[i]) == "" {
			i++
		}
		if i >= len(lines) {
			break
		}
		if len(s.Cues) >= l.MaxCues {
			return fail(fmt.Errorf("subtitle exceeds cue limit"))
		}
		rawID := fmt.Sprintf("raw-%06d", len(s.Cues)+1)
		if cueNumber.MatchString(strings.TrimSpace(lines[i])) {
			i++
		}
		if i >= len(lines) {
			return fail(fmt.Errorf("cue %d missing timing", len(s.Cues)+1))
		}
		match := timestampLine.FindStringSubmatch(strings.TrimSpace(lines[i]))
		if match == nil {
			return fail(fmt.Errorf("cue %d has malformed timing", len(s.Cues)+1))
		}
		start, err := parseTime(match[1:5])
		if err != nil {
			return fail(fmt.Errorf("cue %d: %w", len(s.Cues)+1, err))
		}
		end, err := parseTime(match[5:9])
		if err != nil {
			return fail(fmt.Errorf("cue %d: %w", len(s.Cues)+1, err))
		}
		if end <= start {
			return fail(fmt.Errorf("cue %d has nonpositive range", len(s.Cues)+1))
		}
		if start < previousStart {
			return fail(fmt.Errorf("cue %d starts before preceding cue", len(s.Cues)+1))
		}
		previousStart = start
		if options.DurationMS != nil && end > *options.DurationMS {
			if end-*options.DurationMS > l.DurationToleranceMS {
				return fail(fmt.Errorf("cue %d exceeds media duration", len(s.Cues)+1))
			}
			s.Warnings = append(s.Warnings, fmt.Sprintf("cue %d slightly exceeds media duration; original timing retained", len(s.Cues)+1))
		}
		i++
		begin := i
		for i < len(lines) && strings.TrimSpace(lines[i]) != "" {
			i++
		}
		rawText := strings.Join(lines[begin:i], "\n")
		// A missing blank separator must not smuggle another cue into wording.
		if strings.Contains(rawText, "-->") {
			return fail(fmt.Errorf("cue %d has missing separator or damaged text", len(s.Cues)+1))
		}
		clean := cleanSubtitle(rawText)
		count := utf8.RuneCountInString(clean)
		if clean == "" || count > l.MaxCueRunes {
			return fail(fmt.Errorf("cue %d has empty or oversized text", len(s.Cues)+1))
		}
		total += count
		if total > l.MaxTextRunes {
			return fail(fmt.Errorf("subtitle exceeds text limit"))
		}
		cue := Cue{ID: fmt.Sprintf("cue-%06d", len(s.Cues)+1), Order: len(s.Cues) + 1, RawText: rawText,
			Text: clean, StartMS: &start, EndMS: &end, TimingMethod: TimingSubtitle,
			RawRefs: []RawCueRef{{ID: rawID, Order: len(s.Cues) + 1, RawText: rawText, StartMS: copyTime(&start), EndMS: copyTime(&end)}}}
		s.Cues = append(s.Cues, cue)
	}
	if err := ctx.Err(); err != nil {
		return fail(err)
	}
	if l.MergeRollingCaptions {
		s.Cues = mergeRolling(s.Cues)
	}
	canonical, err := Canonicalize(s, l)
	if err != nil {
		return fail(err)
	}
	if err := ctx.Err(); err != nil {
		return fail(err)
	}
	return canonical, nil
}

func parseTime(parts []string) (int64, error) {
	values := make([]int64, 4)
	for i, p := range parts {
		n, err := strconv.ParseInt(p, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("invalid timestamp")
		}
		values[i] = n
	}
	if values[1] >= 60 || values[2] >= 60 {
		return 0, fmt.Errorf("invalid timestamp minute or second")
	}
	return ((values[0]*60+values[1])*60+values[2])*1000 + values[3], nil
}

func cleanSubtitle(s string) string {
	// Strip only supported literal styling first: encoded angle brackets are
	// speech/code and must remain text after entity decoding.
	s = lineBreak.ReplaceAllString(s, "\n")
	s = limitedStyle.ReplaceAllString(s, "")
	return normalizeText(html.UnescapeString(s))
}

func sameLanguage(actual, wanted string) bool {
	if actual == "" || wanted == "" {
		return true
	}
	return subtitlelang.Matches(actual, wanted)
}

func mergeRolling(cues []Cue) []Cue {
	merged := make([]Cue, 0, len(cues))
	for _, cue := range cues {
		if len(merged) > 0 {
			previous := &merged[len(merged)-1]
			// A shared onset and strictly cumulative text/end-time extension
			// provide rolling-caption evidence. Equal text or suffix matches do
			// not prove repetition: they can be two speakers or repeated speech.
			if *previous.StartMS == *cue.StartMS && *cue.EndMS > *previous.EndMS &&
				len(cue.Text) > len(previous.Text) && strings.HasPrefix(cue.Text, previous.Text) {
				previous.Text = cue.Text
				previous.EndMS = copyTime(cue.EndMS)
				previous.RawRefs = append(previous.RawRefs, cue.RawRefs...)
				previous.RawText += "\n" + cue.RawText
				continue
			}
		}
		merged = append(merged, cue)
	}
	return merged
}
