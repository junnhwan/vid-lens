package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"unicode"

	"vid-lens/internal/artifact"
	"vid-lens/internal/model"
)

func TestForcedAlignmentProjectsCompleteSentenceSources(t *testing.T) {
	text := "第一句话。第二句话。"
	var words []model.TranscriptionSegment
	for i, r := range []rune(text) {
		if unicode.IsLetter(r) {
			words = append(words, model.TranscriptionSegment{Text: string(r), TextStart: i, TextEnd: i + 1, StartMS: int64(i) * 100, EndMS: int64(i+1) * 100, Method: "forced_alignment"})
		}
	}
	row := timedSourceRow(t, text, words)
	observations, timed := transcriptObservations(row, text)
	if !timed || len(observations) != 2 || observations[0].Content != "第一句话。" || observations[1].Content != "第二句话。" {
		t.Fatalf("character fragments escaped into canonical sources: count=%d", len(observations))
	}
	for _, o := range observations {
		if len(o.Refs) != 1 || o.Refs[0].ContentHash != artifact.Hash(o.Content) {
			t.Fatal("sentence source cannot freeze or map citations")
		}
	}
	if observations[0].Refs[0].StartMS != 0 || observations[0].Refs[0].EndMS != 400 || observations[1].Refs[0].StartMS != 500 {
		t.Fatal("sentence time did not retain observed word union")
	}
	retained := string([]rune(text)[2:])
	clipped, _ := transcriptObservations(row, retained, 2, len([]rune(text)))
	if clipped[0].Refs[0].StartMS != 200 || clipped[0].Refs[0].ContentHash != artifact.Hash(clipped[0].Content) || clipped[0].Refs[0].StableID == observations[0].Refs[0].StableID {
		t.Fatal("trimmed sentence lost its precise visible provenance")
	}
}

func timedSourceRow(t *testing.T, content string, segments []model.TranscriptionSegment) model.VideoTranscriptionChunk {
	t.Helper()
	raw, err := json.Marshal(segments)
	if err != nil {
		t.Fatal(err)
	}
	return model.VideoTranscriptionChunk{ID: 10, SegmentKey: "observed", Status: model.TranscriptionChunkStatusCompleted,
		Content: content, WindowStartMS: 0, WindowEndMS: 305000, TimedSegments: string(raw)}
}

func TestTimedTranscriptMapsDispersedSentencesWithoutWholeWindowTime(t *testing.T) {
	row := timedSourceRow(t, "缓存避免重复存储。进程可以随意重启。", []model.TranscriptionSegment{
		{Text: "缓存避免重复存储。", StartMS: 52000, EndMS: 57000},
		{Text: "进程可以随意重启。", StartMS: 241000, EndMS: 247000},
	})
	observations, timed := transcriptObservations(row, row.Content)
	if !timed || len(observations) != 2 {
		t.Fatalf("observations = %+v timed=%v", observations, timed)
	}
	chunks := buildTranscriptIndexChunks(row.Content, []model.VideoTranscriptionChunk{row}, 800, 0)
	if len(chunks) != 1 || len(chunks[0].SourceRefs) != 2 {
		t.Fatalf("chunks = %+v", chunks)
	}
	if chunks[0].SourceRefs[0].Content != "缓存避免重复存储。" || chunks[0].SourceRefs[0].StartMS != 52000 || chunks[0].SourceRefs[1].StartMS != 241000 {
		t.Fatalf("span mapping = %+v", chunks[0].SourceRefs)
	}
	timeline := BuildVideoTimeline(65, []model.VideoTranscriptionChunk{row}, nil)
	if len(timeline.Atoms) != 2 || timeline.Atoms[0].TimeRangeStatus != model.ChunkTimeRangeExact || timeline.Atoms[1].StartMS != 241000 {
		t.Fatalf("timeline = %+v", timeline)
	}
}

func TestTimedTranscriptPreservesStitchedSuffixAndLeavesGapsCoarse(t *testing.T) {
	row := timedSourceRow(t, "重复的边界。保留这句。没有时间戳。", []model.TranscriptionSegment{
		{Text: "重复的边界。", StartMS: 10000, EndMS: 15000},
		{Text: "保留这句。", StartMS: 16000, EndMS: 19000},
	})
	retained := "\n\n。保留这句。没有时间戳。"
	observations, timed := transcriptObservations(row, retained)
	var rebuilt strings.Builder
	var statuses []string
	for _, observation := range observations {
		rebuilt.WriteString(observation.Content)
		statuses = append(statuses, observation.Refs[0].TimeRangeStatus)
	}
	if !timed || rebuilt.String() != retained || len(observations) != 3 || statuses[0] != "exact" || statuses[1] != "exact" || statuses[2] != "coarse" {
		t.Fatalf("rebuilt=%q observations=%+v", rebuilt.String(), observations)
	}
}

func TestTimedTranscriptRejectsEditedOrInvalidSourceTime(t *testing.T) {
	row := timedSourceRow(t, "相同句子。相同句子。", []model.TranscriptionSegment{
		{Text: "相同句子。", StartMS: -1, EndMS: 10000},
		{Text: "相同句子。", StartMS: 18000, EndMS: 22000},
	})
	observations, timed := transcriptObservations(row, row.Content)
	if timed || len(observations) != 1 || observations[0].Refs[0].TimeRangeStatus != "coarse" {
		t.Fatalf("repeated source = %+v", observations)
	}
	if got, timed := transcriptObservations(row, "用户编辑的句子。"); timed || len(got) != 1 || got[0].Refs[0].TimeRangeStatus != "coarse" {
		t.Fatalf("edited source mapping = %+v timed=%v", got, timed)
	}
	row.TimedSegments = `[{"text":"相同句子。","start_ms":1,"end_ms":999999}]`
	if got, timed := transcriptObservations(row, row.Content); timed || got[0].Refs[0].TimeRangeStatus != "coarse" {
		t.Fatalf("out-of-window mapping = %+v", got)
	}
}

func TestPackedSourceRefsRetainOnlyVisibleText(t *testing.T) {
	chunks := SplitObservationsIntoChunks([]SourceTextObservation{{Content: "第一句话。第二句话。第三句话。", Modality: model.ChunkModalityTranscript,
		Refs: []ChunkSourceRef{{SourceType: "transcript", StableID: "row", StartMS: 0, EndMS: 300000, TimeRangeStatus: "coarse"}}}}, 40, 0)
	if len(chunks) < 2 {
		t.Fatalf("chunks = %+v", chunks)
	}
	for _, chunk := range chunks {
		if len(chunk.SourceRefs) != 1 || strings.TrimSpace(chunk.SourceRefs[0].Content) != chunk.Content {
			t.Fatalf("visible ref = %+v", chunk)
		}
		ref := chunk.SourceRefs[0]
		if string([]rune(chunk.Content)[ref.TextStart:ref.TextEnd]) != ref.Content {
			t.Fatalf("source text offsets no longer identify visible quote: %+v", chunk)
		}
	}
}

func TestPackedSourceOffsetsLocateIdenticalSentencesAtDifferentTimes(t *testing.T) {
	observations := []SourceTextObservation{
		{Content: "\n 可以重启。", Modality: "transcript", Refs: []ChunkSourceRef{{SourceType: "transcript", StableID: "a", StartMS: 1000, EndMS: 2000, TimeRangeStatus: "exact"}}},
		{Content: "另一观点。", Modality: "transcript", Refs: []ChunkSourceRef{{SourceType: "transcript", StableID: "b", StartMS: 10000, EndMS: 12000, TimeRangeStatus: "exact"}}},
		{Content: "可以重启。 \n", Modality: "transcript", Refs: []ChunkSourceRef{{SourceType: "transcript", StableID: "c", StartMS: 120000, EndMS: 122000, TimeRangeStatus: "exact"}}},
	}
	chunks := SplitObservationsIntoChunks(observations, 800, 0)
	if len(chunks) != 1 || len(chunks[0].SourceRefs) != 3 {
		t.Fatalf("chunks=%+v", chunks)
	}
	refs := chunks[0].SourceRefs
	if refs[0].Content != refs[2].Content || refs[0].TextStart == refs[2].TextStart {
		t.Fatalf("different occurrences lost text position: %+v", refs)
	}
	for _, ref := range refs {
		if string([]rune(chunks[0].Content)[ref.TextStart:ref.TextEnd]) != ref.Content {
			t.Fatalf("incorrect occurrence offset=%+v", ref)
		}
	}
}

func TestCitationCandidatesDoNotExposeRetainedBoundaryPunctuationAsEvidence(t *testing.T) {
	citations := buildCitations("剩余内容", []RetrievedChunk{{Content: "。剩余的真实句子。", Modality: "transcript", SourceMappingStatus: "mapped", TimeRangeStatus: "coarse", StartMS: 18000, EndMS: 42000}})
	if len(citations) != 1 || citations[0].AnchorQuote != "剩余的真实句子。" {
		t.Fatalf("punctuation-only citation=%+v", citations)
	}
}

func TestTimedTranscriptKeepsUnmatchedGapsAsSeparateVerbatimSources(t *testing.T) {
	row := timedSourceRow(t, "未定时的开头。中间有真实时间。未定时的结尾。", []model.TranscriptionSegment{
		{Text: "中间有真实时间。", StartMS: 10000, EndMS: 14000},
	})
	chunks := buildTranscriptIndexChunks(row.Content, []model.VideoTranscriptionChunk{row}, 800, 0)
	if len(chunks) != 1 || len(chunks[0].SourceRefs) != 3 {
		t.Fatalf("gaps merged: %+v", chunks)
	}
	for _, ref := range chunks[0].SourceRefs {
		if !strings.Contains(row.Content, ref.Content) {
			t.Fatalf("fabricated context from disconnected gaps: %+v", ref)
		}
	}
}

func TestPartiallyTimedTimelinePreservesSpeechTextOrderAndCanonicalIdentity(t *testing.T) {
	row := timedSourceRow(t, "第一句。未定时的第二句。第三句。", []model.TranscriptionSegment{
		{Text: "第一句。", StartMS: 5000, EndMS: 7000},
		{Text: "第三句。", StartMS: 15000, EndMS: 17000},
	})
	timeline := BuildVideoTimeline(65, []model.VideoTranscriptionChunk{row}, nil)
	var rebuilt strings.Builder
	for _, atom := range timeline.Atoms {
		rebuilt.WriteString(atom.Content)
		ref := atom.SourceRefs[0]
		if atom.ID != ref.SourceType+":"+ref.StableID || ref.ContentHash != artifact.Hash(atom.Content) {
			t.Fatalf("source snapshot identity/hash diverges: atom=%+v", atom)
		}
	}
	if rebuilt.String() != row.Content || timeline.Atoms[1].TimeRangeStatus != "coarse" || timeline.Atoms[1].StartMS != 0 {
		t.Fatalf("reader reordered text or invented gap time: %+v", timeline)
	}
	// A retained suffix still points to the complete native source observation.
	observations, _ := transcriptObservations(row, "。未定时的第二句。第三句。")
	if observations[0].Refs[0].ContentHash != timeline.Atoms[0].SourceRefs[0].ContentHash || observations[0].Refs[0].StableID != timeline.Atoms[0].SourceRefs[0].StableID {
		t.Fatalf("stitched excerpt lost canonical identity: %+v", observations)
	}
}

func TestFineTimelineDoesNotRejectOrdinaryLengthStudySource(t *testing.T) {
	var content strings.Builder
	segments := make([]model.TranscriptionSegment, 0, 1050)
	for i := 0; i < 1050; i++ {
		text := fmt.Sprintf("第%d句。", i)
		content.WriteString(text)
		segments = append(segments, model.TranscriptionSegment{Text: text, StartMS: int64(i) * 2000, EndMS: int64(i+1) * 2000})
	}
	row := timedSourceRow(t, content.String(), segments)
	row.WindowEndMS = 2100000
	timeline := BuildVideoTimeline(65, []model.VideoTranscriptionChunk{row}, nil)
	if len(timeline.Atoms) != 1050 {
		t.Fatalf("native source count=%d", len(timeline.Atoms))
	}
	if reason := studySourceReason(&model.VideoTask{Status: model.TaskStatusCompleted}, []model.VideoTranscriptionChunk{row}, timeline); reason != "" {
		t.Fatalf("ordinary speech rejected only due to finer spans: %s", reason)
	}
	svc, db, _ := artifactFixture(t, artifactModelResponse)
	row.ID, row.TaskID = 0, 42
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.VideoTranscription{}).Where("task_id = ?", 42).Update("content", row.Content).Error; err != nil {
		t.Fatal(err)
	}
	source, err := svc.Source(context.Background(), 7, 42)
	if err != nil || len(source.Evidence) != 1050 {
		t.Fatalf("fine sources could not freeze: count=%v err=%v", source, err)
	}
}

func TestForcedAlignmentLongVideoFreezesSentenceSourcesAndMapsRetrieval(t *testing.T) {
	// A character-based aligner produces over 10,000 acoustic tokens on an
	// ordinary lesson. Evidence must remain readable sentences, and retrieval
	// excerpts must still identify exactly those frozen sentence sources.
	text := strings.Repeat("这是一个包含完整语音时间并可以回放的句子。", 600)
	var words []model.TranscriptionSegment
	for i, r := range []rune(text) {
		if unicode.IsLetter(r) {
			words = append(words, model.TranscriptionSegment{Text: string(r), TextStart: i, TextEnd: i + 1,
				StartMS: int64(i) * 100, EndMS: int64(i+1) * 100, Method: "forced_alignment"})
		}
	}
	if len(words) <= 10000 {
		t.Fatal("fixture no longer exercises a long word-level alignment")
	}
	row := timedSourceRow(t, text, words)
	row.WindowEndMS = int64(len([]rune(text))) * 100
	row.CoreEndMS = row.WindowEndMS
	svc, db, calls := artifactFixture(t, artifactModelResponse)
	row.ID, row.TaskID = 0, 42
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.VideoTranscription{}).Where("task_id = ?", 42).Update("content", row.Content).Error; err != nil {
		t.Fatal(err)
	}
	source, err := svc.Source(context.Background(), 7, 42)
	if err != nil || source == nil || len(source.Evidence) != 600 {
		t.Fatalf("word alignment fragmented or rejected a normal lesson: source=%v err=%v", source, err)
	}
	frozen := make(map[string]model.SourceSnapshotItem, len(source.Evidence))
	for _, evidence := range source.Evidence {
		frozen[evidence.SourceIdentity] = evidence
	}
	chunks := buildTranscriptIndexChunks(text, []model.VideoTranscriptionChunk{row}, 100, 20)
	for _, chunk := range chunks {
		if chunk.SourceMappingStatus != model.ChunkSourceMapped || len(chunk.SourceRefs) == 0 {
			t.Fatal("sentence evidence lost retrieval provenance")
		}
		for _, ref := range chunk.SourceRefs {
			evidence, ok := frozen[ref.SourceType+":"+ref.StableID]
			if !ok || evidence.ContentHash != ref.ContentHash || evidence.StartMS == nil || *evidence.StartMS != ref.StartMS || *evidence.EndMS != ref.EndMS {
				t.Fatalf("retrieval cannot resolve frozen sentence: %+v", ref)
			}
		}
	}
	if calls.Load() != 0 {
		t.Fatal("source freezing unexpectedly rewrote transcript through AI")
	}
}
