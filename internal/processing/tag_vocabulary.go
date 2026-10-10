package processing

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
	"vid-lens/internal/usertags"
)

// These bounds apply to the complete vocabulary payload sent to the model,
// independently of the maximum five classification results.
const MaxTagVocabularyCandidates = 64
const MaxTagVocabularyBytes = 16 * 1024

type TagVocabularyEntry struct {
	TagID   string   `json:"tag_id"`
	Name    string   `json:"name"`
	Aliases []string `json:"aliases"`
}

// TagVocabularySnapshot freezes owner-authorized names and aliases at request
// acceptance. Truncated explicitly identifies a deterministic shortlist.
type TagVocabularySnapshot struct {
	Version    int64                `json:"version"`
	Candidates []TagVocabularyEntry `json:"candidates"`
	Truncated  bool                 `json:"truncated"`
}

func (s *TagVocabularySnapshot) Validate() error {
	if s == nil {
		return nil
	} // Previously accepted snapshots remain readable.
	raw, err := json.Marshal(s)
	if err != nil || len(raw) > MaxTagVocabularyBytes || len(s.Candidates) > MaxTagVocabularyCandidates || s.Version < 0 {
		return fmt.Errorf("invalid frozen tag vocabulary")
	}
	seen := map[string]bool{}
	for _, candidate := range s.Candidates {
		if candidate.TagID == "" || len(candidate.TagID) > 128 || !utf8.ValidString(candidate.TagID) || strings.IndexFunc(candidate.TagID, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0 || seen[candidate.TagID] || len(candidate.Aliases) > 20 {
			return fmt.Errorf("invalid frozen tag vocabulary")
		}
		seen[candidate.TagID] = true
		for _, name := range append([]string{candidate.Name}, candidate.Aliases...) {
			if _, _, err := usertags.Normalize(name, 80); err != nil {
				return fmt.Errorf("invalid frozen tag vocabulary")
			}
		}
	}
	return nil
}
