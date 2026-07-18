package service

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestExtractEvidencePrefersRelevantVerbatimSentence(t *testing.T) {
	anchor := strings.Repeat("这一句只是在介绍背景信息。", 10) +
		"工具执行完成后，调用结果会作为新消息反馈给模型，模型再决定下一步动作。" +
		strings.Repeat("这一句讨论的是其他内容。", 10)

	got := extractEvidence("工具调用结果怎样反馈给模型？", "工具调用结果 模型反馈", anchor, 120)
	if !strings.Contains(got, "调用结果会作为新消息反馈给模型") {
		t.Fatalf("evidence = %q, want relevant sentence", got)
	}
	if !strings.Contains(anchor, got) {
		t.Fatalf("evidence must be a verbatim substring of anchor: %q", got)
	}
	if utf8.RuneCountInString(got) > 120 {
		t.Fatalf("evidence runes = %d, want <= 120", utf8.RuneCountInString(got))
	}
}

func TestExtractEvidenceFallsBackToBoundedVerbatimWindow(t *testing.T) {
	anchor := strings.Repeat("没有标点的转写文本", 80)

	got := extractEvidence("完全不匹配的问题", "", anchor, 64)
	if got == "" {
		t.Fatal("evidence should not be empty")
	}
	if !strings.Contains(anchor, got) {
		t.Fatalf("evidence must be a verbatim substring of anchor: %q", got)
	}
	if utf8.RuneCountInString(got) > 64 {
		t.Fatalf("evidence runes = %d, want <= 64", utf8.RuneCountInString(got))
	}
}

func TestBuildCitationsUsesAnchorInsteadOfExpandedContext(t *testing.T) {
	anchor := strings.Repeat("背景句子。", 30) + "真正相关的证据是工具结果会反馈给模型。" + strings.Repeat("结尾句子。", 30)
	expanded := "只属于前一个邻居块的秘密内容。\n" + anchor + "\n只属于后一个邻居块的秘密内容。"

	citations := buildCitations("工具结果如何反馈？", []RetrievedChunk{{
		EvidenceID:      "evidence-1",
		ChunkID:         9,
		ChunkIndex:      3,
		Content:         expanded,
		AnchorContent:   anchor,
		Source:          RetrievalSourceHybrid,
		VectorRank:      1,
		KeywordRank:     2,
		RRFScore:        0.42,
		MatchedQuery:    "工具结果反馈",
		WindowTruncated: true,
	}})
	if len(citations) != 1 {
		t.Fatalf("citations = %#v, want one", citations)
	}
	got := citations[0]
	if got.CitationID != "C1" {
		t.Fatalf("citation id = %q, want C1", got.CitationID)
	}
	if !strings.Contains(got.Content, "工具结果会反馈给模型") {
		t.Fatalf("citation content = %q, want relevant evidence", got.Content)
	}
	if strings.Contains(got.Content, "邻居块的秘密内容") {
		t.Fatalf("citation leaked expanded neighbor context: %q", got.Content)
	}
	if !strings.Contains(anchor, got.Content) {
		t.Fatalf("citation must be verbatim anchor evidence: %q", got.Content)
	}
	if utf8.RuneCountInString(got.Content) > defaultCitationEvidenceRunes {
		t.Fatalf("citation runes = %d, want <= %d", utf8.RuneCountInString(got.Content), defaultCitationEvidenceRunes)
	}
}

func TestBuildCitationsSkipsEmptyEvidence(t *testing.T) {
	got := buildCitations("问题", []RetrievedChunk{{ChunkID: 1, Content: "   "}})
	if len(got) != 0 {
		t.Fatalf("citations = %#v, want empty", got)
	}
}

func TestSelectAnswerCitationsKeepsOnlyReferencedIDs(t *testing.T) {
	candidates := []Citation{
		{CitationID: "C1", Content: "evidence one"},
		{CitationID: "C2", Content: "evidence two"},
		{CitationID: "C3", Content: "evidence three"},
		{CitationID: "C4", Content: "evidence four"},
	}

	got := selectAnswerCitations("先说明第三点 [C3]，再说明第一点 [C1]，重复引用 [C3]。", candidates)
	if len(got) != 2 {
		t.Fatalf("selected citations = %#v, want C1 and C3 only", got)
	}
	if got[0].CitationID != "C1" || got[1].CitationID != "C3" {
		t.Fatalf("selected citation IDs = [%s %s], want [C1 C3]", got[0].CitationID, got[1].CitationID)
	}
}

func TestSelectAnswerCitationsFallsBackToTopOne(t *testing.T) {
	candidates := []Citation{
		{CitationID: "C1", Content: "evidence one"},
		{CitationID: "C2", Content: "evidence two"},
		{CitationID: "C3", Content: "evidence three"},
	}

	for _, answer := range []string{"模型没有输出引用编号", "模型输出了不存在的编号 [C9]"} {
		got := selectAnswerCitations(answer, candidates)
		if len(got) != 1 || got[0].CitationID != "C1" {
			t.Fatalf("selectAnswerCitations(%q) = %#v, want top one", answer, got)
		}
	}
}

func TestBuildCitationsKeepsDisplayedEvidenceWithin160Runes(t *testing.T) {
	anchor := "其实就是我们把要求发给agent，agent把我们的要求，还有刚刚我们看到的一大段沟通规则，发给大模型，大模型思考了一下，觉得要调用工具，于是返回要调用工具。agent看到要调用工具，于是去调用工具拿到结果，然后再次请求大模型，把工具调用结果告诉大模型，大模型再次陷入思考，进入一个循环，直到觉得不需要调用工具了，就返回最终答案。agent拿到答案之后不再需要调用工具，于是退出循环，把最终结果返回给用户，然后输出在页面上。"

	got := buildCitations("工具调用结果怎样反馈给模型？", []RetrievedChunk{{
		ChunkID: 271, ChunkIndex: 9, Content: anchor, MatchedQuery: "工具调用结果反馈模型",
	}})
	if len(got) != 1 {
		t.Fatalf("citations = %#v, want one", got)
	}
	if !strings.Contains(got[0].Content, "把工具调用结果告诉大模型") {
		t.Fatalf("evidence = %q, want the relevant verbatim statement", got[0].Content)
	}
	if runes := utf8.RuneCountInString(got[0].Content); runes > 160 {
		t.Fatalf("evidence runes = %d, want <= 160: %q", runes, got[0].Content)
	}
	if !strings.Contains(anchor, got[0].Content) {
		t.Fatalf("evidence must be a verbatim substring of anchor: %q", got[0].Content)
	}
}

func TestSelectAnswerCitationsRecognizesSupportedTokenForms(t *testing.T) {
	candidates := []Citation{
		{CitationID: "C1", Content: "evidence one"},
		{CitationID: "C2", Content: "evidence two"},
		{CitationID: "C3", Content: "evidence three"},
	}
	tests := []struct {
		name    string
		answer  string
		wantIDs []string
	}{
		{name: "single", answer: "结论 [C1]。", wantIDs: []string{"C1"}},
		{name: "adjacent", answer: "结论 [C1][C2]。", wantIDs: []string{"C1", "C2"}},
		{name: "ascii comma with space", answer: "结论 [C1, C2]。", wantIDs: []string{"C1", "C2"}},
		{name: "ascii comma", answer: "结论 [C1,C3]。", wantIDs: []string{"C1", "C3"}},
		{name: "enumeration comma", answer: "结论 [C1、C2]。", wantIDs: []string{"C1", "C2"}},
		{name: "lowercase and Chinese comma", answer: "结论 [c1， c3]。", wantIDs: []string{"C1", "C3"}},
		{name: "candidate order dedup and invalid exclusion", answer: "先引用 [C3, C9]，再引用 [C1][C3]。", wantIDs: []string{"C1", "C3"}},
		{name: "invalid only falls back", answer: "不存在 [C9]。", wantIDs: []string{"C1"}},
		{name: "no reference falls back", answer: "没有引用。", wantIDs: []string{"C1"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := selectAnswerCitations(tt.answer, candidates)
			if len(got) != len(tt.wantIDs) {
				t.Fatalf("selected citations = %#v, want IDs %v", got, tt.wantIDs)
			}
			for i, wantID := range tt.wantIDs {
				if got[i].CitationID != wantID {
					t.Fatalf("selected citation %d ID = %q, want %q (all: %#v)", i, got[i].CitationID, wantID, got)
				}
			}
		})
	}
}

func TestSelectAnswerCitationsFallbackIsAtMostTopOne(t *testing.T) {
	tests := []struct {
		name       string
		candidates []Citation
		wantIDs    []string
	}{
		{name: "none", candidates: nil, wantIDs: nil},
		{name: "one", candidates: []Citation{{CitationID: "C1"}}, wantIDs: []string{"C1"}},
		{name: "three", candidates: []Citation{{CitationID: "C1"}, {CitationID: "C2"}, {CitationID: "C3"}}, wantIDs: []string{"C1"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := selectAnswerCitations("没有有效引用 [C9]。", tt.candidates)
			if len(got) != len(tt.wantIDs) {
				t.Fatalf("selected citations = %#v, want IDs %v", got, tt.wantIDs)
			}
			for i, wantID := range tt.wantIDs {
				if got[i].CitationID != wantID {
					t.Fatalf("selected citation %d ID = %q, want %q", i, got[i].CitationID, wantID)
				}
			}
		})
	}
}

func TestFinalizeAnswerCitationsRecognizesAndRemovesSupportedTokens(t *testing.T) {
	candidates := []Citation{
		{CitationID: "C1", Content: "evidence one"},
		{CitationID: "C2", Content: "evidence two"},
		{CitationID: "C3", Content: "evidence three"},
	}
	tests := []struct {
		name    string
		token   string
		wantIDs []string
	}{
		{name: "single", token: "[C1]", wantIDs: []string{"C1"}},
		{name: "adjacent", token: "[C1][C2]", wantIDs: []string{"C1", "C2"}},
		{name: "ascii comma with space", token: "[C1, C2]", wantIDs: []string{"C1", "C2"}},
		{name: "ascii comma", token: "[C1,C3]", wantIDs: []string{"C1", "C3"}},
		{name: "enumeration comma", token: "[C1、C2]", wantIDs: []string{"C1", "C2"}},
		{name: "lowercase and Chinese comma", token: "[c1， c3]", wantIDs: []string{"C1", "C3"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := finalizeAnswerCitations("结论 "+tt.token+"。", candidates)
			if got.Answer != "结论。" {
				t.Fatalf("clean answer = %q, want %q", got.Answer, "结论。")
			}
			if len(got.Citations) != len(tt.wantIDs) {
				t.Fatalf("citations = %#v, want IDs %v", got.Citations, tt.wantIDs)
			}
			for i, wantID := range tt.wantIDs {
				if got.Citations[i].CitationID != wantID {
					t.Fatalf("citation %d ID = %q, want %q", i, got.Citations[i].CitationID, wantID)
				}
			}
		})
	}
}

func TestFinalizeAnswerCitationsKeepsCandidateOrderAndDeduplicates(t *testing.T) {
	candidates := []Citation{
		{CitationID: "C1", Content: "evidence one"},
		{CitationID: "C2", Content: "evidence two"},
		{CitationID: "C3", Content: "evidence three"},
	}

	got := finalizeAnswerCitations("先说第三条 [C3, C9]，再说第一条 [C1][C3]。", candidates)
	if got.Answer != "先说第三条，再说第一条。" {
		t.Fatalf("clean answer = %q", got.Answer)
	}
	if len(got.Citations) != 2 || got.Citations[0].CitationID != "C1" || got.Citations[1].CitationID != "C3" {
		t.Fatalf("citations = %#v, want C1 and C3 in candidate order", got.Citations)
	}
}

func TestFinalizeAnswerCitationsCleansAnswerWithoutRewritingOtherBrackets(t *testing.T) {
	candidates := []Citation{{CitationID: "C1"}, {CitationID: "C2"}, {CitationID: "C3"}, {CitationID: "C4"}}
	raw := "第一句 [C1, C3, C4]。\n第二句 [C1、C2]。\n\n\n\n框架 [Gin] 和 [链接](https://example.com) [C0] [C1 / C2] !"

	got := finalizeAnswerCitations(raw, candidates)
	want := "第一句。\n第二句。\n\n框架 [Gin] 和 [链接](https://example.com) [C0] [C1 / C2]!"
	if got.Answer != want {
		t.Fatalf("clean answer = %q, want %q", got.Answer, want)
	}
	if len(got.Citations) != 2 || got.Citations[0].CitationID != "C1" || got.Citations[1].CitationID != "C2" {
		t.Fatalf("citations = %#v, want top-ranked C1/C2 only", got.Citations)
	}
}

func TestFinalizeAnswerCitationsFallsBackWhenNoValidCandidateReferenced(t *testing.T) {
	candidates := []Citation{{CitationID: "C1"}, {CitationID: "C2"}, {CitationID: "C3"}}

	got := finalizeAnswerCitations("答案引用了不存在的证据 [C9]。", candidates)
	if got.Answer != "答案引用了不存在的证据。" {
		t.Fatalf("clean answer = %q", got.Answer)
	}
	if len(got.Citations) != 1 || got.Citations[0].CitationID != "C1" {
		t.Fatalf("citations = %#v, want C1-only fallback", got.Citations)
	}
}

func TestExtractEvidenceEndsAtUsefulBoundaryAfterRelevantPhrase(t *testing.T) {
	anchor := "背景句完整。关键证据说明完整结束。" + strings.Repeat("后续残片", 20) + "。"

	got := extractEvidence("关键证据是什么？", "关键证据", anchor, 48)
	want := "背景句完整。关键证据说明完整结束。"
	if got != want {
		t.Fatalf("evidence = %q, want %q", got, want)
	}
	if !strings.Contains(anchor, got) {
		t.Fatalf("evidence must be a verbatim substring of anchor: %q", got)
	}
	if utf8.RuneCountInString(got) > 48 {
		t.Fatalf("evidence runes = %d, want <= 48", utf8.RuneCountInString(got))
	}
}

func TestExtractEvidenceDoesNotTrimBeforeRelevantPhrase(t *testing.T) {
	anchor := "较早句子。" + strings.Repeat("前", 20) + "关键证据" + strings.Repeat("后", 40)

	got := extractEvidence("关键证据", "关键证据", anchor, 40)
	if !strings.Contains(got, "关键证据") {
		t.Fatalf("evidence lost the relevant phrase: %q", got)
	}
	if !strings.Contains(anchor, got) {
		t.Fatalf("evidence must be a verbatim substring of anchor: %q", got)
	}
	if utf8.RuneCountInString(got) > 40 {
		t.Fatalf("evidence runes = %d, want <= 40", utf8.RuneCountInString(got))
	}
}

func TestSelectAnswerCitationsFallbackSkipsInvalidAndDuplicateCandidates(t *testing.T) {
	candidates := []Citation{
		{CitationID: "", Content: "blank"},
		{CitationID: "C0", Content: "zero"},
		{CitationID: " c2 ", Content: "first C2"},
		{CitationID: "c2", Content: "duplicate C2"},
		{CitationID: "C01", Content: "leading zero"},
		{CitationID: "C3", Content: "C3"},
		{CitationID: "not-a-citation", Content: "invalid"},
		{CitationID: "C4", Content: "beyond fallback limit"},
	}

	got := selectAnswerCitations("没有引用", candidates)
	if len(got) != 1 {
		t.Fatalf("fallback citations = %#v, want first valid unique candidate", got)
	}
	if got[0].CitationID != "C2" || got[0].Content != "first C2" {
		t.Fatalf("first fallback = %#v, want normalized first valid C2", got[0])
	}
}

func TestFinalizeAnswerCitationsPreservesNestedBracketsAndMarkdownLinks(t *testing.T) {
	candidates := []Citation{
		{CitationID: "C1"},
		{CitationID: "C2"},
		{CitationID: "C3"},
	}
	raw := "嵌套 [[C1]]；说明 [说明 [C1]]；链接 [C3](https://example.com)；普通 [Gin]；文档 [说明](doc.md)；有效 [C2]。"

	got := finalizeAnswerCitations(raw, candidates)
	want := "嵌套 [[C1]]；说明 [说明 [C1]]；链接 [C3](https://example.com)；普通 [Gin]；文档 [说明](doc.md)；有效。"
	if got.Answer != want {
		t.Fatalf("clean answer = %q, want %q", got.Answer, want)
	}
	if len(got.Citations) != 1 || got.Citations[0].CitationID != "C2" {
		t.Fatalf("citations = %#v, want only top-level non-link C2", got.Citations)
	}
}

func TestFinalizeAnswerCitationsPreservesEscapedMarkdownBrackets(t *testing.T) {
	candidates := []Citation{{CitationID: "C1"}, {CitationID: "C2"}}

	got := finalizeAnswerCitations(`字面量 \[C1]，有效 [C2]。`, candidates)
	if got.Answer != `字面量 \[C1]，有效。` {
		t.Fatalf("clean answer = %q", got.Answer)
	}
	if len(got.Citations) != 1 || got.Citations[0].CitationID != "C2" {
		t.Fatalf("citations = %#v, want only C2", got.Citations)
	}
}

func TestFinalizeAnswerCitationsPreservesMarkdownCitationBoundaries(t *testing.T) {
	candidates := []Citation{{CitationID: "C1"}, {CitationID: "C2"}}
	raw := strings.Join([]string{
		`转义字面量 \[C1]。`,
		`引用式链接 [C1][source]。`,
		`[C1]: https://example.com`,
		`引用目标 [来源][C1]。`,
		`普通链接 [C1](https://example.com)。`,
		`真正内部标记[C1][C2]和[C1,C2]。`,
	}, "\n")

	got := finalizeAnswerCitations(raw, candidates)
	want := strings.Join([]string{
		`转义字面量 \[C1]。`,
		`引用式链接 [C1][source]。`,
		`[C1]: https://example.com`,
		`引用目标 [来源][C1]。`,
		`普通链接 [C1](https://example.com)。`,
		`真正内部标记和。`,
	}, "\n")
	if got.Answer != want {
		t.Fatalf("clean answer = %q, want %q", got.Answer, want)
	}
	if len(got.Citations) != 2 || got.Citations[0].CitationID != "C1" || got.Citations[1].CitationID != "C2" {
		t.Fatalf("citations = %#v, want internal C1/C2 only", got.Citations)
	}
}

func TestExtractEvidenceShortAnchorEndsAfterRelevantSentence(t *testing.T) {
	anchor := "开场背景。关键证据在这里。后面只是无关尾巴"

	got := extractEvidence("关键证据是什么？", "关键证据", anchor, 80)
	want := "开场背景。关键证据在这里。"
	if got != want {
		t.Fatalf("evidence = %q, want %q", got, want)
	}
	if !strings.Contains(anchor, got) {
		t.Fatalf("evidence must be a verbatim substring of anchor: %q", got)
	}
}

func TestExtractEvidenceHardTruncationPreservesBoundaryPunctuationExactly(t *testing.T) {
	anchor := "  甲乙丙丁戊己庚，辛壬癸  "

	got := extractEvidence("完全不匹配", "", anchor, 8)
	want := "甲乙丙丁戊己庚，"
	if got != want {
		t.Fatalf("hard-truncated evidence = %q, want exact source window %q", got, want)
	}
	if !strings.Contains(anchor, got) {
		t.Fatalf("evidence must be a verbatim substring of anchor: %q", got)
	}
	if utf8.RuneCountInString(got) != 8 {
		t.Fatalf("evidence runes = %d, want exact hard bound 8", utf8.RuneCountInString(got))
	}
}

func TestFinalizeAnswerCitationsPreservesMarkdownCodeRegionsExactly(t *testing.T) {
	candidates := []Citation{
		{CitationID: "C1"},
		{CitationID: "C2"},
		{CitationID: "C3"},
	}
	inline := "`[C1]  ,  inline`"
	multiBacktick := "`` `[C2]`  !  multi ``"
	fenced := "```go\n[C2]  ,  fenced\n\n\nvalue  !  stays\n```\n"
	raw := "普通 [Gin] 与 [链接](doc.md)\n\n\n\n代码：" + inline +
		"\n多反引号：" + multiBacktick +
		"\n围栏：\n" + fenced +
		"外部结论 [C3] 。"
	want := "普通 [Gin] 与 [链接](doc.md)\n\n代码：" + inline +
		"\n多反引号：" + multiBacktick +
		"\n围栏：\n" + fenced +
		"外部结论。"

	got := finalizeAnswerCitations(raw, candidates)
	if got.Answer != want {
		t.Fatalf("clean answer = %q, want exact Markdown %q", got.Answer, want)
	}
	for _, code := range []string{inline, multiBacktick, fenced} {
		if !strings.Contains(got.Answer, code) {
			t.Fatalf("Markdown code region was not preserved byte-for-byte: %q in %q", code, got.Answer)
		}
	}
	if len(got.Citations) != 1 || got.Citations[0].CitationID != "C3" {
		t.Fatalf("citations = %#v, want only outside citation C3", got.Citations)
	}
}

func TestExtractEvidenceKeepsRelevantPhraseAcrossChunkBoundary(t *testing.T) {
	anchor := "甲甲甲甲甲甲关键证据乙乙乙乙"

	got := extractEvidence("关键证据", "关键证据", anchor, 8)

	if !strings.Contains(got, "关键证据") {
		t.Fatalf("evidence lost cross-boundary relevant phrase: %q", got)
	}
	if utf8.RuneCountInString(got) > 8 {
		t.Fatalf("evidence runes = %d, want <= 8", utf8.RuneCountInString(got))
	}
	if !strings.Contains(anchor, got) {
		t.Fatalf("evidence must be a verbatim substring of anchor: %q", got)
	}
}

func TestFinalizeAnswerCitationsPreservesAdditionalMarkdownBlockCode(t *testing.T) {
	candidates := []Citation{
		{CitationID: "C1"},
		{CitationID: "C2"},
		{CitationID: "C3"},
	}
	tests := []struct {
		name string
		code string
	}{
		{
			name: "tilde fenced code",
			code: "~~~text\n[C1]  ,  fenced\n\n\nvalue  !  stays\n~~~\n",
		},
		{
			name: "indented code with blank lines",
			code: "    [C2]  ,  indented\n\n\n    value  !  stays\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw := "普通 [Gin] 与 [链接](doc.md)\n代码：\n\n" + tt.code + "外部结论 [C3] 。"
			got := finalizeAnswerCitations(raw, candidates)

			if !strings.Contains(got.Answer, tt.code) {
				t.Fatalf("Markdown code was not preserved byte-for-byte:\n got %q\nwant code %q", got.Answer, tt.code)
			}
			if strings.Contains(got.Answer, "外部结论 [C3]") {
				t.Fatalf("outside citation token was not removed: %q", got.Answer)
			}
			if !strings.Contains(got.Answer, "普通 [Gin] 与 [链接](doc.md)") {
				t.Fatalf("ordinary Markdown was changed: %q", got.Answer)
			}
			if len(got.Citations) != 1 || got.Citations[0].CitationID != "C3" {
				t.Fatalf("citations = %#v, want only outside citation C3", got.Citations)
			}
		})
	}
}

func TestFinalizeAnswerCitationsDoesNotTreatIndentedParagraphContinuationAsCode(t *testing.T) {
	candidates := []Citation{{CitationID: "C1"}, {CitationID: "C2"}}
	raw := "普通段落\n    [C1]  ,  仍是段落\n外部 [C2]。"

	got := finalizeAnswerCitations(raw, candidates)

	if strings.Contains(got.Answer, "[C1]") {
		t.Fatalf("indented paragraph continuation was incorrectly protected: %q", got.Answer)
	}
	if len(got.Citations) != 2 || got.Citations[0].CitationID != "C1" || got.Citations[1].CitationID != "C2" {
		t.Fatalf("citations = %#v, want paragraph citations C1 and C2", got.Citations)
	}
}

func TestFinalizeAnswerCitationsPreservesFencesInsideMarkdownContainers(t *testing.T) {
	candidates := []Citation{{CitationID: "C1"}, {CitationID: "C2"}, {CitationID: "C3"}}
	tests := []struct {
		name string
		code string
	}{
		{
			name: "unclosed blockquote fence",
			code: "> ```text\n> [C1]  ,  code\n>\n>\n> value  !  stays",
		},
		{
			name: "unclosed list fence",
			code: "- ~~~text\n  [C2]  ,  code\n\n\n  value  !  stays",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw := "外部结论 [C3]。\n\n" + tt.code
			got := finalizeAnswerCitations(raw, candidates)

			if !strings.HasSuffix(got.Answer, tt.code) {
				t.Fatalf("container code was not preserved byte-for-byte:\n got %q\nwant suffix %q", got.Answer, tt.code)
			}
			if len(got.Citations) != 1 || got.Citations[0].CitationID != "C3" {
				t.Fatalf("citations = %#v, want only outside C3", got.Citations)
			}
		})
	}
}

func TestFinalizeAnswerCitationsInlineBackticksDoNotCrossBlockCode(t *testing.T) {
	candidates := []Citation{{CitationID: "C1"}, {CitationID: "C3"}}
	fenced := "```text\n[C1]  ,  code\n```\n"
	raw := "`unclosed inline\n" + fenced + "外部结论 [C3]  ! `"

	got := finalizeAnswerCitations(raw, candidates)

	if !strings.Contains(got.Answer, fenced) {
		t.Fatalf("fenced code was skipped by inline matching: %q", got.Answer)
	}
	if strings.Contains(got.Answer, "[C3]") {
		t.Fatalf("outside citation was incorrectly protected by cross-block inline range: %q", got.Answer)
	}
	if len(got.Citations) != 1 || got.Citations[0].CitationID != "C3" {
		t.Fatalf("citations = %#v, want only outside C3", got.Citations)
	}
}
