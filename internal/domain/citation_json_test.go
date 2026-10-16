package domain

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"
)

func TestExternalCitationSurvivesAnswerJSONRoundTrip(t *testing.T) {
	readAt := time.Date(2026, 10, 16, 10, 5, 57, 504974000, time.UTC)
	cite := Citation{ID: "web", Kind: "external_reference", Label: "Official source", ReferenceID: "reference", ContentSHA256: "content-hash", URL: "https://example.com/source", Title: "Actual page", ReadAt: &readAt, Quote: Ptr("Actual source excerpt.")}
	want := Answer{Kind: "grounded", Summary: "An answer with a web source", Citations: map[string]Citation{cite.ID: cite}}
	var got Answer
	if err := json.Unmarshal(JSON(want), &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Citations[cite.ID], cite) {
		t.Fatalf("answer serialization lost web provenance: %+v", got.Citations[cite.ID])
	}
}
