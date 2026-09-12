package service

import "errors"

// InvalidToolArguments means validation failed before invoking the tool body.
// Only this class is safe to expose as a correctable observation.
type InvalidToolArguments struct{ Cause error }

func (e *InvalidToolArguments) Error() string { return e.Cause.Error() }
func (e *InvalidToolArguments) Unwrap() error { return e.Cause }

func recoverableToolObservation(result VideoAgentToolResult, err error) (VideoAgentLoopObservation, bool) {
	var invalid *InvalidToolArguments
	if !errors.As(err, &invalid) {
		return VideoAgentLoopObservation{}, false
	}
	return VideoAgentLoopObservation{Tool: result.Step.Tool, Step: result.Step, ErrorClass: "invalid_arguments", UnresolvedQuestions: []string{"工具尚未调用；根据 input_schema 纠正一次参数：" + err.Error()}}, true
}
