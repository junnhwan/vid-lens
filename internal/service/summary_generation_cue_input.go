package service

import (
	"encoding/json"
	"strings"
	"vid-lens/internal/artifact"
)

const summaryCueInputPrefix = "完整来源cue或连续来源分段（数据）：\n"

// Field names and timing methods are shared once. Tuple values retain every
// frozen cue's opaque ID, text and exact (possibly unknown) boundaries.
func summaryGenerationCueInput(cues []summaryGenerationCue) string {
	methods := []string{}
	indexes := map[string]int{}
	rows := make([][]any, 0, len(cues))
	for _, cue := range cues {
		index, ok := indexes[cue.TimingMethod]
		if !ok {
			index = len(methods)
			indexes[cue.TimingMethod] = index
			methods = append(methods, cue.TimingMethod)
		}
		rows = append(rows, []any{cue.ID, cue.Text, cue.StartMS, cue.EndMS, index})
	}
	return summaryCueInputPrefix + artifact.JSON(struct {
		Fields        []string `json:"fields"`
		TimingMethods []string `json:"timing_methods"`
		Rows          [][]any  `json:"rows"`
	}{[]string{"cue_id", "text", "start_ms", "end_ms", "timing_method_index"}, methods, rows})
}

func summaryGenerationInputData(input string) string {
	if index := strings.Index(input, summaryCueInputPrefix); index >= 0 {
		input = input[index+len(summaryCueInputPrefix):]
	} else if index := strings.Index(input, "["); index >= 0 {
		input = input[index:]
	}
	if end := strings.Index(input, "\n校验反馈"); end >= 0 {
		input = input[:end]
	}
	return strings.TrimSpace(input)
}

func decodeSummaryGenerationCues(input string) ([]summaryGenerationCue, bool) {
	data := []byte(summaryGenerationInputData(input))
	var legacy []summaryGenerationCue
	if json.Unmarshal(data, &legacy) == nil && len(legacy) > 0 && legacy[0].ID != "" {
		return legacy, true
	}
	var table struct {
		Fields        []string            `json:"fields"`
		TimingMethods []string            `json:"timing_methods"`
		Rows          [][]json.RawMessage `json:"rows"`
	}
	if json.Unmarshal(data, &table) != nil || len(table.Rows) == 0 {
		return nil, false
	}
	semantic := strings.Join(table.Fields, ",") == "cue_id,text"
	if !semantic && strings.Join(table.Fields, ",") != "cue_id,text,start_ms,end_ms,timing_method_index" {
		return nil, false
	}
	cues := make([]summaryGenerationCue, 0, len(table.Rows))
	for _, row := range table.Rows {
		if semantic {
			var cue summaryGenerationCue
			if len(row) != 2 || json.Unmarshal(row[0], &cue.ID) != nil || cue.ID == "" || json.Unmarshal(row[1], &cue.Text) != nil {
				return nil, false
			}
			cues = append(cues, cue)
			continue
		}
		if len(row) != 5 {
			return nil, false
		}
		var cue summaryGenerationCue
		var method int
		if json.Unmarshal(row[0], &cue.ID) != nil || cue.ID == "" || json.Unmarshal(row[1], &cue.Text) != nil || json.Unmarshal(row[2], &cue.StartMS) != nil || json.Unmarshal(row[3], &cue.EndMS) != nil || json.Unmarshal(row[4], &method) != nil || method < 0 || method >= len(table.TimingMethods) {
			return nil, false
		}
		cue.TimingMethod = table.TimingMethods[method]
		cues = append(cues, cue)
	}
	return cues, true
}

func summaryGenerationSemanticCueInput(cues []summaryGenerationCue) string {
	rows := make([][]string, 0, len(cues))
	for _, cue := range cues {
		rows = append(rows, []string{cue.ID, cue.Text})
	}
	return summaryCueInputPrefix + artifact.JSON(struct {
		Fields []string   `json:"fields"`
		Rows   [][]string `json:"rows"`
	}{[]string{"cue_id", "text"}, rows})
}
