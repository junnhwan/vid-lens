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
	ID      string `json:"id"`
	Order   int    `json:"order"`
	RawText string `json:"raw_text"`
	StartMS *int64 `json:"start_ms"`
	EndMS   *int64 `json:"end_ms"`
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
	texts := make([]string, 0, len(s.Cues))
	totalRaw, totalText, totalCueRaw, previousRawOrder := 0, 0, 0, 0
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
		for _, ref := range cue.RawRefs {
			if ref.ID == "" || len(ref.ID) > 128 || ref.Order <= previousRawOrder || rawIDs[ref.ID] {
				return fmt.Errorf("invalid or repeated raw cue mapping")
			}
			previousRawOrder = ref.Order
			rawIDs[ref.ID] = true
			if !utf8.ValidString(ref.RawText) {
				return fmt.Errorf("invalid original text encoding")
			}
			if err := validateTimes(ref.StartMS, ref.EndMS, cue.TimingMethod); err != nil {
				return fmt.Errorf("raw cue %s: %w", ref.ID, err)
			}
			if cue.StartMS != nil && (*ref.StartMS < *cue.StartMS || *ref.EndMS > *cue.EndMS) {
				return fmt.Errorf("raw cue lies outside merged time range")
			}
			totalRaw += len(ref.RawText)
		}
		totalText += utf8.RuneCountInString(cue.Text)
		if len(rawIDs) > l.MaxCues || totalRaw > l.MaxBytes || totalText > l.MaxTextRunes {
			return fmt.Errorf("source exceeds size limits")
		}
		texts = append(texts, cue.Text)
	}
	if strings.Join(texts, "\n") != s.CanonicalText {
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
	totalBytes, totalRunes, totalRefs := 0, 0, 0
	for _, cue := range s.Cues {
		totalBytes += len(cue.RawText)
		totalRunes += utf8.RuneCountInString(cue.Text)
		totalRefs += len(cue.RawRefs)
		if totalBytes > l.MaxBytes || totalRunes > l.MaxTextRunes || totalRefs > l.MaxCues || utf8.RuneCountInString(cue.Text) > l.MaxCueRunes {
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
		c.RawRefs = append([]RawCueRef(nil), c.RawRefs...)
		if len(c.RawRefs) == 0 {
			c.RawRefs = []RawCueRef{{ID: c.ID, Order: c.Order, RawText: c.RawText, StartMS: copyTime(c.StartMS), EndMS: copyTime(c.EndMS)}}
		}
		for j := range c.RawRefs {
			c.RawRefs[j].StartMS = copyTime(c.RawRefs[j].StartMS)
			c.RawRefs[j].EndMS = copyTime(c.RawRefs[j].EndMS)
		}
		texts = append(texts, c.Text)
	}
	s.CanonicalText = strings.Join(texts, "\n")
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
