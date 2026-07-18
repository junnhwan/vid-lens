package main

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

func renderMarkdown(opts evalOptions, taskIDs []int64, caseCount int, embeddingModel string, topK, candidateK int, results []modeResult) string {
	return renderMarkdownWithAgentAnswerEval(opts, taskIDs, caseCount, embeddingModel, topK, candidateK, results, nil)
}

func renderMarkdownWithAgentAnswerEval(opts evalOptions, taskIDs []int64, caseCount int, embeddingModel string, topK, candidateK int, results []modeResult, answerResults []answerModeResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# VidLens Resume Quantification Results\n\n")
	fmt.Fprintf(&b, "## RAG Retrieval A/B Evaluation\n\n")
	fmt.Fprintf(&b, "- Date: %s\n", time.Now().Format("2006-01-02"))
	fmt.Fprintf(&b, "- Environment: %s\n", opts.environment)
	fmt.Fprintf(&b, "- Code commit: %s\n", opts.commit)
	fmt.Fprintf(&b, "- Task IDs: %s\n", formatInt64s(taskIDs))
	fmt.Fprintf(&b, "- Case count: %d\n", caseCount)
	fmt.Fprintf(&b, "- Embedding model: %s\n", embeddingModel)
	fmt.Fprintf(&b, "- TopK: %d\n", topK)
	fmt.Fprintf(&b, "- CandidateK: %d\n", candidateK)
	fmt.Fprintf(&b, "- Latency note: retrieval latency excludes the shared query embedding API call.\n\n")
	fmt.Fprintf(&b, "| Mode | Recall@%d | MRR | No Result Rate | Avg Retrieval Latency | Rewrite Fallback Rate | Avg Expanded Context | Rerank Changed Rank Count | Citation Context Hit Rate | Expanded Context Hit Rate |\n", topK)
	fmt.Fprintf(&b, "| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |\n")
	for _, result := range results {
		fmt.Fprintf(&b, "| %s | %s | %.3f | %s | %.2f ms | %s | %.1f chars | %d | %s | %s |\n",
			result.mode,
			formatPercent(result.report.RecallAtK),
			result.report.MRR,
			formatPercent(result.report.NoResultRate),
			result.report.AvgLatencyMs,
			formatPercent(result.report.RewriteFallbackRate),
			result.report.AvgExpandedContextChars,
			result.report.RerankChangedRankCount,
			formatPercent(result.report.CitationContextHitRate),
			formatPercent(result.report.ExpandedContextHitRate),
		)
	}
	fmt.Fprintf(&b, "\n")
	renderCategoryMetrics(&b, topK, results)
	fmt.Fprintf(&b, "\n")
	renderAgentAnswerEvaluation(&b, answerResults)
	if len(results) >= 2 {
		base := results[0].report
		hybrid, hasHybrid := findModeResult(results, "Vector + BM25 + RRF")
		modelRerank, hasModelRerank := findModeResult(results, "Rewrite + MultiQuery + RRF + Window + Model Rerank")
		fmt.Fprintf(&b, "Conclusion:\n")
		if hasHybrid && (hybrid.report.RecallAtK > base.RecallAtK || hybrid.report.MRR > base.MRR) && hybrid.report.NoResultRate <= base.NoResultRate {
			fmt.Fprintf(&b, "BM25+RRF %s and %s. This supports a cautious claim about hybrid retrieval improving retrieval ranking on this self-built case set, not a broad claim about answer accuracy or production RAG quality.\n",
				recallComparisonText(topK, base.RecallAtK, hybrid.report.RecallAtK),
				mrrComparisonText(base.MRR, hybrid.report.MRR))
			if hasModelRerank {
				if modelRerank.report.RecallAtK > base.RecallAtK || modelRerank.report.MRR > base.MRR {
					fmt.Fprintf(&b, "Model Rerank changed ranking in this run; only claim it if the category metrics justify the specific scenario.\n\n")
				} else {
					fmt.Fprintf(&b, "Model Rerank did not improve ranking in this run; 不要写 model rerank 提升检索排名的简历 claim，建议默认关闭或仅作为后续可选优化继续评估。\n\n")
				}
			} else {
				fmt.Fprintf(&b, "\n")
			}
			fmt.Fprintf(&b, "Resume sentence:\n")
			fmt.Fprintf(&b, "设计并实现 VidLens 视频 RAG 检索评测框架，支持 vector-only、BM25+RRF、query rewrite、多查询召回、相邻片段回填和 rerank 多模式对比；在自建 %d 条视频 QA case 上，BM25+RRF %s，%s，但 model rerank 本轮未证明排序收益，因此不作为简历提升 claim。\n\n",
				caseCount,
				recallResumeText(topK, base.RecallAtK, hybrid.report.RecallAtK),
				mrrResumeText(base.MRR, hybrid.report.MRR))
		} else {
			fmt.Fprintf(&b, "On this small self-built video QA evaluation set, the RAG 2.0 modes did not produce a safer aggregate improvement over vector-only retrieval. Do not write a resume claim about retrieval improvement from this run.\n\n")
			fmt.Fprintf(&b, "Resume sentence:\n")
			fmt.Fprintf(&b, "设计并实现 VidLens 视频 RAG 检索评测框架，支持 vector-only、BM25+RRF、query rewrite、多查询召回、相邻片段回填和 rerank 多模式对比；通过自建 %d 条视频 QA case 记录 Recall@%d、MRR、无结果率和检索延迟，为后续优化提供可量化依据。\n\n", caseCount, topK)
		}
	}
	for _, result := range results {
		fmt.Fprintf(&b, "### %s Case Details\n\n", result.mode)
		fmt.Fprintf(&b, "| # | Hit | First Hit Rank | Result Count | Latency |\n")
		fmt.Fprintf(&b, "| ---: | --- | ---: | ---: | ---: |\n")
		for i, c := range result.report.Cases {
			rank := "-"
			if c.FirstHitRank > 0 {
				rank = fmt.Sprintf("%d", c.FirstHitRank)
			}
			fmt.Fprintf(&b, "| %d | %t | %s | %d | %.2f ms |\n", i+1, c.Hit, rank, c.ResultCount, c.LatencyMs)
		}
		fmt.Fprintf(&b, "\nSource counts: %s\n\n", formatSourceCounts(result.report.SourceCounts))
	}
	return b.String()
}

func findModeResult(results []modeResult, mode string) (modeResult, bool) {
	for _, result := range results {
		if result.mode == mode {
			return result, true
		}
	}
	return modeResult{}, false
}

func renderCategoryMetrics(b *strings.Builder, topK int, results []modeResult) {
	categories := make([]string, 0)
	seen := make(map[string]bool)
	for _, result := range results {
		for category := range result.report.Categories {
			if !seen[category] {
				seen[category] = true
				categories = append(categories, category)
			}
		}
	}
	if len(categories) == 0 {
		return
	}
	sort.Strings(categories)
	fmt.Fprintf(b, "### Per-Category Metrics\n\n")
	fmt.Fprintf(b, "| Mode | Category | Cases | Recall@%d | MRR | No Result Rate | Avg Retrieval Latency | Rewrite Fallback Rate | Avg Expanded Context | Citation Context Hit Rate | Expanded Context Hit Rate | Rerank Changed Rank Count |\n", topK)
	fmt.Fprintf(b, "| --- | --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |\n")
	for _, result := range results {
		for _, category := range categories {
			categoryReport, ok := result.report.Categories[category]
			if !ok {
				continue
			}
			fmt.Fprintf(b, "| %s | %s | %d | %s | %.3f | %s | %.2f ms | %s | %.1f chars | %s | %s | %d |\n",
				result.mode,
				category,
				categoryReport.TotalCases,
				formatPercent(categoryReport.RecallAtK),
				categoryReport.MRR,
				formatPercent(categoryReport.NoResultRate),
				categoryReport.AvgLatencyMs,
				formatPercent(categoryReport.RewriteFallbackRate),
				categoryReport.AvgExpandedContextChars,
				formatPercent(categoryReport.CitationContextHitRate),
				formatPercent(categoryReport.ExpandedContextHitRate),
				categoryReport.RerankChangedRankCount,
			)
		}
	}
}

func renderAgentAnswerEvaluation(b *strings.Builder, answerResults []answerModeResult) {
	if len(answerResults) == 0 {
		return
	}
	fmt.Fprintf(b, "## Agent Answer Evaluation\n\n")
	fmt.Fprintf(b, "| Mode | Answer Point Coverage | Citation Hit Rate | No Answer Rate | Avg Tool Steps | Fallback/Error Rate | Avg Latency |\n")
	fmt.Fprintf(b, "| --- | ---: | ---: | ---: | ---: | ---: | ---: |\n")
	for _, result := range answerResults {
		fmt.Fprintf(b, "| %s | %s | %s | %s | %.1f | %s | %.2f ms |\n",
			result.mode,
			formatPercent(result.report.AnswerPointCoverage),
			formatPercent(result.report.CitationHitRate),
			formatPercent(result.report.NoAnswerRate),
			result.report.AvgToolSteps,
			formatPercent(result.report.FallbackErrorRate),
			result.report.AvgLatencyMs,
		)
	}
	fmt.Fprintf(b, "\n")
	ordinary, hasOrdinary := findAnswerModeResult(answerResults, "Ordinary RAG answer")
	agentic, hasAgentic := findAnswerModeResult(answerResults, "Agentic answer")
	if hasOrdinary && hasAgentic {
		if agentic.report.AnswerPointCoverage > ordinary.report.AnswerPointCoverage &&
			agentic.report.FallbackErrorRate <= ordinary.report.FallbackErrorRate {
			fmt.Fprintf(b, "Agentic answer improved deterministic answer-point coverage from %s to %s. Treat this as local eval evidence, not a production benchmark or broad answer-accuracy claim.\n\n",
				formatPercent(ordinary.report.AnswerPointCoverage),
				formatPercent(agentic.report.AnswerPointCoverage))
		} else {
			fmt.Fprintf(b, "Agentic answer eval did not prove a safer answer-point coverage improvement over ordinary RAG in this run. Do not claim answer accuracy improvement from Agentic QA without stronger eval evidence.\n\n")
		}
	}
}

func findAnswerModeResult(results []answerModeResult, mode string) (answerModeResult, bool) {
	for _, result := range results {
		if result.mode == mode {
			return result, true
		}
	}
	return answerModeResult{}, false
}

func recallComparisonText(topK int, base, opt float64) string {
	if opt > base {
		return fmt.Sprintf("improved Recall@%d from %s to %s", topK, formatPercent(base), formatPercent(opt))
	}
	if opt == base {
		return fmt.Sprintf("kept Recall@%d at %s", topK, formatPercent(opt))
	}
	return fmt.Sprintf("changed Recall@%d from %s to %s", topK, formatPercent(base), formatPercent(opt))
}

func mrrComparisonText(base, opt float64) string {
	if opt > base {
		return fmt.Sprintf("improved MRR from %.3f to %.3f", base, opt)
	}
	if opt == base {
		return fmt.Sprintf("kept MRR at %.3f", opt)
	}
	return fmt.Sprintf("changed MRR from %.3f to %.3f", base, opt)
}

func recallResumeText(topK int, base, opt float64) string {
	if opt > base {
		return fmt.Sprintf("将 Recall@%d 从 %s 提升至 %s", topK, formatPercent(base), formatPercent(opt))
	}
	if opt == base {
		return fmt.Sprintf("Recall@%d 均为 %s", topK, formatPercent(opt))
	}
	return fmt.Sprintf("Recall@%d 从 %s 变为 %s", topK, formatPercent(base), formatPercent(opt))
}

func mrrResumeText(base, opt float64) string {
	if opt > base {
		return fmt.Sprintf("MRR 从 %.3f 提升至 %.3f", base, opt)
	}
	if opt == base {
		return fmt.Sprintf("MRR 均为 %.3f", opt)
	}
	return fmt.Sprintf("MRR 从 %.3f 变为 %.3f", base, opt)
}

func formatPercent(v float64) string {
	return fmt.Sprintf("%.1f%%", v*100)
}

func formatInt64s(values []int64) string {
	parts := make([]string, 0, len(values))
	for _, value := range values {
		parts = append(parts, fmt.Sprintf("%d", value))
	}
	return strings.Join(parts, ", ")
}

func formatSourceCounts(counts map[string]int) string {
	if len(counts) == 0 {
		return "(none)"
	}
	keys := make([]string, 0, len(counts))
	for key := range counts {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf("%s=%d", key, counts[key]))
	}
	return strings.Join(parts, ", ")
}

func parentDir(path string) string {
	idx := strings.LastIndexAny(path, `/\`)
	if idx < 0 {
		return "."
	}
	return path[:idx]
}
