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
		{ExecutionContractV12, true, false, ExecutionContractV12},
		{ExecutionContractV10, true, true, ExecutionContractV9},
		{ExecutionContractV9, false, false, ExecutionContractV9},
	} {
		if got := QuestionContract(test.article, test.workflow, test.external); got != test.want {
			t.Fatalf("question contract %s/%v/%v=%s want %s", test.article, test.workflow, test.external, got, test.want)
		}
	}
}
