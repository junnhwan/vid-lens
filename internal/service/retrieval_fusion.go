package service

import (
	"fmt"
	"sort"
	"strings"
	"unicode"
)

const (
	RetrievalSourceVector  = "vector"
	RetrievalSourceKeyword = "keyword"
	RetrievalSourceHybrid  = "hybrid"
	defaultRRFK            = 60.0
)

func FuseRetrievedChunks(vectorChunks, keywordChunks []RetrievedChunk, topK int, k float64) []RetrievedChunk {
	if k <= 0 {
		k = defaultRRFK
	}

	type fusedState struct {
		chunk RetrievedChunk
	}
	states := make(map[string]*fusedState, len(vectorChunks)+len(keywordChunks))
	order := make([]string, 0, len(vectorChunks)+len(keywordChunks))
	getState := func(chunk RetrievedChunk) *fusedState {
		key := retrievalChunkKey(chunk)
		state := states[key]
		if state == nil {
			state = &fusedState{chunk: chunk}
			states[key] = state
			order = append(order, key)
		}
		return state
	}

	for i, chunk := range vectorChunks {
		state := getState(chunk)
		if state.chunk.Content == "" {
			state.chunk.Content = chunk.Content
		}
		state.chunk.VectorRank = i + 1
		state.chunk.Source = sourceForRanks(state.chunk.VectorRank, state.chunk.KeywordRank)
	}
	for i, chunk := range keywordChunks {
		state := getState(chunk)
		if state.chunk.Content == "" {
			state.chunk.Content = chunk.Content
		}
		state.chunk.KeywordRank = i + 1
		state.chunk.Source = sourceForRanks(state.chunk.VectorRank, state.chunk.KeywordRank)
	}

	fused := make([]RetrievedChunk, 0, len(states))
	for _, key := range order {
		chunk := states[key].chunk
		score := 0.0
		if chunk.VectorRank > 0 {
			score += 1.0 / (k + float64(chunk.VectorRank))
		}
		if chunk.KeywordRank > 0 {
			score += 1.0 / (k + float64(chunk.KeywordRank))
		}
		chunk.RRFScore = score
		chunk.Score = float32(score)
		if chunk.Source == "" {
			chunk.Source = sourceForRanks(chunk.VectorRank, chunk.KeywordRank)
		}
		fused = append(fused, chunk)
	}

	sort.SliceStable(fused, func(i, j int) bool {
		if fused[i].RRFScore != fused[j].RRFScore {
			return fused[i].RRFScore > fused[j].RRFScore
		}
		if fused[i].VectorRank != fused[j].VectorRank {
			if fused[i].VectorRank == 0 {
				return false
			}
			if fused[j].VectorRank == 0 {
				return true
			}
			return fused[i].VectorRank < fused[j].VectorRank
		}
		if fused[i].KeywordRank != fused[j].KeywordRank {
			if fused[i].KeywordRank == 0 {
				return false
			}
			if fused[j].KeywordRank == 0 {
				return true
			}
			return fused[i].KeywordRank < fused[j].KeywordRank
		}
		return fused[i].ChunkIndex < fused[j].ChunkIndex
	})
	if topK > 0 && len(fused) > topK {
		fused = fused[:topK]
	}
	return fused
}

func sourceForRanks(vectorRank, keywordRank int) string {
	switch {
	case vectorRank > 0 && keywordRank > 0:
		return RetrievalSourceHybrid
	case vectorRank > 0:
		return RetrievalSourceVector
	case keywordRank > 0:
		return RetrievalSourceKeyword
	default:
		return ""
	}
}

func ExtractQueryTerms(query string) []string {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return nil
	}
	seen := make(map[string]bool)
	terms := make([]string, 0)
	add := func(term string) {
		term = strings.TrimSpace(term)
		if term == "" || seen[term] {
			return
		}
		seen[term] = true
		terms = append(terms, term)
	}

	var ascii []rune
	var cjk []rune
	flushASCII := func() {
		if len(ascii) >= 2 {
			add(string(ascii))
		}
		ascii = ascii[:0]
	}
	flushCJK := func() {
		addCJKTerms(cjk, add)
		cjk = cjk[:0]
	}

	for _, r := range []rune(query) {
		switch {
		case isCJK(r):
			flushASCII()
			cjk = append(cjk, r)
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			flushCJK()
			ascii = append(ascii, r)
		default:
			flushASCII()
			flushCJK()
		}
	}
	flushASCII()
	flushCJK()
	return terms
}

func addCJKTerms(runes []rune, add func(string)) {
	if len(runes) == 0 {
		return
	}
	if len(runes) <= 4 {
		add(string(runes))
	}
	for n := 2; n <= 4; n++ {
		if len(runes) < n {
			continue
		}
		for i := 0; i+n <= len(runes); i++ {
			add(string(runes[i : i+n]))
		}
	}
}

func isCJK(r rune) bool {
	return unicode.Is(unicode.Han, r)
}

func describeRetrievedChunk(chunk RetrievedChunk) string {
	source := chunk.Source
	if source == "" {
		source = sourceForRanks(chunk.VectorRank, chunk.KeywordRank)
	}
	if source == "" {
		source = "unknown"
	}
	return fmt.Sprintf("[Chunk %d source=%s score=%.3f rrf=%.4f vector_rank=%d keyword_rank=%d]",
		chunk.ChunkIndex, source, chunk.Score, chunk.RRFScore, chunk.VectorRank, chunk.KeywordRank)
}
