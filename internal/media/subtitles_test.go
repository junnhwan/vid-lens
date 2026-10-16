package media

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"vidlens/internal/config"
)

func TestTranscriptProvenanceAndCoverage(t *testing.T) {
	v, e := ParseSubtitles([]byte("WEBVTT\n\n00:00:00.000 --> 00:00:02.100\nA &amp; B\n\n00:00:02.100 --> 00:00:04.000\n<b>second</b>\n"), ".vtt")
	if e != nil || len(v) != 2 || v[0].Text != "A & B" || v[1].Text != "second" {
		t.Fatalf("%+v %v", v, e)
	}
	ok, coverage := TranscriptCoverage(v, 4000)
	if !ok || coverage != "complete" {
		t.Fatal(coverage)
	}
	ok, coverage = TranscriptCoverage(v, 10000)
	if ok || coverage != "partial" {
		t.Fatal(coverage)
	}
	b := []byte(`{"body":[{"from":0.125,"to":1.875,"content":"literal 2.5 kg"}]}`)
	v, e = ParseSubtitles(b, ".json")
	if e != nil || v[0].StartMS != 125 || v[0].EndMS != 1875 {
		t.Fatalf("%+v %v", v, e)
	}
}
func TestSubtitleChoicePrefersManualThenAuto(t *testing.T) {
	m := metadata{Subtitles: map[string][]subformat{"en": {{Ext: "vtt"}}}, Auto: map[string][]subformat{"zh": {{Ext: "json3"}}}}
	lang, auto := chooseSubtitle(m)
	if lang != "en" || auto {
		t.Fatalf("%s %v", lang, auto)
	}
	m.Subtitles = nil
	lang, auto = chooseSubtitle(m)
	if lang != "zh" || !auto {
		t.Fatalf("%s %v", lang, auto)
	}
	m.Subtitles = map[string][]subformat{"ja": {{Ext: "vtt"}}}
	lang, auto = chooseSubtitle(m)
	if lang != "ja" || auto {
		t.Fatalf("usable platform subtitles unnecessarily sent to ASR: %s %v", lang, auto)
	}
	m.Subtitles = map[string][]subformat{"ar": {{Ext: "vtt"}}, "zh-Hant": {{Ext: "vtt"}}}
	if lang, auto = chooseSubtitle(m); lang != "zh-Hant" || auto {
		t.Fatalf("language prefix preference changed: %s %v", lang, auto)
	}
}

func TestSubtitleChoiceSkipsUnparseableTrackBeforeASR(t *testing.T) {
	m := metadata{Subtitles: map[string][]subformat{"zh": {{Ext: "ttml"}}}, Auto: map[string][]subformat{"en": {{Ext: "json3"}}}}
	lang, auto := chooseSubtitle(m)
	if lang != "en" || !auto {
		t.Fatalf("usable automatic JSON3 was skipped for unsupported manual track; would fall back to whole-video ASR: %s auto=%v", lang, auto)
	}
}
func TestASRStreamingMultipartHasKnownLength(t *testing.T) {
	audio := bytes.Repeat([]byte{1, 2, 3}, 10000)
	file := filepath.Join(t.TempDir(), "sample.mp3")
	if e := os.WriteFile(file, audio, 0600); e != nil {
		t.Fatal(e)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ContentLength < 1 || len(r.TransferEncoding) > 0 {
			t.Error("ASR used an unknown-length chunked upload")
		}
		if e := r.ParseMultipartForm(1 << 20); e != nil {
			t.Error(e)
		}
		f, _, e := r.FormFile("file")
		if e != nil {
			t.Error(e)
			return
		}
		defer f.Close()
		got, _ := io.ReadAll(f)
		if !bytes.Equal(got, audio) || r.FormValue("model") != "asr-test" {
			t.Error("multipart data changed")
		}
		w.Header().Set("x-siliconcloud-trace-id", "test-trace")
		_ = json.NewEncoder(w).Encode(map[string]string{"text": "actual words"})
	}))
	defer server.Close()
	service := &Service{Config: config.Config{}}
	raw, id, status, e := service.asrRequest(context.Background(), config.Profile{ASRBaseURL: server.URL + "/v1/audio/transcriptions", ASRModel: "asr-test"}, "test-key", file)
	if e != nil || status != 200 || id != "test-trace" || !bytes.Contains(raw, []byte("actual words")) {
		t.Fatalf("%s %s %d %v", raw, id, status, e)
	}
}
func TestCopyBoundedNeverAcceptsOversize(t *testing.T) {
	file := filepath.Join(t.TempDir(), "download")
	_, n, e := copyBounded(context.Background(), file, bytes.NewReader(make([]byte, 101)), 100)
	if e == nil || n != 101 {
		t.Fatal("oversize download accepted")
	}
}
