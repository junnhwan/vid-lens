package mq

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"vid-lens/internal/model"

	amqp "github.com/rabbitmq/amqp091-go"
)

var errHandlerFailure = errors.New("handler failure")

// consumeMessagesN 的并发分发是 per-queue prefetch 生效的前提：prefetch 只
// 缓冲未 ack 的投递，worker 池才真正并行处理。

func TestConsumeMessagesNRunsHandlersConcurrently(t *testing.T) {
	reader := &scriptedAmqpReader{}
	reader.fetches = []amqp.Delivery{
		reader.stubDelivery(1, []byte("a")),
		reader.stubDelivery(2, []byte("b")),
		reader.stubDelivery(3, []byte("c")),
	}
	started := make(chan struct{}, 3)
	release := make(chan struct{})
	var mu sync.Mutex
	running, peak := 0, 0
	handler := func(context.Context, amqp.Delivery) error {
		mu.Lock()
		running++
		if running > peak {
			peak = running
		}
		mu.Unlock()
		started <- struct{}{}
		<-release
		mu.Lock()
		running--
		mu.Unlock()
		return nil
	}

	done := make(chan error, 1)
	go func() { done <- consumeMessagesN(context.Background(), reader, handler, 3) }()
	for i := 0; i < 3; i++ {
		select {
		case <-started:
		case <-time.After(2 * time.Second):
			t.Fatalf("only %d of 3 handlers started concurrently", i)
		}
	}
	close(release)
	if err := <-done; err == nil {
		t.Fatal("scripted reader closing should surface the channel-closed error")
	}
	mu.Lock()
	defer mu.Unlock()
	if peak != 3 {
		t.Fatalf("peak handler concurrency = %d, want 3", peak)
	}
	if _, acks, nacks, _ := reader.snapshot(); len(acks) != 3 || len(nacks) != 0 {
		t.Fatalf("acks=%v nacks=%v, want all 3 acked", acks, nacks)
	}
}

func TestConsumeMessagesNNacksFailedHandlersAndStopsDispatch(t *testing.T) {
	reader := &scriptedAmqpReader{}
	reader.fetches = []amqp.Delivery{
		reader.stubDelivery(1, []byte("a")),
		reader.stubDelivery(2, []byte("b")),
	}
	done := make(chan error, 1)
	go func() {
		done <- consumeMessagesN(context.Background(), reader, func(context.Context, amqp.Delivery) error {
			return errHandlerFailure
		}, 2)
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected loop to end after handler failures")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("consumeMessagesN did not terminate")
	}
	if _, acks, nacks, closed := reader.snapshot(); len(nacks) != 2 || len(acks) != 0 || !closed {
		t.Fatalf("acks=%v nacks=%v closed=%v, want both nacked and reader closed", acks, nacks, closed)
	}
}

func TestPrefetchForQueueOverridesSharedPrefetch(t *testing.T) {
	consumer := NewConsumer(nil, nil, nil, nil, "")
	consumer.SetMQConfig([]string{"127.0.0.1:5672"}, 1)
	consumer.SetQueuePrefetch("video-transcribe", 3)
	if got := consumer.prefetchForQueue("video-transcribe"); got != 3 {
		t.Fatalf("transcribe prefetch = %d, want 3", got)
	}
	if got := consumer.prefetchForQueue("video-download"); got != 1 {
		t.Fatalf("download prefetch = %d, want shared 1", got)
	}
}

func TestVisualBranchConcurrencyCap(t *testing.T) {
	consumer := &Consumer{}
	consumer.SetVisualConcurrency(1)
	enter := make(chan struct{}, 2)
	release := make(chan struct{})
	consumer.visualIndex = func(context.Context, *model.VideoTask) (int, error) {
		enter <- struct{}{}
		<-release
		return 1, nil
	}
	task := &model.VideoTask{ID: 9}

	wait1 := consumer.startVisualIndexBranch(context.Background(), task)
	<-enter
	wait2 := consumer.startVisualIndexBranch(context.Background(), task)
	select {
	case <-enter:
		t.Fatal("second visual branch should wait behind the concurrency cap")
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	if out := wait1(); out.err != nil || out.count != 1 {
		t.Fatalf("first branch outcome = %+v", out)
	}
	if out := wait2(); out.err != nil || out.count != 1 {
		t.Fatalf("second branch outcome = %+v, want it to run after the slot freed", out)
	}
}
