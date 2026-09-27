package service

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"vid-lens/internal/ai"
	"vid-lens/internal/model"
)

func TestArtifactGenerationUsesStreamAndPersistsActualUsage(t *testing.T) {
	svc, _, calls := artifactFixture(t, func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var request struct {
			Stream  bool `json:"stream"`
			Options struct {
				IncludeUsage bool `json:"include_usage"`
			} `json:"stream_options"`
		}
		if json.Unmarshal(raw, &request) != nil || !request.Stream || !request.Options.IncludeUsage {
			w.WriteHeader(http.StatusGatewayTimeout)
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(raw))
		artifactModelResponse(w, r)
	})
	ctx := context.Background()
	run, err := svc.Submit(ctx, 7, "stream", artifactRequest(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = svc.ExecuteArtifact(ctx, run.ID); err != nil {
		t.Fatal(err)
	}
	result, err := svc.Run(ctx, 7, run.ID)
	if err != nil || result.Status != "completed" || result.Usage.TokenSource != "actual" || result.Usage.CompletionTokens != 50 || calls.Load() != 1 {
		t.Fatalf("result=%+v calls=%d err=%v", result, calls.Load(), err)
	}
}

func TestArtifactIncompleteStreamDoesNotPublishVersion(t *testing.T) {
	for _, mode := range []string{"disconnect", "truncated", "oversized"} {
		t.Run(mode, func(t *testing.T) {
			svc, db, _ := artifactFixture(t, func(w http.ResponseWriter, r *http.Request) {
				if mode == "truncated" {
					artifactStreamResponse(w, `{}`, "length")
					return
				}
				w.Header().Set("Content-Type", "text/event-stream")
				content := `{"schema_version":1`
				if mode == "oversized" {
					content = strings.Repeat("x", 512*1024+1)
				}
				data, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"content": content}}}})
				_, _ = w.Write(append(append([]byte("data: "), data...), []byte("\n\n")...))
			})
			ctx := context.Background()
			run, err := svc.Submit(ctx, 7, mode, artifactRequest(), nil)
			if err != nil {
				t.Fatal(err)
			}
			if err = svc.ExecuteArtifact(ctx, run.ID); err != nil {
				t.Fatal(err)
			}
			result, err := svc.Run(ctx, 7, run.ID)
			if err != nil || result.Status != "failed" || result.Result != nil {
				t.Fatalf("result=%+v err=%v", result, err)
			}
			var versions int64
			if err = db.Model(&model.ArtifactVersion{}).Where("artifact_id = ?", run.ArtifactID).Count(&versions).Error; err != nil || versions != 0 {
				t.Fatalf("published versions=%d err=%v", versions, err)
			}
		})
	}
}

type cancelledStudyStream struct{}

func (cancelledStudyStream) Chat(context.Context, []ai.ChatMessage) (string, error) {
	panic("unexpected non-streaming call")
}
func (cancelledStudyStream) StreamChat(ctx context.Context, _ []ai.ChatMessage, emit func(string) error) error {
	if err := emit(`{"partial":`); err != nil {
		return err
	}
	<-ctx.Done()
	return ctx.Err()
}
func TestCollectStudyResponseHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := collectStudyResponse(ctx, cancelledStudyStream{}, nil)
	if err != context.Canceled {
		t.Fatalf("err=%v", err)
	}
}
