package service

import (
	"encoding/json"
	"strings"
)

const researchNoEvidenceLimit = 3

func researchEvidenceTool(tool string) bool {
	switch tool {
	case VideoAgentToolSearchTranscript, VideoAgentToolSearchVisualEvidence,
		VideoAgentToolGetTranscriptWindow, VideoAgentToolInspectVisualWindow, VideoAgentToolInvestigateVisual:
		return true
	}
	return false
}

func (r *VideoAgentLoopRunner) researchStalled(state VideoAgentLoopState) bool {
	if r.policy.ConvergenceVersion == 0 {
		return false
	}
	// Derive this from observations so interrupted runs recover the same
	// decision without mutable counters or a second persistence contract.
	var evidence []RetrievedChunk
	streak := 0
	for _, observation := range state.Observations {
		if observation.ErrorClass != "" || !researchEvidenceTool(observation.Tool) {
			continue
		}
		merged := mergeResearchProgressEvidence(evidence, observation.NewEvidence, r.policy.ConvergenceVersion)
		if researchEvidenceSize(merged) > researchEvidenceSize(evidence) {
			streak = 0
		} else {
			streak++
		}
		evidence = merged
	}
	return streak >= researchNoEvidenceLimit
}

func researchEvidenceSize(evidence []RetrievedChunk) int {
	size := len(evidence)
	for _, chunk := range evidence {
		size += len(chunk.Content)
	}
	return size
}

func mergeResearchProgressEvidence(existing, added []RetrievedChunk, version int) []RetrievedChunk {
	merged := mergeVideoAgentLoopEvidence(existing, added)
	if version == 0 {
		return merged
	}
	indices := make(map[string]int, len(merged))
	for i, chunk := range merged {
		indices[videoAgentLoopEvidenceKey(chunk)] = i
	}
	for _, chunk := range added {
		i := indices[videoAgentLoopEvidenceKey(chunk)]
		previous := merged[i]
		// Keep the observed identity and provenance. A longer copy of the
		// same source is useful progress; a changed ranking is not.
		if chunk.TaskID == previous.TaskID && chunk.ChunkID == previous.ChunkID &&
			len(chunk.Content) > len(previous.Content) && strings.Contains(chunk.Content, previous.Content) {
			merged[i].Content = chunk.Content
		}
	}
	return merged
}

// Preserve only allow-listed, bounded search/window arguments in the planner
// view. Full tool results and citation bodies stay in the durable checkpoint.
func compactResearchArguments(tool string, arguments json.RawMessage) json.RawMessage {
	var compact map[string]json.RawMessage
	if json.Unmarshal(researchActionArguments(tool, arguments), &compact) != nil {
		return nil
	}
	for _, key := range []string{"question", "query", "goal"} {
		var value string
		if json.Unmarshal(compact[key], &value) == nil {
			compact[key], _ = json.Marshal(trimRunes(value, 160))
		}
	}
	encoded, _ := json.Marshal(compact)
	return encoded
}

// Duplicate detection uses the complete query. The short planner display must
// not make distinct questions with the same prefix look like the same action.
func researchActionArguments(tool string, arguments json.RawMessage) json.RawMessage {
	if !researchEvidenceTool(tool) {
		return nil
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(arguments, &fields) != nil {
		return nil
	}
	compact := make(map[string]json.RawMessage)
	for _, key := range []string{"task_id", "chunk_index", "radius", "start_ms", "end_ms", "max_frames", "top_k"} {
		if value, ok := fields[key]; ok {
			var number int64
			if json.Unmarshal(value, &number) == nil {
				compact[key] = value
			}
		}
	}
	for _, key := range []string{"question", "query", "goal"} {
		var value string
		if json.Unmarshal(fields[key], &value) == nil && value != "" {
			compact[key], _ = json.Marshal(value)
		}
	}
	encoded, _ := json.Marshal(compact)
	return encoded
}

func repeatedResearchAction(state VideoAgentLoopState, decision VideoAgentLoopDecision) bool {
	if !researchEvidenceTool(decision.Tool) {
		return false
	}
	arguments := researchActionArguments(decision.Tool, decision.Arguments)
	if len(arguments) == 0 || string(arguments) == "{}" || decision.Tool == VideoAgentToolInvestigateVisual {
		return false
	}
	for _, step := range state.Steps {
		if step.Status != VideoAgentLoopStepCompleted || step.Action.Tool != decision.Tool || step.Observation == nil || step.Observation.ErrorClass != "" {
			continue
		}
		previous := researchActionArguments(step.Action.Tool, step.Action.Arguments)
		if string(previous) == string(arguments) {
			return true
		}
		if decision.Tool == VideoAgentToolGetTranscriptWindow && researchWindowCovered(previous, arguments) {
			return true
		}
	}
	return false
}

func researchWindowCovered(previous, next json.RawMessage) bool {
	var before, after struct {
		TaskID     int64 `json:"task_id"`
		ChunkIndex int   `json:"chunk_index"`
		Radius     int   `json:"radius"`
	}
	if json.Unmarshal(previous, &before) != nil || json.Unmarshal(next, &after) != nil || before.TaskID != after.TaskID {
		return false
	}
	before.Radius = min(3, max(0, before.Radius))
	after.Radius = min(3, max(0, after.Radius))
	return max(0, after.ChunkIndex-after.Radius) >= max(0, before.ChunkIndex-before.Radius) &&
		after.ChunkIndex+after.Radius <= before.ChunkIndex+before.Radius
}

func researchAnswerStopReason(state VideoAgentLoopState) string {
	if len(state.Steps) > 0 && state.Steps[len(state.Steps)-1].Action.StopReason == "evidence_stalled" {
		return "evidence_stalled"
	}
	return "answer_generated"
}
