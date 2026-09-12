package ai

import (
	"context"
	"fmt"
	"strings"
)

// StreamDelta separates provider-supplied reasoning from the answer. The
// context observer survives admission/retry/metrics wrappers without bypassing
// their accounting or adding an extra provider call.
type StreamDelta struct {
	Kind string `json:"kind"`
	Text string `json:"text,omitempty"`
}
type streamObserverKey struct{}
type streamObserver func(StreamDelta) error

func StreamResponse(ctx context.Context, client ChatClient, messages []ChatMessage, emit func(StreamDelta) error) error {
	if emit == nil {
		return fmt.Errorf("stream emit 不能为空")
	}
	ctx = context.WithValue(ctx, streamObserverKey{}, streamObserver(emit))
	if streaming, ok := client.(StreamingChatClient); ok {
		return streaming.StreamChat(ctx, messages, func(text string) error { return emit(StreamDelta{Kind: "answer", Text: text}) })
	}
	answer, err := client.Chat(ctx, messages)
	if err != nil {
		return err
	}
	return emit(StreamDelta{Kind: "answer", Text: answer})
}

func emitProviderReasoning(ctx context.Context, text string) error {
	if text == "" {
		return nil
	}
	if emit, ok := ctx.Value(streamObserverKey{}).(streamObserver); ok {
		return emit(StreamDelta{Kind: "reasoning", Text: text})
	}
	return nil
}

// Hold only a possible tag suffix, never the complete answer. This also keeps
// split <think> tags out of the answer when a compatible endpoint embeds them.
type thinkingSplitter struct {
	pending  string
	thinking bool
}

func (s *thinkingSplitter) push(text string, final bool, emit func(bool, string) error) error {
	s.pending += text
	for s.pending != "" {
		tag := "<think>"
		if s.thinking {
			tag = "</think>"
		}
		if i := strings.Index(s.pending, tag); i >= 0 {
			if i > 0 {
				if err := emit(s.thinking, s.pending[:i]); err != nil {
					return err
				}
			}
			s.pending = s.pending[i+len(tag):]
			s.thinking = !s.thinking
			continue
		}
		keep := 0
		if !final {
			for n := 1; n < len(tag) && n <= len(s.pending); n++ {
				if strings.HasSuffix(s.pending, tag[:n]) {
					keep = n
				}
			}
		}
		n := len(s.pending) - keep
		if n == 0 {
			return nil
		}
		if err := emit(s.thinking, s.pending[:n]); err != nil {
			return err
		}
		s.pending = s.pending[n:]
		if keep > 0 {
			return nil
		}
	}
	return nil
}
