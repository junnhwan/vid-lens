package transcript

import "testing"

func TestStitchRemovesChineseOverlapAndKeepsBoundaryPunctuation(t *testing.T) {
	result := Stitch([]string{
		"今天我们介绍深度学习的基本概念",
		"深度学习的基本概念。首先看神经网络。",
	})
	if result.Content != "今天我们介绍深度学习的基本概念。首先看神经网络。" {
		t.Fatalf("Content = %q", result.Content)
	}
	if len(result.Boundaries) != 1 || result.Boundaries[0].Method != "exact_normalized_overlap" || result.Boundaries[0].MatchRunes == 0 {
		t.Fatalf("Boundaries = %+v", result.Boundaries)
	}
}

func TestStitchIgnoresWhitespaceCaseAndPunctuationDuringEnglishMatch(t *testing.T) {
	result := Stitch([]string{
		"We now explain Retrieval-Augmented Generation",
		"retrieval augmented generation, starting with embeddings.",
	})
	if result.Content != "We now explain Retrieval-Augmented Generation, starting with embeddings." {
		t.Fatalf("Content = %q", result.Content)
	}
}

func TestStitchUsesSafeLanguageAwareJoinWhenNoOverlapMatches(t *testing.T) {
	english := Stitch([]string{"first part", "second part"})
	if english.Content != "first part second part" || english.Boundaries[0].Method != "append" {
		t.Fatalf("English result = %+v", english)
	}
	chinese := Stitch([]string{"前半句话", "后半句话"})
	if chinese.Content != "前半句话后半句话" {
		t.Fatalf("Chinese content = %q", chinese.Content)
	}
}

func TestStitchDoesNotDeleteShortCoincidentalOverlap(t *testing.T) {
	result := Stitch([]string{"他说我们", "我们继续"})
	if result.Content != "他说我们我们继续" || result.Boundaries[0].MatchRunes != 0 {
		t.Fatalf("result = %+v", result)
	}
}

func TestStitchSkipsBlankParts(t *testing.T) {
	result := Stitch([]string{"  ", "唯一内容", ""})
	if result.Content != "唯一内容" || len(result.Boundaries) != 0 {
		t.Fatalf("result = %+v", result)
	}
}
