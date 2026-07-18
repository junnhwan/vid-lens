package main

import (
	"fmt"
	"io"
	"strings"
)

type evalProgress struct {
	enabled bool
	out     io.Writer
}

func newEvalProgress(enabled bool, out io.Writer) evalProgress {
	if out == nil {
		out = io.Discard
	}
	return evalProgress{enabled: enabled, out: out}
}

func (p evalProgress) stage(format string, args ...any) {
	if !p.enabled {
		return
	}
	fmt.Fprintf(p.out, "[rag-eval] %s\n", fmt.Sprintf(format, args...))
}

func (p evalProgress) caseStep(stage string, idx, total int, c evalCase) {
	if !p.enabled {
		return
	}
	fmt.Fprintf(p.out, "[rag-eval] %s case %d/%d task=%d question=%q\n", stage, idx, total, c.TaskID, truncateProgressQuestion(c.Question))
}

func truncateProgressQuestion(question string) string {
	question = strings.TrimSpace(question)
	runes := []rune(question)
	if len(runes) <= 80 {
		return question
	}
	return string(runes[:80]) + "..."
}
