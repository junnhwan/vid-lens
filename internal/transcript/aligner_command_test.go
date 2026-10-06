package transcript

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"
)

func init() {
	if os.Getenv("VIDLENS_ALIGN_COMMAND_FIXTURE") == "1" {
		time.Sleep(10 * time.Second)
		os.Exit(0)
	}
}
func TestCommandAlignerTimeoutAndQueuedCancellationReleaseSlot(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("VIDLENS_ALIGN_COMMAND_FIXTURE", "1")
	aligner := &CommandAligner{Command: []string{exe}, Timeout: 100 * time.Millisecond}
	done := make(chan error, 1)
	aligner.once.Do(func() { aligner.slots = make(chan struct{}, 1) })
	go func() { _, err := aligner.Align(context.Background(), "audio", nil); done <- err }()
	deadline := time.Now().Add(time.Second)
	for len(aligner.slots) == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	queued, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := aligner.Align(queued, "audio", nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("queued cancellation: %v", err)
	}
	if err := <-done; !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("active timeout: %v", err)
	}
	if len(aligner.slots) != 0 {
		t.Fatal("slot leaked")
	}
	// A later operation can acquire the same slot and remains timeout bounded.
	if _, err := aligner.Align(context.Background(), "audio", nil); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
}
