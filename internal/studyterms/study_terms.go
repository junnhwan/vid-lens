package studyterms

import (
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"vid-lens/internal/model"
)

// StudyTermEvidence is a derived, bounded reading aid. The snapshot items remain
// the source of truth; a candidate is never an instruction to replace ASR text.
type StudyTermEvidence struct {
	Candidates                []StudyTermCandidate `json:"candidates"`
	VisualObservations        []StudyTermSource    `json:"visual_observations,omitempty"`
	OmittedCandidates         int                  `json:"omitted_candidates,omitempty"`
	OmittedVisualObservations int                  `json:"omitted_visual_observations,omitempty"`
}

type StudyTermCandidate struct {
	Term              string            `json:"term"`
	VisualSupport     []StudyTermSource `json:"visual_support"`
	ASRMentions       []StudyTermSource `json:"asr_mentions,omitempty"`
	PossibleConflicts []StudyTermSource `json:"possible_conflicts,omitempty"`
}

type StudyTermSource struct {
	EvidenceID string `json:"evidence_id"`
	Modality   string `json:"modality"`
	Excerpt    string `json:"excerpt"`
	Truncated  bool   `json:"truncated,omitempty"`
	StartMS    *int64 `json:"start_ms,omitempty"`
	EndMS      *int64 `json:"end_ms,omitempty"`
}

// StudyTermCorrection names the exact observations behind a proposed spelling
// change. A successful check confirms source presence and temporal compatibility,
// not that the two names have the same meaning. The caller must still assess that.
type StudyTermCorrection struct {
	From                 string `json:"from"`
	To                   string `json:"to"`
	TranscriptEvidenceID string `json:"transcript_evidence_id"`
	VisualEvidenceID     string `json:"visual_evidence_id"`
}

const TitleGuidance = " 只能依据转写与已出现的画面证据起标题。画面明确显示的专有名称若与同期转写指向同一实体，以画面原拼写为准，不附转写误音；若两种相近名称确实代表不同实体，必须保留区别。证据不足时保持原说法，不猜测熟悉品牌。"

// FocusEvidence bounds prompt cost while preserving an explicit count of
// omitted derived hints. Original snapshot observations are never removed.
func FocusEvidence(input StudyTermEvidence, candidateLimit, observationLimit int) StudyTermEvidence {
	if candidateLimit < 0 {
		candidateLimit = 0
	}
	if observationLimit < 0 {
		observationLimit = 0
	}
	out := input
	if len(out.Candidates) > candidateLimit {
		out.OmittedCandidates += len(out.Candidates) - candidateLimit
		out.Candidates = out.Candidates[:candidateLimit]
	}
	if len(out.VisualObservations) > observationLimit {
		out.OmittedVisualObservations += len(out.VisualObservations) - observationLimit
		out.VisualObservations = out.VisualObservations[:observationLimit]
	}
	return out
}

var studyLatinTerm = regexp.MustCompile(`[A-Za-z][A-Za-z0-9]*(?:[-_.+][A-Za-z0-9]+)*`)
var studyLatinPhrase = regexp.MustCompile(`\b[A-Z][A-Za-z0-9]*(?:[ \t]+[A-Z][A-Za-z0-9]*){1,3}\b`)
var studyQuotedTerm = regexp.MustCompile(`[“「《]([\p{Han}A-Za-z0-9·_.+-]{2,40})[”」》]`)

// DeriveStudyTermEvidence extracts spellings actually visible in OCR or Vision
// observations. Similar ASR spellings are only possible conflicts. It does not
// use a product dictionary, infer substitutions, or mutate any snapshot item.
func DeriveStudyTermEvidence(items []model.SourceSnapshotItem) StudyTermEvidence {
	const maxCandidates = 48
	byKey := map[string]*StudyTermCandidate{}
	transcripts := make([]model.SourceSnapshotItem, 0)
	visuals := make([]model.SourceSnapshotItem, 0)
	for _, item := range items {
		switch item.Modality {
		case model.ChunkModalityTranscript:
			transcripts = append(transcripts, item)
		case model.ChunkModalityVisualOCR, model.ChunkModalityVisualCaption:
			visuals = append(visuals, item)
			for _, term := range visualTerms(item) {
				key := strings.ToLower(term)
				candidate := byKey[key]
				if candidate == nil {
					candidate = &StudyTermCandidate{Term: term, VisualSupport: []StudyTermSource{}}
					byKey[key] = candidate
				}
				addTermSource(&candidate.VisualSupport, item, term, 3)
			}
		}
	}
	all := make([]StudyTermCandidate, 0, len(byKey))
	for _, candidate := range byKey {
		all = append(all, *candidate)
	}
	for i := range all {
		candidate := &all[i]
		for _, item := range visuals {
			if !overlapsAny(item, candidate.VisualSupport) {
				continue
			}
			for _, term := range visualTerms(item) {
				if plausibleAlternative(term, candidate.Term) {
					addTermSource(&candidate.PossibleConflicts, item, term, 2)
				}
			}
		}
		for _, item := range transcripts {
			if !overlapsAny(item, candidate.VisualSupport) {
				continue
			}
			if containsTerm(item.Content, candidate.Term) {
				addTermSource(&candidate.ASRMentions, item, candidate.Term, 2)
			}
			for _, word := range studyLatinTerm.FindAllString(item.Content, -1) {
				if plausibleAlternative(word, candidate.Term) {
					addTermSource(&candidate.PossibleConflicts, item, word, 2)
				}
			}
			for _, phrase := range studyLatinPhrase.FindAllString(item.Content, -1) {
				if plausibleAlternative(phrase, candidate.Term) {
					addTermSource(&candidate.PossibleConflicts, item, phrase, 2)
				}
			}
		}
	}
	sort.Slice(all, func(i, j int) bool {
		iNamedPhrase, jNamedPhrase := strings.Contains(all[i].Term, " ") && len(all[i].PossibleConflicts) > 0, strings.Contains(all[j].Term, " ") && len(all[j].PossibleConflicts) > 0
		if iNamedPhrase != jNamedPhrase {
			return iNamedPhrase
		}
		iConflict, jConflict := len(all[i].PossibleConflicts) > 0, len(all[j].PossibleConflicts) > 0
		if iConflict != jConflict {
			return iConflict
		}
		iASR, jASR := len(all[i].ASRMentions) > 0, len(all[j].ASRMentions) > 0
		if iASR != jASR {
			return iASR
		}
		iOCR, jOCR := hasOCR(all[i]), hasOCR(all[j])
		if iOCR != jOCR {
			return iOCR
		}
		if len(all[i].VisualSupport) != len(all[j].VisualSupport) {
			return len(all[i].VisualSupport) > len(all[j].VisualSupport)
		}
		return strings.ToLower(all[i].Term) < strings.ToLower(all[j].Term)
	})
	result := StudyTermEvidence{Candidates: all}
	if len(result.Candidates) > maxCandidates {
		result.OmittedCandidates = len(result.Candidates) - maxCandidates
		result.Candidates = result.Candidates[:maxCandidates]
	}
	// Captions without typographic name markers still carry useful visual
	// spellings (including Chinese names). Include bounded raw excerpts so they
	// can be assessed instead of silently requiring OCR or a name dictionary.
	const maxObservations = 32
	if len(visuals) > maxObservations {
		result.OmittedVisualObservations = len(visuals) - maxObservations
	}
	for i := 0; i < len(visuals) && len(result.VisualObservations) < maxObservations; i++ {
		index := i
		if len(visuals) > maxObservations {
			index = i * (len(visuals) - 1) / (maxObservations - 1)
		}
		item := visuals[index]
		excerpt, truncated := leadingRunes(item.Content, 240)
		result.VisualObservations = append(result.VisualObservations, StudyTermSource{
			EvidenceID: item.ID, Modality: item.Modality, Excerpt: excerpt, Truncated: truncated,
			StartMS: item.StartMS, EndMS: item.EndMS,
		})
	}
	return result
}

func leadingRunes(s string, limit int) (string, bool) {
	runes := []rune(s)
	if len(runes) <= limit {
		return s, false
	}
	return string(runes[:limit]), true
}

// ValidateStudyTermCorrection checks an explicit proposal against the original
// snapshot. Unknown timing and competing visual spelling fail closed. It must
// not be used as a string-replacement authorization without semantic review.
func ValidateStudyTermCorrection(items []model.SourceSnapshotItem, proposal StudyTermCorrection) bool {
	from, to := strings.TrimSpace(proposal.From), strings.TrimSpace(proposal.To)
	if from == "" || to == "" || strings.EqualFold(from, to) || proposal.TranscriptEvidenceID == "" || proposal.VisualEvidenceID == "" {
		return false
	}
	var asr, visual *model.SourceSnapshotItem
	for i := range items {
		item := &items[i]
		if item.ID == proposal.TranscriptEvidenceID && item.Modality == model.ChunkModalityTranscript {
			asr = item
		}
		if item.ID == proposal.VisualEvidenceID && (item.Modality == model.ChunkModalityVisualOCR || item.Modality == model.ChunkModalityVisualCaption) {
			visual = item
		}
	}
	if asr == nil || visual == nil || !containsTerm(asr.Content, from) || !containsTerm(visual.Content, to) || !knownOverlap(*asr, *visual) {
		return false
	}
	for _, item := range items {
		if (item.Modality == model.ChunkModalityVisualOCR || item.Modality == model.ChunkModalityVisualCaption) && knownOverlap(*asr, item) && containsTerm(item.Content, from) {
			return false
		}
	}
	return true
}

func visualTerms(item model.SourceSnapshotItem) []string {
	seen := map[string]bool{}
	terms := make([]string, 0)
	add := func(term string) {
		term = strings.TrimSpace(term)
		key := strings.ToLower(term)
		if !seen[key] && utf8.RuneCountInString(term) >= 2 && utf8.RuneCountInString(term) <= 40 {
			seen[key] = true
			terms = append(terms, term)
		}
	}
	for _, term := range studyLatinTerm.FindAllString(item.Content, -1) {
		first, _ := utf8.DecodeRuneInString(term)
		if unicode.IsUpper(first) || strings.IndexFunc(term, unicode.IsDigit) >= 0 || (item.Modality == model.ChunkModalityVisualOCR && strings.TrimSpace(item.Content) == term) {
			add(term)
		}
	}
	for _, phrase := range studyLatinPhrase.FindAllString(item.Content, -1) {
		add(strings.Join(strings.Fields(phrase), " "))
	}
	for _, match := range studyQuotedTerm.FindAllStringSubmatch(item.Content, -1) {
		add(match[1])
	}
	if item.Modality == model.ChunkModalityVisualOCR {
		for _, line := range strings.Split(item.Content, "\n") {
			line = strings.TrimSpace(line)
			if utf8.RuneCountInString(line) >= 2 && utf8.RuneCountInString(line) <= 16 && allHan(line) {
				add(line)
			}
		}
	}
	return terms
}

func allHan(s string) bool {
	for _, r := range s {
		if !unicode.Is(unicode.Han, r) {
			return false
		}
	}
	return true
}

func hasOCR(c StudyTermCandidate) bool {
	for _, s := range c.VisualSupport {
		if s.Modality == model.ChunkModalityVisualOCR {
			return true
		}
	}
	return false
}

func addTermSource(dst *[]StudyTermSource, item model.SourceSnapshotItem, term string, limit int) {
	for _, old := range *dst {
		if old.EvidenceID == item.ID {
			return
		}
	}
	if len(*dst) >= limit {
		return
	}
	*dst = append(*dst, StudyTermSource{EvidenceID: item.ID, Modality: item.Modality, Excerpt: termExcerpt(item.Content, term), StartMS: item.StartMS, EndMS: item.EndMS})
}

func termExcerpt(content, term string) string {
	idx := strings.Index(strings.ToLower(content), strings.ToLower(term))
	if idx < 0 {
		return ""
	}
	runes := []rune(content)
	start := utf8.RuneCountInString(content[:idx])
	end := start + utf8.RuneCountInString(term)
	if start > 32 {
		start -= 32
	} else {
		start = 0
	}
	if end+32 < len(runes) {
		end += 32
	} else {
		end = len(runes)
	}
	return string(runes[start:end])
}

func containsTerm(content, term string) bool {
	if term == "" {
		return false
	}
	lower, target := strings.ToLower(content), strings.ToLower(term)
	for offset := 0; offset < len(lower); {
		idx := strings.Index(lower[offset:], target)
		if idx < 0 {
			return false
		}
		start, end := offset+idx, offset+idx+len(target)
		if (start == 0 || !termRune([]rune(lower[:start])[utf8.RuneCountInString(lower[:start])-1])) && (end == len(lower) || !termRune([]rune(lower[end:])[0])) {
			return true
		}
		offset = end
	}
	return false
}

func termRune(r rune) bool { return r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_' }

func knownOverlap(a, b model.SourceSnapshotItem) bool {
	return a.StartMS != nil && a.EndMS != nil && b.StartMS != nil && b.EndMS != nil && *a.EndMS > *a.StartMS && *b.EndMS > *b.StartMS && *a.StartMS < *b.EndMS && *b.StartMS < *a.EndMS
}

func overlapsAny(item model.SourceSnapshotItem, sources []StudyTermSource) bool {
	for _, s := range sources {
		if item.StartMS == nil || item.EndMS == nil || s.StartMS == nil || s.EndMS == nil || (*item.StartMS < *s.EndMS && *s.StartMS < *item.EndMS) {
			return true
		}
	}
	return false
}

func plausibleAlternative(word, term string) bool {
	if !fullLatinName(word) || !fullLatinName(term) {
		return false
	}
	a, b := strings.ToLower(word), strings.ToLower(term)
	if a == b || len(a) < 2 || len(b) < 2 || len(a) > 40 || len(b) > 40 || absInt(len(a)-len(b)) > 2 {
		return false
	}
	// Bounded edit distance only raises a review flag; it never picks a winner.
	prev := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		curr := make([]int, len(b)+1)
		curr[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			curr[j] = minInt(prev[j]+1, curr[j-1]+1, prev[j-1]+cost)
		}
		prev = curr
	}
	return prev[len(b)] <= 2
}

func fullLatinName(s string) bool {
	return studyLatinTerm.FindString(s) == s || studyLatinPhrase.FindString(s) == s
}

func absInt(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

func minInt(a, b, c int) int {
	if b < a {
		a = b
	}
	if c < a {
		a = c
	}
	return a
}
