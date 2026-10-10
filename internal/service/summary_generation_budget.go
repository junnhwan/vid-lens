package service

import (
	"encoding/json"
	"unicode/utf8"

	"vid-lens/internal/ai"
	"vid-lens/internal/summarydoc"
)

// Account for the fixed envelope, independent chapter IDs and exact cue refs,
// in addition to compressed text. Dense subtitle tracks therefore receive
// more JSON headroom than equally short sources with only a few cues. This is
// a scheduling estimate, not a claim about provider token usage.
func summaryGenerationOutputDemand(input string) int64 {
	data := summaryGenerationInputData(input)
	chars, refs := 0, 0
	reducing := false
	if len(data) > 0 {
		if cues, ok := decodeSummaryGenerationCues(input); ok {
			refs = len(cues)
			for _, cue := range cues {
				chars += utf8.RuneCountInString(cue.Text)
			}
		} else {
			reducing = true
			var docs []summarydoc.Document
			if json.Unmarshal([]byte(data), &docs) == nil {
				seen := map[string]bool{}
				for _, doc := range docs {
					for _, block := range doc.Blocks {
						chars += utf8.RuneCountInString(block.Title) + utf8.RuneCountInString(block.BodyMarkdown)
						for _, ref := range block.SourceRefs {
							for _, id := range ref.CueIDs {
								seen[id] = true
							}
						}
					}
				}
				refs = len(seen)
			}
		}
	}
	if reducing {
		return max(768, int64(384+refs*48+chars/2))
	}
	return max(768, int64(512+refs*96+chars/2))
}

func (e *summaryGenerationExecution) plannedOutput(input string) int64 {
	return min(e.output, summaryGenerationOutputDemand(input))
}

func (e *summaryGenerationExecution) contextFits(messages []ai.ChatMessage, output int64) bool {
	return output >= 128 && studyPromptTokens(messages)+output+256 <= e.window
}
