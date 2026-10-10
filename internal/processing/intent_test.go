package processing

import (
	"encoding/json"
	"strings"
	"testing"
	"vid-lens/internal/ai"
)

func TestFrozenIntentRoundTripAndValidation(t *testing.T) {
	options, err := Normalize(Options{AutoSummary: true, AutoTagsEnabled: true, SummaryInstruction: "  关注配置  "}, false)
	if err != nil || options.SummaryInstruction != "关注配置" || options.OutputMode != "auto" || options.TextSourcePolicy != "prefer_platform" {
		t.Fatalf("normalized=%+v err=%v", options, err)
	}
	intent := Intent{ID: "import-1", GenerationID: "gen-1", Version: 1, RecipeVersion: Recipe, Options: options}
	raw, _ := json.Marshal(intent)
	got, err := Decode(string(raw))
	if err != nil || got != intent {
		t.Fatalf("decode=%+v %v", got, err)
	}
	local, err := Normalize(options, true)
	if err != nil || local.TextSourcePolicy != "force_asr" {
		t.Fatalf("local=%+v %v", local, err)
	}
	for _, bad := range []Options{{TextSourcePolicy: "other"}, {OutputMode: "image_text"}, {SummaryInstruction: strings.Repeat("字", 2001)}} {
		if _, err := Normalize(bad, false); err == nil {
			t.Fatalf("accepted=%+v", bad)
		}
	}
	intent.Options.TextSourcePolicy = ""
	raw, _ = json.Marshal(intent)
	if _, err := Decode(string(raw)); err == nil {
		t.Fatal("malformed frozen options silently renormalized")
	}
}

func TestProfileFingerprintRejectsASRModelDriftWithoutStoringKeys(t *testing.T) {
	profile := ai.Profile{ID: 1, LLMProvider: "openai", LLMModel: "model-1", LLMAPIKey: "secret", ASRModel: "speech-1"}
	before := FingerprintProfile(profile)
	profile.LLMAPIKey = "rotated"
	if FingerprintProfile(profile) != before {
		t.Fatal("credential rotation changes config identity")
	}
	profile.ASRModel = "speech-2"
	if FingerprintProfile(profile) == before {
		t.Fatal("ASR config drift omitted")
	}
}
