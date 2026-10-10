package processing

import (
	"strings"
	"testing"
)

func TestFrozenTagVocabularyRejectsUnboundedOrMalformedPromptInputs(t *testing.T) {
	valid := TagVocabularyEntry{TagID: "owner-tag", Name: "Go", Aliases: []string{"Golang"}}
	for _, snapshot := range []TagVocabularySnapshot{
		{Version: -1},
		{Candidates: []TagVocabularyEntry{valid, valid}},
		{Candidates: []TagVocabularyEntry{{TagID: "bad id", Name: "Go"}}},
		{Candidates: []TagVocabularyEntry{{TagID: "id", Name: strings.Repeat("字", 81)}}},
		{Candidates: []TagVocabularyEntry{{TagID: "id", Name: "Go", Aliases: make([]string, 21)}}},
		{Candidates: make([]TagVocabularyEntry, MaxTagVocabularyCandidates+1)},
	} {
		if snapshot.Validate() == nil {
			t.Fatal("invalid vocabulary accepted")
		}
	}
	validSnapshot := TagVocabularySnapshot{Candidates: []TagVocabularyEntry{valid}}
	if validSnapshot.Validate() != nil {
		t.Fatal("valid owner snapshot rejected")
	}
	if (*TagVocabularySnapshot)(nil).Validate() != nil {
		t.Fatal("old frozen intent lost compatibility")
	}
}
