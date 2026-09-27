package studyterms

import (
	"strings"
	"testing"

	"vid-lens/internal/model"
)

func termItem(id, modality, content string, start, end int64) model.SourceSnapshotItem {
	return model.SourceSnapshotItem{ID: id, Modality: modality, Content: content, StartMS: &start, EndMS: &end}
}

func TestDeriveStudyTermEvidenceKeepsOriginalAndShowsPossibleConflict(t *testing.T) {
	items := []model.SourceSnapshotItem{
		termItem("asr-1", model.ChunkModalityTranscript, "给 Pad 装系统。", 0, 300_000),
		termItem("ocr-1", model.ChunkModalityVisualOCR, "Pi\n安装步骤", 50_000, 50_001),
		termItem("vision-1", model.ChunkModalityVisualCaption, "画面展示 Pi 开发板。", 50_000, 50_001),
	}
	result := DeriveStudyTermEvidence(items)
	var pi *StudyTermCandidate
	for i := range result.Candidates {
		if result.Candidates[i].Term == "Pi" {
			pi = &result.Candidates[i]
		}
	}
	if pi == nil || len(pi.VisualSupport) != 2 || len(pi.PossibleConflicts) != 1 || pi.PossibleConflicts[0].EvidenceID != "asr-1" {
		t.Fatalf("Pi evidence = %+v", pi)
	}
	if items[0].Content != "给 Pad 装系统。" || items[1].Content != "Pi\n安装步骤" {
		t.Fatal("raw source changed")
	}
	if !ValidateStudyTermCorrection(items, StudyTermCorrection{From: "Pad", To: "Pi", TranscriptEvidenceID: "asr-1", VisualEvidenceID: "ocr-1"}) {
		t.Fatal("explicit cross-modal evidence should be accepted for semantic review")
	}
}

func TestStudyTermCorrectionRejectsSimilarNamesAndUnknownTiming(t *testing.T) {
	base := []model.SourceSnapshotItem{
		termItem("asr", model.ChunkModalityTranscript, "讲解 Pad 和 Pi 两款设备。", 0, 300_000),
		termItem("screen", model.ChunkModalityVisualOCR, "Pi", 20_000, 20_001),
		termItem("other-screen", model.ChunkModalityVisualOCR, "Pad", 30_000, 30_001),
	}
	proposal := StudyTermCorrection{From: "Pad", To: "Pi", TranscriptEvidenceID: "asr", VisualEvidenceID: "screen"}
	if ValidateStudyTermCorrection(base, proposal) {
		t.Fatal("competing visual spelling must block automatic acceptance")
	}
	base = base[:2]
	base[0].StartMS, base[0].EndMS = nil, nil
	if ValidateStudyTermCorrection(base, proposal) {
		t.Fatal("unknown alignment must not authorize a correction")
	}
	base[0] = termItem("asr", model.ChunkModalityTranscript, "讲解 iPad。", 0, 300_000)
	if ValidateStudyTermCorrection(base, proposal) {
		t.Fatal("Pad must not match the interior of iPad")
	}
	base[0] = termItem("asr", model.ChunkModalityTranscript, "讲解 Pad。", 300_000, 600_000)
	if ValidateStudyTermCorrection(base, proposal) {
		t.Fatal("distant visual evidence must not support replacement")
	}
}

func TestStudyTermCandidateRequiresVisualSource(t *testing.T) {
	items := []model.SourceSnapshotItem{termItem("asr", model.ChunkModalityTranscript, "Pad 和普通设备", 0, 1000)}
	if result := DeriveStudyTermEvidence(items); len(result.Candidates) != 0 {
		t.Fatalf("ASR-only candidates = %+v", result.Candidates)
	}
	items = append(items, termItem("ocr", model.ChunkModalityVisualOCR, "树莓派", 10, 11))
	result := DeriveStudyTermEvidence(items)
	if len(result.Candidates) != 1 || result.Candidates[0].Term != "树莓派" || result.Candidates[0].VisualSupport[0].EvidenceID != "ocr" {
		t.Fatalf("Chinese OCR candidate = %+v", result.Candidates)
	}
}

func TestFocusEvidenceReportsOmittedHintsWithoutChangingInput(t *testing.T) {
	input := StudyTermEvidence{Candidates: []StudyTermCandidate{{Term: "A"}, {Term: "B"}}, VisualObservations: []StudyTermSource{{EvidenceID: "v1"}, {EvidenceID: "v2"}}, OmittedCandidates: 3, OmittedVisualObservations: 1}
	out := FocusEvidence(input, 1, 1)
	if len(out.Candidates) != 1 || len(out.VisualObservations) != 1 || out.OmittedCandidates != 4 || out.OmittedVisualObservations != 2 {
		t.Fatalf("bounded evidence: %+v", out)
	}
	if len(input.Candidates) != 2 || len(input.VisualObservations) != 2 {
		t.Fatal("original evidence mutated")
	}
}

func TestMultiwordVisualNameRetainsOriginalSpelling(t *testing.T) {
	items := []model.SourceSnapshotItem{
		termItem("asr", model.ChunkModalityTranscript, "从 Cloud Code 转向 Pi。", 0, 1000),
		termItem("vision", model.ChunkModalityVisualCaption, "画面写着 Claude Code 和 Pi。", 100, 101),
	}
	evidence := DeriveStudyTermEvidence(items)
	found := false
	for _, c := range evidence.Candidates {
		if c.Term == "Claude Code" {
			found = true
			if len(c.VisualSupport) == 0 || len(c.PossibleConflicts) == 0 {
				t.Fatalf("missing original support/conflict: %+v", c)
			}
		}
	}
	if !found {
		t.Fatalf("multiword name missing: %+v", evidence.Candidates)
	}
	if !ValidateStudyTermCorrection(items,StudyTermCorrection{From:"Cloud Code",To:"Claude Code",TranscriptEvidenceID:"asr",VisualEvidenceID:"vision"}) {t.Fatal("multiword correction evidence rejected")}
}

func TestStudyTermEvidenceIncludesQuotedVisionNameAndVisualConflict(t *testing.T) {
	items := []model.SourceSnapshotItem{
		termItem("ocr", model.ChunkModalityVisualOCR, "Pi", 10, 11),
		termItem("vision", model.ChunkModalityVisualCaption, "画面写着《树莓派》，旁边标出 Pad。", 10, 11),
	}
	result := DeriveStudyTermEvidence(items)
	var sawChinese, sawConflict bool
	for _, candidate := range result.Candidates {
		if candidate.Term == "树莓派" && len(candidate.VisualSupport) == 1 && candidate.VisualSupport[0].EvidenceID == "vision" {
			sawChinese = true
		}
		if candidate.Term == "Pi" && len(candidate.PossibleConflicts) == 1 && candidate.PossibleConflicts[0].EvidenceID == "vision" {
			sawConflict = true
		}
	}
	if !sawChinese || !sawConflict {
		t.Fatalf("quoted name or visual conflict missing: %+v", result)
	}
}

func TestStudyTermEvidenceKeepsUnquotedVisionObservationWithBoundedDisclosure(t *testing.T) {
	items := []model.SourceSnapshotItem{termItem("vision", model.ChunkModalityVisualCaption, "画面展示树莓派设备。", 10, 11)}
	result := DeriveStudyTermEvidence(items)
	if len(result.Candidates) != 0 || len(result.VisualObservations) != 1 || result.VisualObservations[0].Excerpt != "画面展示树莓派设备。" {
		t.Fatalf("unquoted Vision observation lost: %+v", result)
	}
	items[0].Content = strings.Repeat("树", 300)
	result = DeriveStudyTermEvidence(items)
	if len([]rune(result.VisualObservations[0].Excerpt)) != 240 || !result.VisualObservations[0].Truncated {
		t.Fatalf("long observation was not marked truncated: %+v", result.VisualObservations[0])
	}
}
