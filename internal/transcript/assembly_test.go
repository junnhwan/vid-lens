package transcript

import (
	"encoding/json"
	"strings"
	"testing"

	"vid-lens/internal/model"
)

func alignedRow(index int, text string, windowStart, windowEnd, coreStart, coreEnd int64, times [][2]int64) model.VideoTranscriptionChunk {
	words := make([]model.TranscriptionSegment, len([]rune(text)))
	for i, r := range []rune(text) {
		words[i] = model.TranscriptionSegment{Text: string(r), TextStart: i, TextEnd: i + 1, StartMS: times[i][0], EndMS: times[i][1], Method: "forced_alignment"}
	}
	raw, _ := json.Marshal(words)
	return model.VideoTranscriptionChunk{ChunkIndex: index, SegmentKey: text, SegmenterVersion: "v2", Content: text, Status: model.TranscriptionChunkStatusCompleted,
		WindowStartMS: windowStart, WindowEndMS: windowEnd, CoreStartMS: coreStart, CoreEndMS: coreEnd, TimedSegments: string(raw)}
}

func TestTimeOwnershipKeepsOneOccurrenceDespiteASRSpellingDifference(t *testing.T) {
	left := alignedRow(0, "甲乙丙", 0, 22000, 0, 20000, [][2]int64{{1000, 2000}, {19000, 19500}, {21000, 21500}})
	right := alignedRow(1, "已丙丁", 18000, 42000, 20000, 40000, [][2]int64{{19000, 19500}, {21000, 21500}, {30000, 31000}})
	result := Assemble([]model.VideoTranscriptionChunk{left, right})
	if result.Content != "甲乙丙丁" {
		t.Fatalf("overlapping recognition leaked into transcript: %+v", result)
	}
	if result.Contributions[1].StartRune != 1 || result.Contributions[0].EndRune != 2 {
		t.Fatalf("source ranges lost: %+v", result)
	}
}

func TestTimeOwnershipPreservesActuallyRepeatedWord(t *testing.T) {
	left := alignedRow(0, "甲甲", 0, 22000, 0, 20000, [][2]int64{{1000, 2000}, {19000, 19500}})
	right := alignedRow(1, "甲甲乙", 18000, 42000, 20000, 40000, [][2]int64{{19000, 19500}, {21000, 21500}, {30000, 31000}})
	if got := Assemble([]model.VideoTranscriptionChunk{left, right}).Content; got != "甲甲甲乙" {
		t.Fatalf("legitimate repetition erased: %q", got)
	}
}

func TestTimeOwnershipReconcilesQuantizedBoundaryInsteadOfLosingCharacter(t *testing.T) {
	// Real 03:00 boundary: one window puts 刚's midpoint exactly at the core
	// boundary, while the other puts it 40 ms before. Independent cuts lose it.
	left := alignedRow(0, "我们把刚才", 0, 22000, 0, 20000, [][2]int64{{19120, 19360}, {19360, 19600}, {19600, 19840}, {19920, 20080}, {20080, 20400}})
	right := alignedRow(1, "我们把刚才创建", 18000, 42000, 20000, 40000, [][2]int64{{19200, 19360}, {19360, 19600}, {19600, 19840}, {19840, 20080}, {20080, 20400}, {20400, 20640}, {20640, 20720}})
	if got := Assemble([]model.VideoTranscriptionChunk{left, right}).Content; got != "我们把刚才创建" {
		t.Fatalf("independent clock quantization lost speech: %q", got)
	}
}

func TestAssembleChecksEachBoundaryAndSilenceBreaksMatching(t *testing.T) {
	rows := []model.VideoTranscriptionChunk{
		{ChunkIndex: 0, SegmentKey: "a", SegmenterVersion: "v", WindowEndMS: 22000, Status: "completed", Content: "开始。共同边界"},
		{ChunkIndex: 1, SegmentKey: "b", SegmenterVersion: "v", WindowStartMS: 18000, WindowEndMS: 42000, Status: "completed", Content: "共同边界。继续。"},
		{ChunkIndex: 2, SegmentKey: "c", SegmenterVersion: "v", WindowStartMS: 38000, WindowEndMS: 62000, Status: "completed"},
		{ChunkIndex: 3, SegmentKey: "d", SegmenterVersion: "v", WindowStartMS: 58000, WindowEndMS: 82000, Status: "completed", Content: "继续。后文。"},
	}
	result := Assemble(rows)
	if !strings.HasPrefix(result.Content, "开始。共同边界。继续。\n\n继续。") {
		t.Fatalf("silence altered earlier boundary or repetition: %q", result.Content)
	}
}

func TestAlignedWordsRejectMissingEditedAndOutOfRangeSource(t *testing.T) {
	row := alignedRow(0, "甲乙", 0, 22000, 0, 20000, [][2]int64{{1000, 2000}, {19000, 19500}})
	var words []model.TranscriptionSegment
	_ = json.Unmarshal([]byte(row.TimedSegments), &words)
	for _, mutate := range []func([]model.TranscriptionSegment) []model.TranscriptionSegment{
		func(x []model.TranscriptionSegment) []model.TranscriptionSegment { return x[1:] },
		func(x []model.TranscriptionSegment) []model.TranscriptionSegment { x[0].Text = "改"; return x },
		func(x []model.TranscriptionSegment) []model.TranscriptionSegment { x[0].EndMS = 999999; return x },
	} {
		if err := ValidateAlignedWords(row, mutate(append([]model.TranscriptionSegment(nil), words...))); err == nil {
			t.Fatal("unverified alignment accepted")
		}
	}
}
