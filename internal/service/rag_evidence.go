package service

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"vid-lens/internal/model"
)

const (
	defaultCitationEvidenceRunes = 160
	maxCitationContextRunes      = 4000
	maxCitationSentencesPerChunk = 48
	maxCitationEvidenceRunes     = 2000
)

var (
	spaceBeforePunctuation = regexp.MustCompile(`[ \t]+([，。！？；：、,.!?;:])`)
	excessiveBlankLines    = regexp.MustCompile(`(?:[ \t]*\r?\n){3,}`)
)

type finalizedAnswer struct {
	Answer    string
	Citations []Citation
}

type citationTokenRange struct {
	start int
	end   int
	ids   []string
}

type markdownCodeRange struct {
	start int
	end   int
}

// finalizeAnswerCitations selects only cited sentence candidates and leaves
// canonical markers in the answer for inline source links. Unsupported IDs are
// removed; Markdown code and links remain literal.
func finalizeAnswerCitations(rawAnswer string, candidates []Citation) finalizedAnswer {
	protected := extractMarkdownCodeRanges(rawAnswer)
	tokens := extractCitationTokenRanges(rawAnswer, protected)
	referenced := collectReferencedCitationIDs(tokens)
	selected := selectReferencedCitations(referenced, candidates)
	for i := range selected {
		selected[i].SupportStatus = "not_checked"
		for _, token := range tokens {
			for _, id := range token.ids {
				if id != selected[i].CitationID {
					continue
				}
				start := strings.LastIndex(rawAnswer[:token.start], "\n") + 1
				claim := rawAnswer[start:token.start]
				claim = cleanVisibleAnswer(claim, extractCitationTokenRanges(claim, nil), nil, nil)
				if claim != "" {
					selected[i].ClaimTexts = append(selected[i].ClaimTexts, claim)
				}
			}
		}
	}
	validIDs := make(map[string]struct{}, len(selected))
	for _, candidate := range selected {
		validIDs[candidate.CitationID] = struct{}{}
	}
	return finalizedAnswer{
		Answer:    cleanVisibleAnswer(rawAnswer, tokens, protected, validIDs),
		Citations: selected,
	}
}

// parseReferencedCitationIDs parses the [Cx] tokens in answer (skipping Markdown
// code regions) and returns the normalized referenced citation id set. Shared by
// finalizeAnswerCitations and docs/architecture/retrieval.md ⑨ evidence-constraint violation detection
// so both walk the same parse pipeline (no duplicated token-extraction logic).
func parseReferencedCitationIDs(answer string) map[string]struct{} {
	protected := extractMarkdownCodeRanges(answer)
	tokens := extractCitationTokenRanges(answer, protected)
	return collectReferencedCitationIDs(tokens)
}

// stripCitationTokensVisible returns the answer with [Cx] citation tokens removed
// (the visible answer finalizeAnswerCitations produces when no citation selection
// matters). docs/architecture/retrieval.md ⑨ uses it to derive a re-retrieval query from the violating
// LLM answer without re-implementing token stripping.
func stripCitationTokensVisible(answer string) string {
	return finalizeAnswerCitations(answer, nil).Answer
}

// selectAnswerCitations remains as a compatibility wrapper for callers that
// only need citation selection.
func selectAnswerCitations(answer string, candidates []Citation) []Citation {
	return finalizeAnswerCitations(answer, candidates).Citations
}

// extractMarkdownCodeRanges finds fenced, indented, and inline code regions.
// Ranges are byte offsets into the original UTF-8 answer and never overlap.
func extractMarkdownCodeRanges(answer string) []markdownCodeRange {
	blocks := extractMarkdownBlockCodeRanges(answer)
	ranges := make([]markdownCodeRange, 0, len(blocks))
	blockIndex := 0
	for i := 0; i < len(answer); {
		if blockIndex < len(blocks) && i == blocks[blockIndex].start {
			ranges = append(ranges, blocks[blockIndex])
			i = blocks[blockIndex].end
			blockIndex++
			continue
		}
		if answer[i] != '`' || isEscapedAt(answer, i) {
			i++
			continue
		}

		runLength := markerRunLength(answer, i, '`')
		limit := len(answer)
		if blockIndex < len(blocks) {
			limit = blocks[blockIndex].start
		}
		closingStart := findMatchingBacktickRun(answer, i+runLength, limit, runLength)
		if closingStart < 0 {
			i += runLength
			continue
		}
		end := closingStart + runLength
		ranges = append(ranges, markdownCodeRange{start: i, end: end})
		i = end
	}
	return ranges
}

func extractMarkdownBlockCodeRanges(text string) []markdownCodeRange {
	ranges := make([]markdownCodeRange, 0)
	previousLineBlank := true
	for lineStart := 0; lineStart < len(text); {
		lineEnd, nextLine := markdownLineBounds(text, lineStart)
		if marker, runLength, ok := markdownFenceOpening(text, lineStart, lineEnd); ok {
			end := findMarkdownFenceEnd(text, nextLine, marker, runLength)
			ranges = append(ranges, markdownCodeRange{start: lineStart, end: end})
			lineStart = end
			previousLineBlank = true
			continue
		}
		if previousLineBlank && isIndentedMarkdownCodeLine(text[lineStart:lineEnd]) {
			end := nextLine
			for end < len(text) {
				followingEnd, followingNext := markdownLineBounds(text, end)
				line := text[end:followingEnd]
				if !isMarkdownBlankLine(line) && !isIndentedMarkdownCodeLine(line) {
					break
				}
				end = followingNext
			}
			ranges = append(ranges, markdownCodeRange{start: lineStart, end: end})
			lineStart = end
			previousLineBlank = true
			continue
		}
		previousLineBlank = isMarkdownBlankLine(text[lineStart:lineEnd])
		lineStart = nextLine
	}
	return ranges
}

func markdownLineBounds(text string, lineStart int) (lineEnd, nextLine int) {
	relativeEnd := strings.IndexByte(text[lineStart:], '\n')
	if relativeEnd < 0 {
		return len(text), len(text)
	}
	lineEnd = lineStart + relativeEnd
	return lineEnd, lineEnd + 1
}

func markdownFenceOpening(text string, lineStart, lineEnd int) (byte, int, bool) {
	delimiterStart, ok := markdownFenceDelimiterStart(text, lineStart, lineEnd)
	if !ok || delimiterStart >= lineEnd || (text[delimiterStart] != '`' && text[delimiterStart] != '~') {
		return 0, 0, false
	}
	marker := text[delimiterStart]
	runLength := markerRunLength(text, delimiterStart, marker)
	if runLength < 3 {
		return 0, 0, false
	}
	return marker, runLength, true
}

func markdownFenceDelimiterStart(text string, lineStart, lineEnd int) (int, bool) {
	contentStart := lineStart
	for contentStart < lineEnd && contentStart-lineStart < 3 && text[contentStart] == ' ' {
		contentStart++
	}
	if contentStart < lineEnd && text[contentStart] == ' ' {
		return 0, false
	}

	for contentStart < lineEnd && text[contentStart] == '>' {
		contentStart++
		if contentStart < lineEnd && (text[contentStart] == ' ' || text[contentStart] == '\t') {
			contentStart++
		}
	}

	if markerEnd, ok := markdownListMarkerEnd(text, contentStart, lineEnd); ok {
		contentStart = markerEnd
		for contentStart < lineEnd && (text[contentStart] == ' ' || text[contentStart] == '\t') {
			contentStart++
		}
	}
	return contentStart, true
}

func markdownListMarkerEnd(text string, start, lineEnd int) (int, bool) {
	if start >= lineEnd {
		return 0, false
	}
	if text[start] == '-' || text[start] == '+' || text[start] == '*' {
		end := start + 1
		return end, end < lineEnd && (text[end] == ' ' || text[end] == '\t')
	}
	end := start
	for end < lineEnd && text[end] >= '0' && text[end] <= '9' {
		end++
	}
	if end == start || end >= lineEnd || (text[end] != '.' && text[end] != ')') {
		return 0, false
	}
	end++
	return end, end < lineEnd && (text[end] == ' ' || text[end] == '\t')
}

func findMarkdownFenceEnd(text string, lineStart int, marker byte, openingLength int) int {
	for lineStart < len(text) {
		lineEnd, nextLine := markdownLineBounds(text, lineStart)
		delimiterStart, ok := markdownFenceDelimiterStart(text, lineStart, lineEnd)
		runLength := 0
		if ok && delimiterStart < lineEnd && text[delimiterStart] == marker {
			runLength = markerRunLength(text, delimiterStart, marker)
		}
		if runLength >= openingLength && onlyFenceTrailingSpace(text[delimiterStart+runLength:lineEnd]) {
			return nextLine
		}
		lineStart = nextLine
	}
	return len(text)
}

func markerRunLength(text string, start int, marker byte) int {
	end := start
	for end < len(text) && text[end] == marker {
		end++
	}
	return end - start
}

func onlyFenceTrailingSpace(text string) bool {
	for i := range text {
		if text[i] != ' ' && text[i] != '\t' && text[i] != '\r' {
			return false
		}
	}
	return true
}

func isIndentedMarkdownCodeLine(line string) bool {
	if len(line) > 0 && line[0] == '\t' {
		return true
	}
	return len(line) >= 4 && line[:4] == "    "
}

func isMarkdownBlankLine(line string) bool {
	for i := range line {
		if line[i] != ' ' && line[i] != '\t' && line[i] != '\r' {
			return false
		}
	}
	return true
}

func findMatchingBacktickRun(text string, start, limit, openingLength int) int {
	for i := start; i < limit; {
		if text[i] != '`' {
			i++
			continue
		}
		runLength := markerRunLength(text, i, '`')
		if runLength == openingLength {
			return i
		}
		i += runLength
	}
	return -1
}

// extractCitationTokenRanges scans complete square-bracket groups outside
// Markdown code. An unmatched prose bracket (such as time=[start,end)) must
// not hide later citations, while complete nested groups remain literal.
func extractCitationTokenRanges(answer string, protected []markdownCodeRange) []citationTokenRange {
	ranges := make([]citationTokenRange, 0)
	stack := make([]int, 0)
	closing := make(map[int]int)
	protectedIndex := 0
	for byteIndex, r := range answer {
		for protectedIndex < len(protected) && byteIndex >= protected[protectedIndex].end {
			protectedIndex++
		}
		if protectedIndex < len(protected) && byteIndex >= protected[protectedIndex].start {
			continue
		}

		switch r {
		case '[':
			stack = append(stack, byteIndex)
		case ']':
			if len(stack) == 0 {
				continue
			}
			start := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			closing[start] = byteIndex
		}
	}
	// Only matched groups suppress their nested brackets. Unmatched opening
	// brackets are ordinary text, so the next complete [Cx] is considered.
	skipUntil := 0
	for start, r := range answer {
		if r != '[' || start < skipUntil {
			continue
		}
		close, matched := closing[start]
		if !matched {
			continue
		}
		end := close + 1
		skipUntil = end
		if isEscapedAt(answer, start) || isMarkdownCitationLabel(answer, start, end) {
			continue
		}
		if ids, ok := parseCitationList(answer[start+1 : close]); ok {
			ranges = append(ranges, citationTokenRange{start: start, end: end, ids: ids})
		}
	}
	return ranges
}

func isMarkdownCitationLabel(text string, openBracket, end int) bool {
	if end < len(text) && text[end] == '(' {
		return true
	}
	if end < len(text) && text[end] == ':' && markdownReferenceDefinitionStart(text, openBracket) {
		return true
	}
	if end < len(text) && text[end] == '[' {
		if close := strings.IndexByte(text[end+1:], ']'); close >= 0 {
			reference := text[end+1 : end+1+close]
			if _, internal := parseCitationList(reference); !internal {
				return true
			}
		}
	}
	if openBracket > 0 && text[openBracket-1] == ']' {
		if previousOpen := strings.LastIndexByte(text[:openBracket-1], '['); previousOpen >= 0 {
			previousLabel := text[previousOpen+1 : openBracket-1]
			if previousLabel != "" {
				_, internal := parseCitationList(previousLabel)
				return !internal
			}
		}
	}
	return false
}

func markdownReferenceDefinitionStart(text string, openBracket int) bool {
	lineStart := strings.LastIndexByte(text[:openBracket], '\n') + 1
	prefix := text[lineStart:openBracket]
	return len(prefix) <= 3 && strings.Trim(prefix, " ") == ""
}

func isEscapedAt(text string, byteIndex int) bool {
	backslashes := 0
	for i := byteIndex - 1; i >= 0 && text[i] == '\\'; i-- {
		backslashes++
	}
	return backslashes%2 == 1
}

func parseCitationList(content string) ([]string, bool) {
	runes := []rune(content)
	ids := make([]string, 0, 1)
	segmentStart := 0
	for i, r := range runes {
		if !isCitationSeparator(r) {
			continue
		}
		id, ok := normalizeCitationID(trimCitationSpaces(string(runes[segmentStart:i])))
		if !ok {
			return nil, false
		}
		ids = append(ids, id)
		segmentStart = i + 1
	}
	id, ok := normalizeCitationID(trimCitationSpaces(string(runes[segmentStart:])))
	if !ok {
		return nil, false
	}
	return append(ids, id), true
}

func isCitationSeparator(r rune) bool {
	switch r {
	case ',', '，', '、':
		return true
	default:
		return false
	}
}

func trimCitationSpaces(text string) string {
	return strings.TrimFunc(text, func(r rune) bool {
		return r == '\t' || unicode.Is(unicode.Zs, r)
	})
}

func normalizeCitationID(raw string) (string, bool) {
	runes := []rune(raw)
	if len(runes) < 2 || (runes[0] != 'C' && runes[0] != 'c') || runes[1] < '1' || runes[1] > '9' {
		return "", false
	}
	for _, r := range runes[2:] {
		if r < '0' || r > '9' {
			return "", false
		}
	}
	return "C" + string(runes[1:]), true
}

func collectReferencedCitationIDs(tokens []citationTokenRange) map[string]struct{} {
	referenced := make(map[string]struct{})
	for _, token := range tokens {
		for _, id := range token.ids {
			referenced[id] = struct{}{}
		}
	}
	return referenced
}

func selectReferencedCitations(referenced map[string]struct{}, candidates []Citation) []Citation {
	selected := make([]Citation, 0, len(referenced))
	selectedIDs := make(map[string]struct{}, len(referenced))
	for _, candidate := range candidates {
		normalizedID, ok := normalizeCitationID(strings.TrimSpace(candidate.CitationID))
		if !ok {
			continue
		}
		if _, ok := referenced[normalizedID]; !ok {
			continue
		}
		if _, duplicate := selectedIDs[normalizedID]; duplicate {
			continue
		}
		candidate.CitationID = normalizedID
		selected = append(selected, candidate)
		selectedIDs[normalizedID] = struct{}{}
	}
	return selected
}

func cleanVisibleAnswer(answer string, tokens []citationTokenRange, protected []markdownCodeRange, validIDs map[string]struct{}) string {
	var visible strings.Builder
	visible.Grow(len(answer))
	tokenIndex := 0

	appendCleanOutside := func(start, end int) {
		var outside strings.Builder
		outside.Grow(end - start)
		cursor := start
		for tokenIndex < len(tokens) && tokens[tokenIndex].end <= start {
			tokenIndex++
		}
		for tokenIndex < len(tokens) && tokens[tokenIndex].start < end {
			token := tokens[tokenIndex]
			outside.WriteString(answer[cursor:token.start])
			seen := make(map[string]struct{}, len(token.ids))
			for _, id := range token.ids {
				if _, valid := validIDs[id]; !valid {
					continue
				}
				if _, duplicate := seen[id]; duplicate {
					continue
				}
				outside.WriteString("[" + id + "]")
				seen[id] = struct{}{}
			}
			cursor = token.end
			tokenIndex++
		}
		outside.WriteString(answer[cursor:end])

		cleaned := spaceBeforePunctuation.ReplaceAllString(outside.String(), "$1")
		cleaned = excessiveBlankLines.ReplaceAllString(cleaned, "\n\n")
		visible.WriteString(cleaned)
	}

	cursor := 0
	for _, code := range protected {
		appendCleanOutside(cursor, code.start)
		visible.WriteString(answer[code.start:code.end])
		cursor = code.end
	}
	appendCleanOutside(cursor, len(answer))

	cleaned := visible.String()
	if len(protected) == 0 || protected[0].start > 0 {
		cleaned = strings.TrimLeftFunc(cleaned, unicode.IsSpace)
	}
	if len(protected) == 0 || protected[len(protected)-1].end < len(answer) {
		cleaned = strings.TrimRightFunc(cleaned, unicode.IsSpace)
	}
	return cleaned
}

// buildCitationSet keeps generation context at chunk granularity, but assigns
// each source sentence its own citation ID. Enumeration does not depend on the
// question: writer prompts and answer finalization must see the same ID table.
func buildCitationSet(_ string, contexts []RetrievedChunk) ([]RetrievedChunk, []Citation) {
	filteredContexts := make([]RetrievedChunk, 0, len(contexts))
	citations := make([]Citation, 0, len(contexts))
	for _, chunk := range contexts {
		chunk.Modality = normalizedChunkModality(chunk.Modality)
		chunk.TimeRangeStatus = normalizedTimeRangeStatus(chunk.TimeRangeStatus, chunk.StartMS, chunk.EndMS)
		chunk.SourceMappingStatus = normalizedMappingStatus(chunk.SourceMappingStatus)
		if chunk.SourceMappingStatus != model.ChunkSourceMapped || chunk.TimeRangeStatus == model.ChunkTimeRangeUnknown {
			chunk.TimeRangeStatus = model.ChunkTimeRangeUnknown
			chunk.StartMS, chunk.EndMS = 0, 0
		}
		rawAnchor := chunk.AnchorContent
		if strings.TrimSpace(rawAnchor) == "" {
			rawAnchor = chunk.Content
		}
		anchor := strings.TrimSpace(rawAnchor)
		if anchor == "" {
			continue
		}
		anchorRefs := citationAnchorRefs(rawAnchor, chunk.SourceRefs)

		if chunk.ContextTimeStatus == "" {
			chunk.ContextStartMS, chunk.ContextEndMS, chunk.ContextTimeStatus = chunk.StartMS, chunk.EndMS, chunk.TimeRangeStatus
		}
		filteredContexts = append(filteredContexts, chunk)
		seen := make(map[string]struct{})
		chunkCitations := make([]Citation, 0)
		for _, sentence := range citationSentenceQuotes(anchor, anchorRefs) {
			evidence := sentence.Content
			startMS, endMS, timeStatus := chunk.StartMS, chunk.EndMS, chunk.TimeRangeStatus
			sourceRefs := append([]ChunkSourceRef(nil), chunk.SourceRefs...)
			sourceIdentity := ""
			// A sentence can inherit a narrower source interval only when one
			// source observation contains it verbatim. Never interpolate time.
			if ref, ok := citationSourceRef(sentence, anchor, anchorRefs); ok && chunk.SourceMappingStatus == model.ChunkSourceMapped {
				startMS, endMS, timeStatus = ref.StartMS, ref.EndMS, ref.TimeRangeStatus
				sourceRefs = []ChunkSourceRef{ref}
				sourceIdentity = ref.SourceType + ":" + ref.StableID
			} else if timeStatus == model.ChunkTimeRangeExact && len(sourceRefs) > 1 {
				// The bounding interval of several observations is only coarse
				// when the quoted occurrence cannot be assigned to one of them.
				timeStatus = model.ChunkTimeRangeCoarse
			}
			for i := range sourceRefs {
				sourceRefs[i].Content = ""
				sourceRefs[i].TextStart, sourceRefs[i].TextEnd = 0, 0
			}
			key := fmt.Sprintf("%d:%d:%s:%s:%s", startMS, endMS, timeStatus, sourceIdentity, evidence)
			if _, duplicate := seen[key]; duplicate {
				continue
			}
			seen[key] = struct{}{}
			displayContext, truncated := boundedCitationContext(chunk.Content, evidence)
			chunkCitations = append(chunkCitations, Citation{
				TaskID:         chunk.TaskID,
				VideoTitle:     chunk.VideoTitle,
				EvidenceID:     chunk.EvidenceID,
				ChunkID:        chunk.ChunkID,
				ChunkIndex:     chunk.ChunkIndex,
				Score:          chunk.Score,
				Content:        evidence,
				AnchorQuote:    evidence,
				DisplayContext: displayContext,
				Source:         chunk.Source,
				VectorRank:     chunk.VectorRank,
				KeywordRank:    chunk.KeywordRank,
				RRFScore:       chunk.RRFScore,
				RerankScore:    chunk.RerankScore,
				FinalRank:      chunk.FinalRank,
				Modality:       chunk.Modality, StartMS: startMS, EndMS: endMS,
				TimeRangeStatus: timeStatus,
				ContextStartMS:  chunk.ContextStartMS, ContextEndMS: chunk.ContextEndMS,
				ContextTimeStatus:       chunk.ContextTimeStatus,
				ContextSourceRefs:       append([]ChunkSourceRef(nil), chunk.ContextSourceRefs...),
				DisplayContextTruncated: truncated || chunk.WindowTruncated,
				QuoteTruncated:          utf8.RuneCountInString(evidence) == maxCitationEvidenceRunes,
				SourceMappingStatus:     chunk.SourceMappingStatus,
				SourceRefs:              sourceRefs,
				ModalityRank:            chunk.ModalityRank, ModalityScore: chunk.ModalityScore, ModalityIntent: chunk.ModalityIntent,
			})
		}
		for _, citation := range boundedSentenceCandidates(chunkCitations) {
			citation.CitationID = "C" + strconv.Itoa(len(citations)+1)
			citations = append(citations, citation)
		}
	}
	return filteredContexts, citations
}

// Split at known source-observation boundaries as well as sentence endings so
// a sentence straddling two ASR observations does not inherit their union.
type citationSentenceQuote struct {
	Content    string
	Start, End int // rune offsets in the trimmed retrieval anchor
}

func citationAnchorRefs(rawAnchor string, refs []ChunkSourceRef) []ChunkSourceRef {
	leadingRunes := utf8.RuneCountInString(rawAnchor) - utf8.RuneCountInString(strings.TrimLeftFunc(rawAnchor, unicode.IsSpace))
	adjusted := append([]ChunkSourceRef(nil), refs...)
	for i := range adjusted {
		if adjusted[i].TextEnd > adjusted[i].TextStart {
			adjusted[i].TextStart -= leadingRunes
			adjusted[i].TextEnd -= leadingRunes
		}
	}
	return adjusted
}

func citationRefTextRange(anchor []rune, ref ChunkSourceRef) (int, int, bool) {
	if ref.Content == "" || ref.TextStart < 0 || ref.TextEnd <= ref.TextStart || ref.TextEnd > len(anchor) {
		return 0, 0, false
	}
	if string(anchor[ref.TextStart:ref.TextEnd]) != ref.Content {
		return 0, 0, false
	}
	return ref.TextStart, ref.TextEnd, true
}

func citationSentenceQuotes(anchor string, refs []ChunkSourceRef) []citationSentenceQuote {
	anchorRunes := []rune(anchor)
	boundaries := []int{0, len(anchorRunes)}
	for _, ref := range refs {
		if start, end, ok := citationRefTextRange(anchorRunes, ref); ok {
			boundaries = append(boundaries, start, end)
			continue
		}
		if ref.TextStart != 0 || ref.TextEnd != 0 {
			continue // malformed persisted offsets cannot be inferred from text
		}
		content := strings.TrimSpace(ref.Content)
		if content == "" || strings.Count(anchor, content) != 1 {
			continue
		}
		start := utf8.RuneCountInString(anchor[:strings.Index(anchor, content)])
		boundaries = append(boundaries, start, start+utf8.RuneCountInString(content))
	}
	sort.Ints(boundaries)
	quotes := make([]citationSentenceQuote, 0)
	for i := 1; i < len(boundaries); i++ {
		if boundaries[i] == boundaries[i-1] {
			continue
		}
		text := anchorRunes[boundaries[i-1]:boundaries[i]]
		for start := 0; start < len(text); {
			end := start
			for end < len(text) && end-start < maxCitationEvidenceRunes {
				r := text[end]
				end++
				terminator := isUsefulEvidenceTerminator(r) || r == '\n' || r == '；' || r == ';'
				if r == '.' && (end == len(text) || unicode.IsSpace(text[end])) {
					terminator = true
				}
				if terminator {
					for end < len(text) && end-start < maxCitationEvidenceRunes && isClosingPunctuation(text[end]) {
						end++
					}
					break
				}
			}
			fragment := string(text[start:end])
			if quote := strings.TrimSpace(fragment); strings.IndexFunc(quote, func(r rune) bool { return unicode.IsLetter(r) || unicode.IsNumber(r) }) >= 0 {
				leading := utf8.RuneCountInString(fragment) - utf8.RuneCountInString(strings.TrimLeftFunc(fragment, unicode.IsSpace))
				quoteStart := boundaries[i-1] + start + leading
				quotes = append(quotes, citationSentenceQuote{Content: quote, Start: quoteStart, End: quoteStart + utf8.RuneCountInString(quote)})
			}
			start = end
		}
	}
	return quotes
}

func citationSourceRef(quote citationSentenceQuote, anchor string, refs []ChunkSourceRef) (ChunkSourceRef, bool) {
	var matched ChunkSourceRef
	count := 0
	anchorRunes := []rune(anchor)
	for _, ref := range refs {
		if strings.TrimSpace(ref.Content) == "" || !strings.Contains(ref.Content, quote.Content) {
			continue
		}
		if start, end, ok := citationRefTextRange(anchorRunes, ref); ok {
			if start > quote.Start || end < quote.End {
				continue
			}
		} else if ref.TextStart != 0 || ref.TextEnd != 0 || strings.Count(anchor, quote.Content) != 1 {
			// Invalid offsets and repeated legacy text cannot choose a native
			// occurrence reliably, even if one source has a precise time.
			return ChunkSourceRef{}, false
		}
		ref.TimeRangeStatus = normalizedTimeRangeStatus(ref.TimeRangeStatus, ref.StartMS, ref.EndMS)
		matched = ref
		count++
	}
	return matched, count == 1 && matched.TimeRangeStatus != model.ChunkTimeRangeUnknown
}

func boundedSentenceCandidates(candidates []Citation) []Citation {
	if len(candidates) <= maxCitationSentencesPerChunk {
		return candidates
	}
	// Include both ends and distribute the remaining slots across the anchor;
	// a long observation must not silently make its final facts unciteable.
	result := make([]Citation, 0, maxCitationSentencesPerChunk)
	for i := 0; i < maxCitationSentencesPerChunk; i++ {
		index := i * (len(candidates) - 1) / (maxCitationSentencesPerChunk - 1)
		result = append(result, candidates[index])
	}
	return result
}

func boundedCitationContext(source, quote string) (string, bool) {
	text := []rune(strings.TrimSpace(source))
	if len(text) <= maxCitationContextRunes {
		return string(text), false
	}
	byteStart := strings.Index(string(text), quote)
	quoteStart := 0
	if byteStart >= 0 {
		quoteStart = utf8.RuneCountInString(string(text)[:byteStart])
	}
	start := quoteStart - (maxCitationContextRunes-utf8.RuneCountInString(quote))/2
	if start < 0 {
		start = 0
	}
	if start+maxCitationContextRunes > len(text) {
		start = len(text) - maxCitationContextRunes
	}
	return strings.TrimSpace(string(text[start : start+maxCitationContextRunes])), true
}

func buildCitations(question string, contexts []RetrievedChunk) []Citation {
	_, citations := buildCitationSet(question, contexts)
	return citations
}

func formatCitationCandidates(citations []Citation) string {
	lines := make([]string, 0, len(citations))
	for _, citation := range citations {
		lines = append(lines, fmt.Sprintf("[%s] (task_id=%d, chunk %d, modality=%s, time=[%d,%d), time_status=%s) %s", citation.CitationID,
			citation.TaskID, citation.ChunkIndex, citation.Modality, citation.StartMS, citation.EndMS, citation.TimeRangeStatus, citation.AnchorQuote))
	}
	return strings.Join(lines, "\n")
}

func formatCitationGenerationContext(contexts []RetrievedChunk) string {
	lines := make([]string, 0, len(contexts))
	for _, chunk := range contexts {
		lines = append(lines, fmt.Sprintf("%s\n%s", describeRetrievedChunk(chunk), chunk.Content))
	}
	return strings.Join(lines, "\n\n")
}

// extractEvidence selects the most query-relevant bounded window while keeping
// the text byte-for-byte traceable to the source after surrounding whitespace
// is trimmed. It performs no summarization and no model call.
func extractEvidence(question, matchedQuery, anchor string, maxRunes int) string {
	anchor = strings.TrimSpace(anchor)
	if anchor == "" {
		return ""
	}
	if maxRunes <= 0 {
		maxRunes = defaultCitationEvidenceRunes
	}
	// Soft budget prefers complete sentences. A hard cut is reported in the DTO.
	terms := ExtractQueryTerms(strings.TrimSpace(question + " " + matchedQuery))
	runes := []rune(anchor)
	if len(runes) <= maxRunes {
		return endEvidenceAfterRelevantPhrase(anchor, terms, maxRunes)
	}
	type sentence struct{ start, end int }
	var sentences []sentence
	start := 0
	for i, r := range runes {
		if isUsefulEvidenceTerminator(r) || r == '\n' || (r == '.' && (i+1 == len(runes) || unicode.IsSpace(runes[i+1]))) {
			sentences = append(sentences, sentence{start, i + 1})
			start = i + 1
		}
	}
	if start < len(runes) {
		sentences = append(sentences, sentence{start, len(runes)})
	}
	best, score := 0, -1
	for i, part := range sentences {
		v := evidenceTermScore(string(runes[part.start:part.end]), terms)
		if v > score {
			best, score = i, v
		}
	}
	part := sentences[best]
	// Carry the preceding sentence for conditions, negation and cross-sentence references.
	if best > 0 && part.end-sentences[best-1].start <= maxRunes*2 {
		part.start = sentences[best-1].start
	}
	for best+1 < len(sentences) && sentences[best+1].end-part.start <= maxRunes {
		best++
		part.end = sentences[best].end
	}
	const hardBudget = 2000
	if part.end-part.start > hardBudget {
		part.end = part.start + hardBudget
	}
	return strings.TrimSpace(string(runes[part.start:part.end]))
}

func relevantEvidenceWindows(anchor string, terms []string, maxRunes int) []string {
	anchorRunes := []rune(anchor)
	foldedAnchor := foldRunes(anchorRunes)
	windows := make([]string, 0)
	for _, term := range terms {
		termRunes := foldRunes([]rune(strings.TrimSpace(term)))
		if len(termRunes) == 0 || len(termRunes) > maxRunes || len(termRunes) > len(anchorRunes) {
			continue
		}
		for occurrenceStart := 0; occurrenceStart+len(termRunes) <= len(anchorRunes); occurrenceStart++ {
			if !runesEqual(foldedAnchor[occurrenceStart:occurrenceStart+len(termRunes)], termRunes) {
				continue
			}
			windowStart := occurrenceStart - (maxRunes-len(termRunes))/2
			if windowStart < 0 {
				windowStart = 0
			}
			if windowStart+maxRunes > len(anchorRunes) {
				windowStart = len(anchorRunes) - maxRunes
			}
			windows = append(windows, string(anchorRunes[windowStart:windowStart+maxRunes]))
		}
	}
	return windows
}

func runesEqual(left, right []rune) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func endEvidenceAfterRelevantPhrase(content string, terms []string, maxRunes int) string {
	runes := []rune(content)
	if maxRunes > 0 && len(runes) > maxRunes {
		runes = runes[:maxRunes]
	}

	relevantEnd := lastRelevantRuneEnd(runes, terms)
	if relevantEnd >= 0 {
		for i := relevantEnd; i < len(runes); i++ {
			if !isUsefulEvidenceTerminator(runes[i]) {
				continue
			}
			end := i + 1
			for end < len(runes) && isClosingPunctuation(runes[end]) {
				end++
			}
			return trimEvidenceWindow(string(runes[:end]))
		}
	}
	return trimEvidenceWindow(string(runes))
}

func lastRelevantRuneEnd(content []rune, terms []string) int {
	foldedContent := foldRunes(content)
	lastEnd := -1
	for _, term := range terms {
		termRunes := foldRunes([]rune(strings.TrimSpace(term)))
		if len(termRunes) == 0 || len(termRunes) > len(foldedContent) {
			continue
		}
		for start := 0; start+len(termRunes) <= len(foldedContent); start++ {
			matched := true
			for i := range termRunes {
				if foldedContent[start+i] != termRunes[i] {
					matched = false
					break
				}
			}
			if matched && start+len(termRunes) > lastEnd {
				lastEnd = start + len(termRunes)
			}
		}
	}
	return lastEnd
}

func foldRunes(runes []rune) []rune {
	folded := make([]rune, len(runes))
	for i, r := range runes {
		folded[i] = unicode.ToLower(r)
	}
	return folded
}

func isUsefulEvidenceTerminator(r rune) bool {
	switch r {
	case '。', '！', '？', '!', '?':
		return true
	default:
		return false
	}
}

func trimEvidenceWindow(content string) string {
	return strings.TrimSpace(content)
}

func evidenceTermScore(content string, terms []string) int {
	content = strings.ToLower(content)
	score := 0
	for _, term := range terms {
		term = strings.ToLower(strings.TrimSpace(term))
		if term == "" || !strings.Contains(content, term) {
			continue
		}
		length := utf8.RuneCountInString(term)
		// Longer terms carry more intent than incidental CJK bigrams.
		score += length * length
	}
	return score
}
