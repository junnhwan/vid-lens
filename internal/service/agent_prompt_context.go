package service

// plannerInputView keeps evidence once, with small text excerpts. Durable state
// remains complete for citation canonicalization and recovery.
func plannerInputView(state VideoAgentLoopState) VideoAgentLoopState {
	view := state
	view.Evidence = nil
	for _, chunk := range balancedEvidence(state.Evidence, 12) {
		if len(view.Evidence) >= 12 {
			break
		}
		chunk.Content = trimRunes(chunk.Content, 600)
		chunk.AnchorContent = ""
		view.Evidence = append(view.Evidence, chunk)
	}
	view.Observations = nil
	view.Steps = nil
	start := len(state.Steps) - 6
	if start < 0 {
		start = 0
	}
	for _, step := range state.Steps[start:] {
		step.Action.Arguments = nil
		step.Action.Reason = ""
		step.Action.PublicSummary = trimRunes(step.Action.PublicSummary, 120)
		step.Trace.Input = nil
		step.Observation = nil
		step.Error = trimRunes(step.Error, 200)
		view.Steps = append(view.Steps, step)
	}
	return view
}
