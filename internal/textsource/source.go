// Package textsource validates immutable subtitle and ASR observations without
// depending on persistence, provider credentials, or model-generated wording.
package textsource

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"
)

const (
	KindSubtitle     = "platform_subtitle"
	KindASR          = "asr"
	KindLegacy       = "legacy"
	TimingSubtitle   = "subtitle_cue"
	TimingUnknown    = "unknown"
	QualityUsable    = "usable"
	QualityUnusable  = "unusable"
	SRTParserVersion = "srt-v1"
)

// Identity identifies the actual media, not a temporary download URL. Zero aid
// or cid means unknown; callers must resolve a Bilibili identity before publish.
type Identity struct {
	Platform         string `json:"platform"`
	BVID             string `json:"bvid,omitempty"`
	AID              int64  `json:"aid,omitempty"`
	CID              int64  `json:"cid,omitempty"`
	PartIndex        int    `json:"part_index,omitempty"`
	MediaFingerprint string `json:"media_fingerprint,omitempty"`
}

// RawCueRef preserves the exact original wording and declared timing when a
// rolling caption is merged. Times remain nullable, including for legacy ASR.
type RawCueRef struct {
	ID               string         `json:"id"`
	Order            int            `json:"order"`
	RawText          string         `json:"raw_text"`
	StartMS          *int64         `json:"start_ms"`
	EndMS            *int64         `json:"end_ms"`
	ObservationID    string         `json:"observation_id,omitempty"`
	ObservationOrder int            `json:"observation_order,omitempty"`
	TextStart        *int           `json:"text_start,omitempty"`
	TextEnd          *int           `json:"text_end,omitempty"`
	TimingMethod     string         `json:"timing_method,omitempty"`
	NativeTimings    []NativeTiming `json:"native_timings,omitempty"`
}

// NativeTiming retains the original provider/alignment interval and exact rune
// offsets even when several observed words form one displayed sentence cue.
type NativeTiming struct {
	SegmentIndex int    `json:"segment_index"`
	TextStart    int    `json:"text_start"`
	TextEnd      int    `json:"text_end"`
	StartMS      int64  `json:"start_ms"`
	EndMS        int64  `json:"end_ms"`
	Method       string `json:"method"`
}

type Cue struct {
	ID           string      `json:"cue_id"`
	Order        int         `json:"order"`
	RawText      string      `json:"raw_text"`
	Text         string      `json:"text"`
	StartMS      *int64      `json:"start_ms"`
	EndMS        *int64      `json:"end_ms"`
	TimingMethod string      `json:"timing_method"`
	RawRefs      []RawCueRef `json:"raw_refs"`
	// Nil joins subtitle cues with a newline. ASR adapters set the actually
	// retained separator explicitly, including an empty string for contiguous
	// spans, so word-level timing does not alter canonical wording.
	JoinBefore *string `json:"join_before,omitempty"`
}

// Snapshot is a transport value. Once published it must be immutable. ID,
// RawObjectKey and FetchedAt are storage metadata excluded from SourceDigest.
type Snapshot struct {
	ID            string   `json:"id,omitempty"`
	RawObjectKey  string   `json:"raw_object_key,omitempty"`
	FetchedAt     string   `json:"fetched_at,omitempty"`
	Kind          string   `json:"kind"`
	Identity      Identity `json:"identity"`
	TrackKey      string   `json:"track_key"`
	Language      string   `json:"language"`
	SubtitleKind  string   `json:"subtitle_kind"`
	KindBasis     string   `json:"kind_basis"`
	ParserVersion string   `json:"parser_version"`
	RawHash       string   `json:"raw_hash"`
	CanonicalText string   `json:"canonical_text"`
	CanonicalHash string   `json:"canonical_hash"`
	SourceDigest  string   `json:"source_digest"`
	Cues          []Cue    `json:"cues"`
	Quality       string   `json:"quality"`
	Warnings      []string `json:"warnings"`
}

// Limits bounds parser and adapter work. Zero numeric fields use safe defaults.
// Rolling merge is opt-in: overlapping speech alone is never proof of rolling
// captions. Only cumulative prefix growth with equal start times is merged.
type Limits struct {
	MaxBytes             int
	MaxCues              int
	MaxTextRunes         int
	MaxCueRunes          int
	MaxParseDurationMS   int64
	DurationToleranceMS  int64
	MergeRollingCaptions bool
}

func DefaultLimits() Limits {
	return Limits{MaxBytes: 5 << 20, MaxCues: 100000, MaxTextRunes: 1000000, MaxCueRunes: 20000, MaxParseDurationMS: 2000, DurationToleranceMS: 2000}
}

func (l Limits) defaults() Limits {
	d := DefaultLimits()
	if l.MaxBytes <= 0 {
		l.MaxBytes = d.MaxBytes
	}
	if l.MaxCues <= 0 {
		l.MaxCues = d.MaxCues
	}
	if l.MaxTextRunes <= 0 {
		l.MaxTextRunes = d.MaxTextRunes
	}
	if l.MaxCueRunes <= 0 {
		l.MaxCueRunes = d.MaxCueRunes
	}
	if l.MaxParseDurationMS <= 0 {
		l.MaxParseDurationMS = d.MaxParseDurationMS
	}
	if l.DurationToleranceMS <= 0 {
		l.DurationToleranceMS = d.DurationToleranceMS
	}
	return l
}

func hashText(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func normalizeText(s string) string {
	s = strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\r", "\n")
	lines := strings.Split(s, "\n")
	for i := range lines {
		lines[i] = strings.TrimSpace(lines[i])
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func validateTimes(start, end *int64, method string) error {
	if (start == nil) != (end == nil) {
		return fmt.Errorf("time range must have both endpoints or neither")
	}
	if start == nil {
		if method != TimingUnknown {
			return fmt.Errorf("missing time must use unknown timing method")
		}
		return nil
	}
	if *start < 0 || *end <= *start {
		return fmt.Errorf("invalid time range")
	}
	if method == "" || method == TimingUnknown {
		return fmt.Errorf("declared time needs an explicit timing method")
	}
	return nil
}

// Validate verifies adapter output without changing it or inventing timestamps.
// It accepts overlap and repeated wording. Order is the observed cue order.
func Validate(s Snapshot, limits Limits) error {
	l := limits.defaults()
	if s.Kind != KindSubtitle && s.Kind != KindASR && s.Kind != KindLegacy {
		return fmt.Errorf("unsupported source kind %q", s.Kind)
	}
	if strings.TrimSpace(s.Identity.Platform) == "" {
		return fmt.Errorf("source platform is required")
	}
	if s.Identity.AID < 0 || s.Identity.CID < 0 || s.Identity.PartIndex < 0 {
		return fmt.Errorf("negative source identity")
	}
	if strings.TrimSpace(s.ParserVersion) == "" {
		return fmt.Errorf("parser version is required")
	}
	if s.Kind == KindSubtitle {
		if s.SubtitleKind != "manual" && s.SubtitleKind != "automatic" && s.SubtitleKind != "unknown" {
			return fmt.Errorf("invalid subtitle kind")
		}
		if s.SubtitleKind != "unknown" && strings.TrimSpace(s.KindBasis) == "" {
			return fmt.Errorf("subtitle kind requires metadata evidence")
		}
	}
	if len(s.Cues) == 0 || len(s.Cues) > l.MaxCues {
		return fmt.Errorf("cue count outside limits")
	}
	if !utf8.ValidString(s.CanonicalText) || strings.TrimSpace(s.CanonicalText) == "" || utf8.RuneCountInString(s.CanonicalText) > l.MaxTextRunes {
		return fmt.Errorf("canonical text is empty, invalid UTF-8 or too large")
	}
	ids, rawIDs := map[string]bool{}, map[string]bool{}
	observations := map[string]string{}
	texts := make([]string, 0, len(s.Cues))
	totalRaw, totalText, totalCueRaw, previousRawOrder, totalNative := 0, 0, 0, 0, 0
	for i, cue := range s.Cues {
		if cue.ID == "" || len(cue.ID) > 128 || ids[cue.ID] {
			return fmt.Errorf("cue %d has missing, duplicate or oversized ID", i+1)
		}
		ids[cue.ID] = true
		if cue.Order != i+1 {
			return fmt.Errorf("cue %s has invalid order", cue.ID)
		}
		if !utf8.ValidString(cue.Text) || !utf8.ValidString(cue.RawText) || strings.TrimSpace(cue.Text) == "" || utf8.RuneCountInString(cue.Text) > l.MaxCueRunes {
			return fmt.Errorf("cue %s has empty, invalid or oversized text", cue.ID)
		}
		totalCueRaw += len(cue.RawText)
		if totalCueRaw > l.MaxBytes {
			return fmt.Errorf("raw cue text exceeds byte limit")
		}
		if err := validateTimes(cue.StartMS, cue.EndMS, cue.TimingMethod); err != nil {
			return fmt.Errorf("cue %s: %w", cue.ID, err)
		}
		if s.Kind == KindSubtitle && (cue.StartMS == nil || cue.TimingMethod != TimingSubtitle) {
			return fmt.Errorf("subtitle cue %s lacks declared subtitle timing", cue.ID)
		}
		if len(cue.RawRefs) == 0 {
			return fmt.Errorf("cue %s lacks original mapping", cue.ID)
		}
		if cue.JoinBefore != nil && (len(*cue.JoinBefore) > 128 || strings.TrimSpace(*cue.JoinBefore) != "") {
			return fmt.Errorf("invalid cue separator")
		}
		for _, ref := range cue.RawRefs {
			if ref.ID == "" || len(ref.ID) > 128 || ref.Order <= previousRawOrder || rawIDs[ref.ID] {
				return fmt.Errorf("invalid or repeated raw cue mapping")
			}
			previousRawOrder = ref.Order
			rawIDs[ref.ID] = true
			if !utf8.ValidString(ref.RawText) {
				return fmt.Errorf("invalid original text encoding")
			}
			method := ref.TimingMethod
			if method == "" {
				method = cue.TimingMethod
			}
			if err := validateTimes(ref.StartMS, ref.EndMS, method); err != nil {
				return fmt.Errorf("raw cue %s: %w", ref.ID, err)
			}
			if cue.StartMS != nil {
				if ref.StartMS == nil {
					return fmt.Errorf("timed cue has untimed original mapping")
				}
				if s.Kind == KindSubtitle && (*ref.StartMS < *cue.StartMS || *ref.EndMS > *cue.EndMS) {
					return fmt.Errorf("raw cue lies outside merged time range")
				}
				if s.Kind == KindASR && (*cue.StartMS < *ref.StartMS || *cue.EndMS > *ref.EndMS) {
					return fmt.Errorf("ASR cue lies outside original window")
				}
			}
			if (ref.TextStart == nil) != (ref.TextEnd == nil) {
				return fmt.Errorf("partial original text mapping")
			}
			if ref.ObservationID != "" {
				if len(ref.ObservationID) > 128 || ref.ObservationOrder < 1 {
					return fmt.Errorf("invalid original observation identity")
				}
				if ref.RawText != "" {
					if prior, ok := observations[ref.ObservationID]; ok && prior != ref.RawText {
						return fmt.Errorf("original observation wording changed")
					}
					observations[ref.ObservationID] = ref.RawText
				}
			}
			if ref.TextStart != nil {
				original := ref.RawText
				if ref.ObservationID != "" {
					original = observations[ref.ObservationID]
				}
				runes := []rune(original)
				if *ref.TextStart < 0 || *ref.TextEnd <= *ref.TextStart || *ref.TextEnd > len(runes) {
					return fmt.Errorf("original text offsets outside observation")
				}
				if s.Kind == KindASR && normalizeText(string(runes[*ref.TextStart:*ref.TextEnd])) != cue.Text {
					return fmt.Errorf("ASR text mapping disagrees with retained observation")
				}
				previousSegment, previousOffset := -1, 0
				var observedStart, observedEnd int64
				for _, native := range ref.NativeTimings {
					totalNative++
					if totalNative > l.MaxCues || native.SegmentIndex <= previousSegment || native.TextStart < previousOffset || native.TextEnd <= native.TextStart || native.TextEnd > len(runes) || native.EndMS <= native.StartMS || native.Method == "" || native.Method != cue.TimingMethod || native.Method == TimingUnknown || ref.StartMS == nil || native.StartMS < *ref.StartMS || native.EndMS > *ref.EndMS {
						return fmt.Errorf("invalid native timing provenance")
					}
					if previousSegment < 0 {
						observedStart, observedEnd = native.StartMS, native.EndMS
					} else {
						observedStart, observedEnd = min(observedStart, native.StartMS), max(observedEnd, native.EndMS)
					}
					previousSegment = native.SegmentIndex
					previousOffset = native.TextEnd
				}
				if len(ref.NativeTimings) > 0 && (cue.StartMS == nil || observedStart != *cue.StartMS || observedEnd != *cue.EndMS) {
					return fmt.Errorf("cue timing disagrees with native observations")
				}
			}
			if ref.TextStart == nil && len(ref.NativeTimings) > 0 {
				return fmt.Errorf("native timings need original offsets")
			}
			totalRaw += len(ref.RawText)
		}
		totalText += utf8.RuneCountInString(cue.Text)
		if len(rawIDs) > l.MaxCues || totalRaw > l.MaxBytes || totalText > l.MaxTextRunes {
			return fmt.Errorf("source exceeds size limits")
		}
		texts = append(texts, cue.Text)
	}
	if joinCueTexts(s.Cues, texts) != s.CanonicalText {
		return fmt.Errorf("canonical text disagrees with ordered cues")
	}
	return nil
}

// Canonicalize returns an independent normalized copy, validates it, and fills
// the content and source hashes. Callers provide actual ASR observations; no
// subtitle is converted into an ASR audio window.
func Canonicalize(s Snapshot, limits Limits) (Snapshot, error) {
	l := limits.defaults()
	if len(s.Cues) == 0 || len(s.Cues) > l.MaxCues {
		s.Quality = QualityUnusable
		return s, fmt.Errorf("cue count outside limits")
	}
	// Check aggregate bounds before copying or normalizing caller-owned data.
	totalBytes, totalRunes, totalRefs, totalNative := 0, 0, 0, 0
	for _, cue := range s.Cues {
		totalBytes += len(cue.RawText)
		totalRunes += utf8.RuneCountInString(cue.Text)
		totalRefs += len(cue.RawRefs)
		for _, ref := range cue.RawRefs {
			totalNative += len(ref.NativeTimings)
		}
		if totalBytes > l.MaxBytes || totalRunes > l.MaxTextRunes || totalRefs > l.MaxCues || totalNative > l.MaxCues || utf8.RuneCountInString(cue.Text) > l.MaxCueRunes {
			s.Quality = QualityUnusable
			return s, fmt.Errorf("source exceeds size limits")
		}
	}
	s.Cues = append([]Cue(nil), s.Cues...)
	s.Warnings = append([]string(nil), s.Warnings...)
	texts := make([]string, 0, len(s.Cues))
	for i := range s.Cues {
		c := &s.Cues[i]
		c.Order = i + 1
		if c.ID == "" {
			c.ID = fmt.Sprintf("cue-%06d", i+1)
		}
		c.Text = normalizeText(c.Text)
		if c.TimingMethod == "" && c.StartMS == nil && c.EndMS == nil {
			c.TimingMethod = TimingUnknown
		}
		c.StartMS, c.EndMS = copyTime(c.StartMS), copyTime(c.EndMS)
		if c.JoinBefore != nil {
			join := *c.JoinBefore
			c.JoinBefore = &join
		}
		c.RawRefs = append([]RawCueRef(nil), c.RawRefs...)
		if len(c.RawRefs) == 0 {
			c.RawRefs = []RawCueRef{{ID: c.ID, Order: c.Order, RawText: c.RawText, StartMS: copyTime(c.StartMS), EndMS: copyTime(c.EndMS)}}
		}
		for j := range c.RawRefs {
			c.RawRefs[j].NativeTimings = append([]NativeTiming(nil), c.RawRefs[j].NativeTimings...)
			c.RawRefs[j].StartMS = copyTime(c.RawRefs[j].StartMS)
			c.RawRefs[j].EndMS = copyTime(c.RawRefs[j].EndMS)
			if c.RawRefs[j].TextStart != nil {
				v := *c.RawRefs[j].TextStart
				c.RawRefs[j].TextStart = &v
			}
			if c.RawRefs[j].TextEnd != nil {
				v := *c.RawRefs[j].TextEnd
				c.RawRefs[j].TextEnd = &v
			}
		}
		texts = append(texts, c.Text)
	}
	s.CanonicalText = joinCueTexts(s.Cues, texts)
	if err := Validate(s, limits); err != nil {
		s.Quality = QualityUnusable
		return s, err
	}
	s.CanonicalHash = hashText(s.CanonicalText)
	digest, err := Digest(s)
	if err != nil {
		return s, err
	}
	s.SourceDigest = digest
	s.Quality = QualityUsable
	return s, nil
}

func joinCueTexts(cues []Cue, texts []string) string {
	var joined strings.Builder
	for i, text := range texts {
		if i > 0 {
			separator := "\n"
			if cues[i].JoinBefore != nil {
				separator = *cues[i].JoinBefore
			}
			joined.WriteString(separator)
		}
		joined.WriteString(text)
	}
	return joined.String()
}

func copyTime(t *int64) *int64 {
	if t == nil {
		return nil
	}
	v := *t
	return &v
}

// Digest hashes a fixed ordered JSON envelope, including original cue mapping.
// It deliberately excludes UUIDs, fetch time, raw bytes hash, storage paths,
// warning diagnostics and temporary URLs. CanonicalHash alone is insufficient
// for detecting a changed part, media revision, or subtitle timing.
func Digest(s Snapshot) (string, error) {
	if err := Validate(s, Limits{}); err != nil {
		return "", err
	}
	envelope := struct {
		Version       string   `json:"version"`
		Kind          string   `json:"kind"`
		Identity      Identity `json:"identity"`
		TrackKey      string   `json:"track_key"`
		Language      string   `json:"language"`
		SubtitleKind  string   `json:"subtitle_kind"`
		KindBasis     string   `json:"kind_basis"`
		ParserVersion string   `json:"parser_version"`
		Text          string   `json:"canonical_text"`
		Cues          []Cue    `json:"cues"`
	}{"text-source-v1", s.Kind, s.Identity, s.TrackKey, s.Language, s.SubtitleKind, s.KindBasis, s.ParserVersion, s.CanonicalText, s.Cues}
	b, err := json.Marshal(envelope)
	if err != nil {
		return "", err
	}
	return hashText(string(b)), nil
}
