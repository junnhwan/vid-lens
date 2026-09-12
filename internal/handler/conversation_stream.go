package handler

import (
	"context"
	"fmt"
	"time"

	"github.com/gin-gonic/gin"
	"vid-lens/internal/service"
)

// The handler goroutine is the only response writer. The acknowledgement
// provides backpressure and propagates disconnects into tools and providers.
func (h *ChatHandler) streamConversation(c *gin.Context, request service.ConversationRequest, emit service.ConversationStreamSink) (service.ConversationResult, error) {
	ctx, cancel := context.WithCancel(c.Request.Context())
	defer cancel()
	type packet struct {
		event service.ConversationStreamEvent
		ack   chan error
	}
	type outcome struct {
		result service.ConversationResult
		err    error
	}
	events := make(chan packet)
	done := make(chan outcome, 1)
	go func() {
		result, err := h.execution.Stream(ctx, request, func(event service.ConversationStreamEvent) error {
			p := packet{event: event, ack: make(chan error, 1)}
			select {
			case events <- p:
			case <-ctx.Done():
				return ctx.Err()
			}
			select {
			case err := <-p.ack:
				return err
			case <-ctx.Done():
				return ctx.Err()
			}
		})
		done <- outcome{result, err}
	}()
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	started := false
	for {
		select {
		case p := <-events:
			err := emit(p.event)
			p.ack <- err
			started = true
			if err != nil {
				return service.ConversationResult{}, err
			}
		case result := <-done:
			return result.result, result.err
		case <-ticker.C:
			if started {
				if _, err := fmt.Fprint(c.Writer, ": heartbeat\n\n"); err != nil {
					return service.ConversationResult{}, err
				}
				c.Writer.Flush()
			}
		case <-ctx.Done():
			return service.ConversationResult{}, ctx.Err()
		}
	}
}
