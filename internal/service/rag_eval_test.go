package service

import (
	"context"
	"testing"
	"time"
)

func TestRAGEvalComputesRecallMRRAndSourceMix(t *testing.T) {
	report := EvaluateRAGRetrieval([]RAGEvalCaseResult{
		{
			Case: RAGEvalCase{
				Question:              "为什么分布式锁释放时要校验 owner？",
				ExpectedChunkKeywords: []string{"owner", "锁"},
			},
			Citations: []RetrievedChunk{
				{ChunkID: 1, ChunkIndex: 0, Source: RetrievalSourceVector, Content: "普通背景片段"},
				{ChunkID: 2, ChunkIndex: 1, Source: RetrievalSourceHybrid, Content: "释放分布式锁前必须校验 owner，避免删掉别人的锁"},
			},
			Duration: 15 * time.Millisecond,
		},
		{
			Case: RAGEvalCase{
				Question:              "视频怎么解释 token bucket？",
				ExpectedChunkKeywords: []string{"token bucket"},
			},
			Citations: nil,
			Duration:  5 * time.Millisecond,
		},
	}, 2)

	if report.TotalCases != 2 {
		t.Fatalf("total cases = %d, want 2", report.TotalCases)
	}
	if report.HitCases != 1 {
		t.Fatalf("hit cases = %d, want 1", report.HitCases)
	}
	if report.RecallAtK != 0.5 {
		t.Fatalf("recall@k = %.3f, want 0.5", report.RecallAtK)
	}
	if report.MRR != 0.25 {
		t.Fatalf("mrr = %.3f, want 0.25", report.MRR)
	}
	if report.NoResultRate != 0.5 {
		t.Fatalf("no result rate = %.3f, want 0.5", report.NoResultRate)
	}
	if report.AvgLatencyMs != 10 {
		t.Fatalf("avg latency ms = %.3f, want 10", report.AvgLatencyMs)
	}
	if report.SourceCounts[RetrievalSourceVector] != 1 || report.SourceCounts[RetrievalSourceHybrid] != 1 {
		t.Fatalf("source counts = %#v, want vector=1 hybrid=1", report.SourceCounts)
	}
	if len(report.Cases) != 2 || !report.Cases[0].Hit || report.Cases[0].FirstHitRank != 2 {
		t.Fatalf("first case result = %+v, want hit at rank 2", report.Cases)
	}
	if report.Cases[1].Hit || report.Cases[1].FirstHitRank != 0 {
		t.Fatalf("second case result = %+v, want no hit", report.Cases[1])
	}
}

func TestRAGEvalTreatsEmptyExpectedKeywordsAsSkipped(t *testing.T) {
	report := EvaluateRAGRetrieval([]RAGEvalCaseResult{
		{
			Case: RAGEvalCase{Question: "没有期望关键词的样例"},
			Citations: []RetrievedChunk{
				{ChunkID: 1, Content: "任意片段", Source: RetrievalSourceKeyword},
			},
		},
	}, 3)

	if report.TotalCases != 1 {
		t.Fatalf("total cases = %d, want 1", report.TotalCases)
	}
	if report.EvaluableCases != 0 {
		t.Fatalf("evaluable cases = %d, want 0", report.EvaluableCases)
	}
	if report.RecallAtK != 0 || report.MRR != 0 {
		t.Fatalf("metrics = recall %.3f mrr %.3f, want zero when no evaluable cases", report.RecallAtK, report.MRR)
	}
	if !report.Cases[0].Skipped {
		t.Fatalf("case = %+v, want skipped", report.Cases[0])
	}
}

func TestRAGEvalRunUsesRetrieverAndPreservesNoResultCases(t *testing.T) {
	cases := []RAGEvalCase{
		{
			Question:              "owner 校验在哪里讲到？",
			ExpectedChunkKeywords: []string{"owner"},
		},
		{
			Question:              "令牌桶在哪里讲到？",
			ExpectedChunkKeywords: []string{"令牌桶"},
		},
	}
	calls := 0
	report, err := RunRAGEval(t.Context(), cases, 3, func(_ context.Context, _ RAGEvalCase, _ int) ([]RetrievedChunk, error) {
		calls++
		if calls == 1 {
			return []RetrievedChunk{{ChunkID: 1, Content: "释放锁前校验 owner", Source: RetrievalSourceHybrid}}, nil
		}
		return nil, nil
	})
	if err != nil {
		t.Fatalf("RunRAGEval() error = %v", err)
	}
	if calls != 2 {
		t.Fatalf("retriever calls = %d, want 2", calls)
	}
	if report.TotalCases != 2 || report.HitCases != 1 || report.NoResultRate != 0.5 {
		t.Fatalf("report = %+v, want one hit and one no-result case", report)
	}
	if report.Cases[0].LatencyMs < 0 || report.Cases[1].LatencyMs < 0 {
		t.Fatalf("latency should be recorded: %+v", report.Cases)
	}
}
