package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMimoTranscribeSendsAudioChatCompletion(t *testing.T) {
	var captured map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if got := r.Header.Get("api-key"); got != "tp-test-key" {
			t.Fatalf("unexpected api-key header: %q", got)
		}
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"转录结果"}}]}`))
	}))
	defer server.Close()

	audioPath := filepath.Join(t.TempDir(), "audio.mp3")
	if err := os.WriteFile(audioPath, []byte("fake mp3"), 0644); err != nil {
		t.Fatalf("write audio: %v", err)
	}

	strategy := NewMimoStrategy("tp-test-key", server.URL, "mimo-v2.5-asr", "mimo-v2.5")
	text, err := strategy.Transcribe(context.Background(), audioPath)
	if err != nil {
		t.Fatalf("transcribe: %v", err)
	}
	if text != "转录结果" {
		t.Fatalf("unexpected transcript: %q", text)
	}
	if captured["model"] != "mimo-v2.5-asr" {
		t.Fatalf("unexpected model: %#v", captured["model"])
	}
	encoded := mustFindInputAudioData(t, captured)
	if !strings.HasPrefix(encoded, "data:audio/mpeg;base64,") {
		t.Fatalf("expected mp3 data URL, got %q", encoded)
	}
}

func TestMimoSummarizeSendsTextChatCompletion(t *testing.T) {
	var captured map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"总结结果"}}]}`))
	}))
	defer server.Close()

	strategy := NewMimoStrategy("tp-test-key", server.URL, "mimo-v2.5-asr", "mimo-v2.5")
	summary, err := strategy.Summarize(context.Background(), "一段转录文本")
	if err != nil {
		t.Fatalf("summarize: %v", err)
	}
	if summary != "总结结果" {
		t.Fatalf("unexpected summary: %q", summary)
	}
	if captured["model"] != "mimo-v2.5" {
		t.Fatalf("unexpected model: %#v", captured["model"])
	}
}

func TestMimoTranscribeChunksCallsASRForEachChunkAndMergesText(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"片段` + string(rune('0'+requests)) + `"}}]}`))
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	chunkA := filepath.Join(tmpDir, "chunk-a.mp3")
	chunkB := filepath.Join(tmpDir, "chunk-b.mp3")
	if err := os.WriteFile(chunkA, []byte("chunk a"), 0644); err != nil {
		t.Fatalf("write chunk a: %v", err)
	}
	if err := os.WriteFile(chunkB, []byte("chunk b"), 0644); err != nil {
		t.Fatalf("write chunk b: %v", err)
	}

	strategy := NewMimoStrategy("tp-test-key", server.URL, "mimo-v2.5-asr", "mimo-v2.5")
	text, err := strategy.TranscribeChunks(context.Background(), []string{chunkA, chunkB})
	if err != nil {
		t.Fatalf("transcribe chunks: %v", err)
	}
	if text != "片段1\n\n片段2" {
		t.Fatalf("unexpected merged transcript: %q", text)
	}
	if requests != 2 {
		t.Fatalf("expected 2 ASR requests, got %d", requests)
	}
}

func mustFindInputAudioData(t *testing.T, request map[string]interface{}) string {
	t.Helper()

	messages, ok := request["messages"].([]interface{})
	if !ok || len(messages) == 0 {
		t.Fatalf("missing messages: %#v", request["messages"])
	}
	message, ok := messages[0].(map[string]interface{})
	if !ok {
		t.Fatalf("invalid first message: %#v", messages[0])
	}
	content, ok := message["content"].([]interface{})
	if !ok || len(content) == 0 {
		t.Fatalf("missing content: %#v", message["content"])
	}
	item, ok := content[0].(map[string]interface{})
	if !ok {
		t.Fatalf("invalid content item: %#v", content[0])
	}
	inputAudio, ok := item["input_audio"].(map[string]interface{})
	if !ok {
		t.Fatalf("missing input_audio: %#v", item)
	}
	data, ok := inputAudio["data"].(string)
	if !ok {
		t.Fatalf("missing input_audio data: %#v", inputAudio)
	}
	return data
}
