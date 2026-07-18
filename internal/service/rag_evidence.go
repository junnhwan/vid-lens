package service

import (
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

const defaultCitationEvidenceRunes = 160

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

// finalizeAnswerCitations is the boundary between model-internal citation
// tokens and the user-visible answer. It uses supported tokens for evidence
// selection before removing those tokens from the answer.
func finalizeAnswerCitations(rawAnswer string, candidates []Citation) finalizedAnswer {
	protected := extractMarkdownCodeRanges(rawAnswer)
	tokens := extractCitationTokenRanges(rawAnswer, protected)
	referenced := collectReferencedCitationIDs(tokens)
	return finalizedAnswer{
		Answer:    cleanVisibleAnswer(rawAnswer, tokens, protected),
		Citations: selectReferencedCitations(referenced, candidates),
	}
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

// extractCitationTokenRanges scans top-level square brackets outside Markdown
// code and returns byte ranges only when the whole bracket content is a
// citation list. Byte offsets allow removal without rewriting Unicode text.
func extractCitationTokenRanges(answer string, protected []markdownCodeRange) []citationTokenRange {
	ranges := make([]citationTokenRange, 0)
	depth := 0
	start := -1
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
			if depth == 0 {
				start = byteIndex
			}
			depth++
		case ']':
			if depth == 0 {
				continue
			}
			depth--
			if depth != 0 {
				continue
			}

			end := byteIndex + utf8.RuneLen(r)
			if start < 0 || isEscapedAt(answer, start) || isMarkdownCitationLabel(answer, start, end) {
				start = -1
				continue
			}
			ids, ok := parseCitationList(answer[start+1 : byteIndex])
			if ok {
				ranges = append(ranges, citationTokenRange{start: start, end: end, ids: ids})
			}
			start = -1
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
	if len(selected) > 0 {
		if len(selected) > 2 {
			selected = selected[:2]
		}
		return selected
	}
	return fallbackCitations(candidates, 1)
}

func fallbackCitations(candidates []Citation, limit int) []Citation {
	fallback := make([]Citation, 0, limit)
	seen := make(map[string]struct{})
	for _, candidate := range candidates {
		normalizedID, ok := normalizeCitationID(strings.TrimSpace(candidate.CitationID))
		if !ok {
			continue
		}
		if _, duplicate := seen[normalizedID]; duplicate {
			continue
		}
		candidate.CitationID = normalizedID
		fallback = append(fallback, candidate)
		seen[normalizedID] = struct{}{}
		if len(fallback) == limit {
			break
		}
	}
	return fallback
}

func cleanVisibleAnswer(answer string, tokens []citationTokenRange, protected []markdownCodeRange) string {
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

// buildCitationSet separates internal retrieval contexts from public evidence.
// The returned Citation content is always a verbatim excerpt of the anchor
// chunk (or Content when no expansion happened); expanded neighbor context is
// never copied into the public DTO.
func buildCitationSet(question string, contexts []RetrievedChunk) ([]RetrievedChunk, []Citation) {
	filteredContexts := make([]RetrievedChunk, 0, len(contexts))
	citations := make([]Citation, 0, len(contexts))
	for _, chunk := range contexts {
		anchor := strings.TrimSpace(chunk.AnchorContent)
		if anchor == "" {
			anchor = strings.TrimSpace(chunk.Content)
		}
		evidence := extractEvidence(question, chunk.MatchedQuery, anchor, defaultCitationEvidenceRunes)
		if evidence == "" {
			continue
		}

		filteredContexts = append(filteredContexts, chunk)
		citationID := "C" + strconv.Itoa(len(citations)+1)
		citations = append(citations, Citation{
			CitationID:  citationID,
			EvidenceID:  chunk.EvidenceID,
			ChunkID:     chunk.ChunkID,
			ChunkIndex:  chunk.ChunkIndex,
			Score:       chunk.Score,
			Content:     evidence,
			Source:      chunk.Source,
			VectorRank:  chunk.VectorRank,
			KeywordRank: chunk.KeywordRank,
			RRFScore:    chunk.RRFScore,
			RerankScore: chunk.RerankScore,
			FinalRank:   chunk.FinalRank,
		})
	}
	return filteredContexts, citations
}

func buildCitations(question string, contexts []RetrievedChunk) []Citation {
	_, citations := buildCitationSet(question, contexts)
	return citations
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
	terms := ExtractQueryTerms(strings.TrimSpace(question + " " + matchedQuery))
	if utf8.RuneCountInString(anchor) <= maxRunes {
		return endEvidenceAfterRelevantPhrase(anchor, terms, maxRunes)
	}

	windowCandidates := relevantEvidenceWindows(anchor, terms, maxRunes)
	for _, candidate := range SplitTextIntoChunks(anchor, maxRunes, 0) {
		windowCandidates = append(windowCandidates, candidate.Content)
	}
	if len(windowCandidates) == 0 {
		return ""
	}
	bestIndex := 0
	bestScore := evidenceTermScore(windowCandidates[0], terms)
	for i := 1; i < len(windowCandidates); i++ {
		score := evidenceTermScore(windowCandidates[i], terms)
		if score > bestScore {
			bestIndex = i
			bestScore = score
		}
	}
	return endEvidenceAfterRelevantPhrase(windowCandidates[bestIndex], terms, maxRunes)
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
