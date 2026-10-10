package service

import (
	"encoding/json"
	"unicode/utf8"

	"vid-lens/internal/ai"
)

// Estimate space for semantic content, hierarchy and a bounded selection of
// cue IDs. Identity and timing are assembled by the server rather than repeated
// by the model. Actual provider usage remains the authoritative budget record.
func summaryGenerationOutputDemand(input string) int64 {
	data := summaryGenerationInputData(input)
	chars, refs := 0, 0
	if len(data) > 0 {
		if cues, ok := decodeSummaryGenerationCues(input); ok {
			refs = len(cues)
			for _, cue := range cues {
				chars += utf8.RuneCountInString(cue.Text)
			}
		} else {
			var docs []summaryGenerationWireDocument
			if json.Unmarshal([]byte(data), &docs) == nil {
				seen := map[string]bool{}
				for _, doc := range docs {
					for _, block := range doc.Blocks {
						chars += utf8.RuneCountInString(block.Title) + utf8.RuneCountInString(block.BodyMarkdown)
						for _, id := range block.CueIDs {
							seen[id] = true
						}
					}
				}
				refs = len(seen)
			}
		}
	}
	return max(768, int64(512+min(refs, 96)*16+chars/2))
}

func (e *summaryGenerationExecution) plannedOutput(input string) int64 {
	return min(e.output, summaryGenerationOutputDemand(input))
}

func (e *summaryGenerationExecution) contextFits(messages []ai.ChatMessage, output int64) bool {
	return output >= 128 && studyPromptTokens(messages)+output+256 <= e.window
}
