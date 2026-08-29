package ai

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestOpenAIAudioTranscriptionClientPostsMultipartAudio(t *testing.T) {
	var gotAuth, gotModel, gotFilename, gotFile string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/audio/transcriptions" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		gotAuth = r.Header.Get("Authorization")
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Fatalf("ParseMultipartForm() error = %v", err)
		}
		gotModel = r.FormValue("model")
		file, header, err := r.FormFile("file")
		if err != nil {
			t.Fatalf("FormFile() error = %v", err)
		}
		defer file.Close()
		gotFilename = header.Filename
		buf := make([]byte, 64)
		n, err := file.Read(buf)
		if err != nil {
			t.Fatalf("read multipart file: %v", err)
		}
		gotFile = string(buf[:n])
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"text":"转写结果"}`))
	}))
	defer server.Close()

	audioPath := filepath.Join(t.TempDir(), "chunk-a.mp3")
	if err := os.WriteFile(audioPath, []byte("audio-bytes"), 0o600); err != nil {
		t.Fatal(err)
	}

	client := NewOpenAIAudioTranscriptionClient(server.URL+"/v1", "sk-asr", "asr-model")
	text, err := client.Transcribe(context.Background(), audioPath)
	if err != nil {
		t.Fatalf("Transcribe() error = %v", err)
	}
	if text != "转写结果" {
		t.Fatalf("text = %q", text)
	}
	if gotAuth != "Bearer sk-asr" || gotModel != "asr-model" || gotFilename != "chunk-a.mp3" || gotFile != "audio-bytes" {
		t.Fatalf("request = auth:%q model:%q filename:%q file:%q", gotAuth, gotModel, gotFilename, gotFile)
	}
}

func TestFactoryOpenAICompatibleASRUsesAudioTranscriptions(t *testing.T) {
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = w.Write([]byte(`{"text":"ok"}`))
	}))
	defer server.Close()

	strategy, err := NewFactory().NewASRStrategy(Profile{
		ASRProvider: "openai_compatible",
		ASRBaseURL:  server.URL + "/v1",
		ASRAPIKey:   "sk-asr",
		ASRModel:    "asr-model",
	})
	if err != nil {
		t.Fatalf("NewASRStrategy() error = %v", err)
	}
	audioPath := filepath.Join(t.TempDir(), "audio.mp3")
	if err := os.WriteFile(audioPath, []byte("audio"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := strategy.Transcribe(context.Background(), audioPath); err != nil {
		t.Fatalf("Transcribe() error = %v", err)
	}
	if gotPath != "/v1/audio/transcriptions" {
		t.Fatalf("path = %q, want /v1/audio/transcriptions", gotPath)
	}
}
