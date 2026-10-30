package domain

import "testing"

func TestArticleWriterDoesNotChooseQuestionAdapter(t *testing.T) {
	for _, test := range []struct {
		article            string
		workflow, external bool
		want               string
	}{
		{ArticleLoopV1, false, false, ExecutionContractV9},
		{ArticleLoopV1, true, false, ExecutionContractV9},
		{ArticleLoopV1, false, true, ExecutionContractV9},
		{ArticleLoopV2, false, false, ExecutionContractV9},
		{ArticleLoopV2, true, false, ExecutionContractV9},
		{ArticleLoopV2, true, true, ExecutionContractV9},
		{ExecutionContractV12, true, false, ExecutionContractV9},
		{ExecutionContractV10, true, true, ExecutionContractV9},
		{ExecutionContractV9, false, false, ExecutionContractV9},
	} {
		if got := QuestionContract(test.article, test.workflow, test.external); got != test.want {
			t.Fatalf("question contract %s/%v/%v=%s want %s", test.article, test.workflow, test.external, got, test.want)
		}
	}
}

func TestArticleVisualObservationCoverage(t *testing.T) {
	scope := ArticleReviewScope{FrameIDs: []string{"pixel"}}
	valid := ArticleVisualObservation{FrameID: "pixel", VisibleFacts: []string{"合成黑色画面"}, Uncertainty: []string{"没有可辨认的文字"}}
	if err := ValidateArticleVisualObservations([]ArticleVisualObservation{valid}, scope); err != nil {
		t.Fatal(err)
	}
	for _, observations := range [][]ArticleVisualObservation{
		nil, {valid, valid}, {{FrameID: "other", VisibleFacts: valid.VisibleFacts}},
		{{FrameID: "pixel"}}, {{FrameID: "pixel", VisibleFacts: []string{" "}}},
		{{FrameID: "pixel", VisibleFacts: valid.VisibleFacts, Uncertainty: []string{" "}}},
	} {
		if ValidateArticleVisualObservations(observations, scope) == nil {
			t.Fatalf("accepted missing/duplicate/undelivered/empty pixel observations: %+v", observations)
		}
	}
	if err := ValidateArticleVisualObservations(nil, ArticleReviewScope{Global: true}); err != nil {
		t.Fatal("global relations-only scope acquired nonexistent pixel obligations", err)
	}
	if ArticlePolicyIdentityForContract(GenerationSpec{}, ArticleLoopV2) == ArticlePolicyIdentity(GenerationSpec{}) {
		t.Fatal("new review policy reused legacy approval identity")
	}
}
