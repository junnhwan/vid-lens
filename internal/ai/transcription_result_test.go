package ai

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"vid-lens/internal/model"
)

func transcriptionAudioFile(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "speech.mp3")
	if err := os.WriteFile(path, []byte("audio"), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestDetailedTranscriptionRequestsWhisperSegmentsAndPreservesNativeTiming(t *testing.T) {
	for _, modelName := range []string{"whisper-1", "text-only-model"} {
		t.Run(modelName, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if err := r.ParseMultipartForm(1 << 20); err != nil {
					t.Fatal(err)
				}
				if modelName == "whisper-1" {
					if r.FormValue("response_format") != "verbose_json" || r.FormValue("timestamp_granularities[]") != "segment" {
						t.Fatalf("form=%v", r.MultipartForm.Value)
					}
				} else if r.FormValue("response_format") != "" {
					t.Fatalf("unexpected format=%q", r.FormValue("response_format"))
				}
				_, _ = w.Write([]byte(`{"text":"一句话。","segments":[{"text":"一句话。","start":1.234,"end":3.456}]}`))
			}))
			defer server.Close()
			result, err := NewOpenAIAudioTranscriptionClient(server.URL, "", modelName).TranscribeDetailed(context.Background(), transcriptionAudioFile(t))
			want := []model.TranscriptionSegment{{Text: "一句话。", StartMS: 1234, EndMS: 3456}}
			if err != nil || result.Text != "一句话。" || !reflect.DeepEqual(result.Segments, want) {
				t.Fatalf("result=%+v err=%v", result, err)
			}
		})
	}
}

func TestDetailedTranscriptionFallsBackOnlyForUnsupportedTimingFormat(t *testing.T) {
	for _, tc := range []struct {
		name, message string
		status, calls int
		succeeds      bool
	}{
		{"unsupported", "response_format verbose_json is not supported", 400, 3, true},
		{"invalid_audio", "invalid audio file", 400, 1, false},
		{"invalid_model", "invalid model", 400, 1, false},
		{"auth", "response_format verbose_json is not supported", 401, 1, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if err := r.ParseMultipartForm(1 << 20); err != nil {
					t.Fatal(err)
				}
				if calls == 1 {
					w.WriteHeader(tc.status)
					_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"message": tc.message}})
					return
				}
				if r.FormValue("response_format") != "" {
					t.Fatal("fallback format not removed")
				}
				_, _ = w.Write([]byte(`{"text":"正常文字"}`))
			}))
			defer server.Close()
			client := NewOpenAIAudioTranscriptionClient(server.URL, "", "whisper-1")
			path := transcriptionAudioFile(t)
			result, err := client.TranscribeDetailed(context.Background(), path)
			if tc.succeeds {
				if err != nil || result.Text != "正常文字" || len(result.Segments) != 0 {
					t.Fatalf("result=%+v err=%v", result, err)
				}
				if _, err := client.TranscribeDetailed(context.Background(), path); err != nil {
					t.Fatal(err)
				}
			} else if err == nil {
				t.Fatal("expected provider error")
			}
			if calls != tc.calls {
				t.Fatalf("calls=%d want=%d", calls, tc.calls)
			}
		})
	}
}

func TestDetailedTranscriptionDiscardsMalformedTimingWithoutLosingText(t *testing.T) {
	result, err := parseTranscriptionResult([]byte(`{"text":"完整原文。","segments":[{"text":"负数","start":-1,"end":2},{"text":"缺字段","end":2},{"text":"逆序","start":3,"end":2},{"text":"坏类型","start":"bad","end":2},{"text":"太大","start":0,"end":1e30},{"text":"有效","start":1,"end":2}]}`))
	if err != nil || result.Text != "完整原文。" || len(result.Segments) != 1 || result.Segments[0].Text != "有效" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestDetailedTranscriptionKeepsWordOnlyResponsesCoarse(t *testing.T) {
	result, err := parseTranscriptionResult([]byte(`{"text":"hello world","words":[{"word":"hello","start":0,"end":0.5},{"word":"world","start":0.6,"end":1}]}`))
	if err != nil || result.Text != "hello world" || len(result.Segments) != 0 {
		t.Fatalf("word-only response became one-word citation evidence: result=%+v err=%v", result, err)
	}
}

func TestDetailedTranscriptionAcceptsExplicitSilenceAndRejectsMissingText(t *testing.T) {
	for _, response := range []string{`{"text":""}`, `{"text":"","segments":[]}`} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(response))
		}))
		result, err := NewOpenAIAudioTranscriptionClient(server.URL, "", "asr").TranscribeDetailed(context.Background(), transcriptionAudioFile(t))
		server.Close()
		if err != nil || result.Text != "" || len(result.Segments) != 0 {
			t.Fatalf("silent response=%s result=%+v err=%v", response, result, err)
		}
	}
	for _, response := range []string{`{}`, `{"text":null}`, `{"text":42}`, `{"error":"no transcript"}`, `{"text":"","segments":[{"text":"speech","start":0,"end":1}]}`, `not-json`} {
		if _, err := parseTranscriptionResult([]byte(response)); err == nil {
			t.Fatalf("malformed or incomplete ASR response accepted as silence: %s", response)
		}
	}
}

type timedRetryStrategy struct{ calls int }

func (s *timedRetryStrategy) Transcribe(context.Context, string) (string, error) {
	panic("detailed capability was lost")
}
func (s *timedRetryStrategy) TranscribeChunks(context.Context, []string) (string, error) {
	return "", nil
}
func (s *timedRetryStrategy) Summarize(context.Context, string) (string, error) { return "", nil }
func (s *timedRetryStrategy) TranscribeDetailed(context.Context, string) (TranscriptionResult, error) {
	s.calls++
	if s.calls == 1 {
		return TranscriptionResult{}, &ProviderError{Class: ErrorProvider5xx, StatusCode: 503, Retryable: true}
	}
	return TranscriptionResult{Text: "timed", Segments: []model.TranscriptionSegment{{Text: "timed", StartMS: 1200, EndMS: 2300}}}, nil
}

func TestDetailedTranscriptionSurvivesAdmissionObservationRetryAndComposition(t *testing.T) {
	base := &timedRetryStrategy{}
	admission := &fakeAdmission{}
	recorder := &recordingCallRecorder{}
	var strategy Strategy = &CompositeStrategy{asr: base}
	strategy = AdmitStrategy(strategy, admission, "provider", "model", "")
	strategy = NewObservedStrategy(strategy, recorder, CallContext{ASRModel: "model"})
	strategy = RetryStrategy(strategy, ProviderRetryPolicy{MaxRetries: 1, Sleep: func(context.Context, time.Duration) error { return nil }})
	result, err := TranscribeDetailed(context.Background(), strategy, "audio.mp3")
	if err != nil || result.Text != "timed" || len(result.Segments) != 1 || result.Segments[0].StartMS != 1200 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if base.calls != 2 || admission.n != 2 || len(recorder.records) != 2 {
		t.Fatalf("calls=%d admissions=%d records=%d", base.calls, admission.n, len(recorder.records))
	}
	legacy, err := TranscribeDetailed(context.Background(), fakeStrategy{}, "audio.mp3")
	if err != nil || legacy.Text != "ok" || len(legacy.Segments) != 0 {
		t.Fatalf("legacy=%+v err=%v", legacy, err)
	}
	denied := AdmitStrategy(base, &fakeAdmission{denyAt: 1}, "p", "m", "")
	if _, err := TranscribeDetailed(context.Background(), denied, "audio.mp3"); !errors.Is(err, ErrAdmissionRejected) || base.calls != 2 {
		t.Fatalf("denied=%v calls=%d", err, base.calls)
	}
}
