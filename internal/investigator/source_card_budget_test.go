package investigator

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/cloudwego/eino/schema"
	"vidlens/internal/domain"
)

// Synthetic coarse ASR windows reproduce the incident's card lengths without
// using a reserved video as a fixture. A valid prefix must remain reusable.
func denseSourceCardFixture() *session {
	s := indexedFixture()
	s.cp.ExecutionContract.Version = domain.ExecutionContractV9
	s.transcript = nil
	for i := 0; i < 68; i++ {
		s.transcript = append(s.transcript, domain.Segment{
			ID: fmt.Sprint("coarse-", i), Ordinal: i, StartMS: int64(i) * 60000, EndMS: int64(i+1) * 60000,
			Text: strings.Repeat("原文保留数字、条件与推理，不能用材料卡替代。", 32), Origin: "asr",
		})
	}
	return s
}

func TestDenseSourceCardCorrectionAdvancesWithoutLosingSourceOrSavedCards(t *testing.T) {
	s := denseSourceCardFixture()
	original := domain.JSON(s.transcript)
	chunks := s.sourceChunks()
	if !s.sourceSurveyPending() || len(chunks) < 2 {
		t.Fatal("fixture must exercise a dense, multi-chunk source")
	}
	s.cp.SourceNotes = []sourceNote{{ChunkID: chunks[0].ID, Summary: strings.Repeat("前段已保存的主题与条件仍须保留。", 4), EvidenceIDs: []string{chunks[0].Passages[0].Alias}}}
	saved := domain.JSON(s.cp.SourceNotes[0])
	_ = s.sourceContext()
	current := chunks[1]
	for _, length := range []int{1835, 1800, 1755} {
		note := sourceNote{ChunkID: current.ID, Summary: strings.Repeat("文", length), EvidenceIDs: []string{current.Passages[0].Alias}}
		if err := validateSourceNote(note, current); err != nil {
			t.Logf("correction with %d characters rejected: %v", length, err)
			continue
		}
		s.cp.SourceNotes = append(s.cp.SourceNotes, note)
		if utf8.RuneCountInString(s.cp.SourceNotes[1].Summary) != length {
			t.Fatal("accepted navigation card was silently truncated")
		}
		break
	}
	if len(s.cp.SourceNotes) != 2 {
		t.Fatalf("dense source remains stuck after three real correction responses: saved=%d/%d", len(s.cp.SourceNotes), len(chunks))
	}
	if !bytes.Equal(original, domain.JSON(s.transcript)) || !bytes.Equal(saved, domain.JSON(s.cp.SourceNotes[0])) {
		t.Fatal("fix discarded original text or rewrote a saved card")
	}
}

func TestAdaptiveCardsKeepLegacyChunkIdentitiesAndFrozenLimits(t *testing.T) {
	current := denseSourceCardFixture()
	legacy := denseSourceCardFixture()
	legacy.cp.ExecutionContract.Version = domain.ExecutionContractV4
	acceptedDense := false
	for i, chunk := range current.sourceChunks() {
		old := legacy.sourceChunks()[i]
		if chunk.ID != old.ID || !bytes.Equal(domain.JSON(chunk.Passages), domain.JSON(old.Passages)) {
			t.Fatal("new card policy changed source identity or original passages")
		}
		note := sourceNote{ChunkID: chunk.ID, Summary: strings.Repeat("文", 1835), EvidenceIDs: []string{chunk.Passages[0].Alias}}
		if validateSourceNote(note, old) == nil {
			t.Fatal("new density budget changed the frozen V4 limit")
		}
		if validateSourceNote(note, chunk) == nil {
			acceptedDense = true
		}
	}
	if !acceptedDense {
		t.Fatal("dense originals received no density-based card budget")
	}
	if legacy.sourceInstructions() != sourceContextInstructions || !strings.Contains(current.sourceInstructions(), adaptiveSourceCardInstructions) {
		t.Fatal("new policy changed old instructions or omitted the new contract")
	}
}

func TestAdaptiveCardBudgetBoundsWholeLedgerAndActualOutputCapacity(t *testing.T) {
	s := denseSourceCardFixture()
	chunk := s.sourceChunks()[0]
	initial := s.sourceCardLimits(chunk)
	if initial.Maximum <= 1835 || initial.Target < 30 || initial.Target >= initial.Maximum {
		t.Fatal("dense source did not get a feasible target with correction headroom", initial)
	}
	s.profile.MaxOutputTokens = 1024
	if limited := s.sourceCardLimits(chunk); limited.Maximum >= initial.Maximum || limited.Maximum*2+512 > 1024 {
		t.Fatal("card limit ignored the actual source-survey output budget", limited)
	}
	s.profile.MaxOutputTokens = 10000
	s.profile.ContextTokens = 16000
	if constrained := s.sourceCardLimits(chunk); constrained.Maximum >= initial.Maximum || constrained.NavigationMax > 2000 {
		t.Fatal("small context did not constrain the complete ledger", constrained)
	}
	// Escaped JSON is counted as physically delivered text, not just raw
	// summary runes. Legal reference/length fields cannot bypass this cap.
	note := sourceNote{ChunkID: chunk.ID, Summary: strings.Repeat("\n", 400), EvidenceIDs: []string{chunk.Passages[0].Alias}}
	s.cp.SourceNotes = []sourceNote{{ChunkID: "saved", Summary: strings.Repeat("文", 1100), EvidenceIDs: []string{"t1"}}}
	if s.sourceCardFitsNavigation(note) {
		t.Fatal("serialized navigation escaped its global envelope")
	}
	s.cp.SourceNotes[0].Summary = strings.Repeat("文", 2100)
	before := domain.JSON(s.cp.SourceNotes)
	if err := s.checkSourceCardCapacity(); err == nil {
		t.Fatal("exhausted navigation would spend another physical model call")
	} else {
		var problem *domain.Problem
		if !errors.As(err, &problem) || problem.Meta["budget_reason"] != "source_navigation_exceeded" {
			t.Fatal("capacity exhaustion lost its explicit resource result", err)
		}
	}
	if !bytes.Equal(before, domain.JSON(s.cp.SourceNotes)) {
		t.Fatal("capacity exhaustion discarded saved progress")
	}
}

func TestAdaptiveCardToolPersistsProgressAndReturnsExecutableCorrection(t *testing.T) {
	s := planSession(t)
	s.cp.HarnessRevision = 16
	s.cp.ExecutionContract = &domain.ExecutionContract{Version: domain.ExecutionContractV5, RunID: s.claim.Run.ID}
	s.cp.Messages = []*schema.Message{schema.SystemMessage(s.sourceInstructions())}
	for i := range s.transcript {
		s.transcript[i].Text = strings.Repeat("原文重要条件与结论。", 650)
	}
	if _, err := s.makeTools(); err != nil {
		t.Fatal(err)
	}
	for i, length := range []int{1000, 1835} {
		_ = s.sourceContext()
		chunk := s.sourceChunks()[i]
		note := sourceNote{ChunkID: chunk.ID, Summary: strings.Repeat("文", length), EvidenceIDs: []string{chunk.Passages[0].Alias}}
		if result, err := planCall(s, fmt.Sprint("dense-card-", i), "record_source_notes", note); err != nil || result["saved"] != true {
			t.Fatal("dense card did not advance through the real tool", result, err)
		}
	}
	prefix := domain.JSON(s.cp.SourceNotes)
	_ = s.sourceContext()
	chunk := s.sourceChunks()[2]
	s.cp.Calls = 3
	bad := sourceNote{ChunkID: chunk.ID, Summary: strings.Repeat("文", 5000), EvidenceIDs: []string{chunk.Passages[0].Alias}}
	feedback, err := planCall(s, "oversized-current-card", "record_source_notes", bad)
	if err != nil || feedback["error"] == nil || feedback["summary_target_characters"] == nil || feedback["summary_excess_characters"].(float64) <= 0 || len(s.cp.SourceNotes) != 2 {
		t.Fatal("oversized card lost bounded, actionable feedback or prior progress", feedback, err)
	}
	limits := s.sourceCardLimits(chunk)
	info, err := s.tools["record_source_notes"].Info(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	sc, err := info.ParamsOneOf.ToJSONSchema()
	if err != nil {
		t.Fatal(err)
	}
	var root map[string]any
	if err = json.Unmarshal(mustSchemaJSON(sc), &root); err != nil {
		t.Fatal(err)
	}
	maximum := root["properties"].(map[string]any)["summary"].(map[string]any)["maxLength"]
	if maximum != float64(limits.Maximum) || feedback["summary_max_characters"] != maximum {
		t.Fatal("actual provider schema and correction disagreed", maximum, feedback)
	}
	s.cp.Calls++
	bad.Summary = strings.Repeat("文", limits.Target)
	if result, err := planCall(s, "compressed-current-card", "record_source_notes", bad); err != nil || result["saved"] != true || s.cp.Progress.Stalled != 0 {
		t.Fatal("targeted correction did not advance and reset genuine progress", result, err)
	}
	if !bytes.Equal(prefix, domain.JSON(s.cp.SourceNotes[:2])) || utf8.RuneCount(domain.JSON(s.cp.SourceNotes)) > s.sourceNavigationLimit() {
		t.Fatal("correction changed prior cards or overflowed the complete ledger")
	}
	run, err := s.engine.Store.Run(context.Background(), s.claim.Run.ID)
	if err != nil {
		t.Fatal(err)
	}
	var restored checkpoint
	if err = json.Unmarshal(run.Checkpoint, &restored); err != nil || !bytes.Equal(domain.JSON(restored.SourceNotes), domain.JSON(s.cp.SourceNotes)) {
		t.Fatal("accepted card progress was not durable", err)
	}
	// Reconstruct the session from the durable checkpoint, including a V4
	// prefix, and verify it remains valid without rewriting any saved card.
	s.cp = restored
	if err = s.validateExecution(); err != nil {
		t.Fatal("saved cards could not resume under their frozen policy", err)
	}
}
