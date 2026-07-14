package eval

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type ArtifactPaths struct {
	Directory      string
	MetadataJSON   string
	CasesJSONL     string
	SummaryJSON    string
	SummaryCSV     string
	ReportMarkdown string
}

func WriteArtifacts(outputRoot string, artifact RunArtifact) (ArtifactPaths, error) {
	if strings.TrimSpace(outputRoot) == "" {
		return ArtifactPaths{}, fmt.Errorf("output directory is required")
	}
	if strings.TrimSpace(artifact.Metadata.RunID) == "" {
		return ArtifactPaths{}, fmt.Errorf("run_id is required")
	}
	runDir := filepath.Join(outputRoot, artifact.Metadata.RunID)
	if err := os.Mkdir(runDir, 0o755); err != nil {
		if os.IsExist(err) {
			return ArtifactPaths{}, fmt.Errorf("run directory already exists: %s", runDir)
		}
		return ArtifactPaths{}, err
	}
	paths := ArtifactPaths{
		Directory:      runDir,
		MetadataJSON:   filepath.Join(runDir, "run-metadata.json"),
		CasesJSONL:     filepath.Join(runDir, "cases.jsonl"),
		SummaryJSON:    filepath.Join(runDir, "summary.json"),
		SummaryCSV:     filepath.Join(runDir, "summary.csv"),
		ReportMarkdown: filepath.Join(runDir, "report.md"),
	}
	if err := writeJSON(paths.MetadataJSON, artifact.Metadata); err != nil {
		return paths, err
	}
	if err := writeJSONL(paths.CasesJSONL, artifact.Cases); err != nil {
		return paths, err
	}
	if err := writeJSON(paths.SummaryJSON, struct {
		Summary  MetricReport        `json:"summary"`
		Analysis *ExperimentAnalysis `json:"analysis,omitempty"`
	}{Summary: artifact.Summary, Analysis: artifact.Analysis}); err != nil {
		return paths, err
	}
	if err := writeSummaryCSV(paths.SummaryCSV, artifact); err != nil {
		return paths, err
	}
	if err := os.WriteFile(paths.ReportMarkdown, []byte(RenderMarkdownReport(artifact)), 0o600); err != nil {
		return paths, err
	}
	return paths, nil
}

func writeJSON(path string, value any) error {
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(raw, '\n'), 0o600)
}

func writeJSONL(path string, cases []CaseArtifact) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()
	encoder := json.NewEncoder(file)
	encoder.SetEscapeHTML(false)
	for _, c := range cases {
		if err := encoder.Encode(c); err != nil {
			return err
		}
	}
	return nil
}

func writeSummaryCSV(path string, artifact RunArtifact) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()
	writer := csv.NewWriter(file)
	defer writer.Flush()
	_ = writer.Write([]string{"scope", "key", "cases", "evaluable_cases", "failed_cases", "failure_rate", "recall_at_k", "mrr", "ndcg_at_k", "context_precision_at_k", "complete_evidence_recall", "answerability_f1"})
	write := func(scope, key string, metric MetricResult) {
		_ = writer.Write([]string{scope, key, strconv.Itoa(metric.Cases), strconv.Itoa(metric.EvaluableCases), strconv.Itoa(metric.FailedCases), formatFloat(metric.FailureRate), formatFloat(metric.RecallAtK), formatFloat(metric.MRR), formatFloat(metric.NDCGAtK), formatFloat(metric.ContextPrecisionAtK), formatFloat(metric.CompleteEvidenceRecall), formatFloat(metric.AnswerabilityF1)})
	}
	write("overall", "all", artifact.Summary.Overall)
	writeMetricMap := func(scope string, values map[string]MetricResult) {
		keys := sortedMetricKeys(values)
		for _, key := range keys {
			write(scope, key, values[key])
		}
	}
	writeMetricMap("category", artifact.Summary.ByCategory)
	writeMetricMap("video", artifact.Summary.ByVideo)
	writeMetricMap("source_group", artifact.Summary.BySourceGroup)
	return writer.Error()
}

func RenderMarkdownReport(artifact RunArtifact) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# RAG Evaluation Run %s\n\n", artifact.Metadata.RunID)
	fmt.Fprintf(&b, "- Commit: `%s`\n- Dataset: `%s` (`%s`)\n- Split: `%s`\n- Experiment: `%s` / `%s`\n", artifact.Metadata.Commit, artifact.Metadata.DatasetVersion, artifact.Metadata.DatasetSHA256, artifact.Metadata.Split, artifact.Metadata.ExperimentID, artifact.Metadata.VariantID)
	fmt.Fprintf(&b, "- Corpus/chunk/vector/config hashes: `%s` / `%s` / `%s` / `%s`\n\n", artifact.Metadata.CorpusSHA256, artifact.Metadata.ChunkManifestSHA256, artifact.Metadata.VectorArtifactSHA256, artifact.Metadata.ConfigSHA256)
	b.WriteString("## Overall metrics\n\n")
	renderMetricTable(&b, map[string]MetricResult{"all": artifact.Summary.Overall})
	b.WriteString("\n## Per-video metrics\n\n")
	renderMetricTable(&b, artifact.Summary.ByVideo)
	b.WriteString("\n## Per-source-group metrics\n\n")
	renderMetricTable(&b, artifact.Summary.BySourceGroup)
	if artifact.Analysis != nil {
		b.WriteString("\n## Preregistered experiment analysis\n\n")
		fmt.Fprintf(&b, "- Status: **%s**\n- Primary metric: `%s`\n- Paired effect: %.6f\n- 95%% cluster CI: [%.6f, %.6f]\n", strings.ToUpper(string(artifact.Analysis.Status)), artifact.Analysis.PrimaryMetric, artifact.Analysis.Bootstrap.ObservedEffect, artifact.Analysis.Bootstrap.Lower, artifact.Analysis.Bootstrap.Upper)
		if len(artifact.Analysis.FailureReasons) > 0 {
			b.WriteString("- Failure reasons:\n")
			for _, reason := range artifact.Analysis.FailureReasons {
				fmt.Fprintf(&b, "  - %s\n", reason)
			}
		}
		b.WriteString("\n### Per-video paired effects\n\n")
		for _, key := range sortedFloatKeys(artifact.Analysis.PerVideoEffects) {
			fmt.Fprintf(&b, "- `%s`: %.6f\n", key, artifact.Analysis.PerVideoEffects[key])
		}
	}
	b.WriteString("\n## Failed cases\n\n")
	failed := 0
	for _, c := range artifact.Cases {
		if c.Result.Failure == nil {
			continue
		}
		failed++
		fmt.Fprintf(&b, "- `%s`: stage=`%s`, code=`%s`, message=%s\n", c.CaseID, c.Result.Failure.Stage, c.Result.Failure.Code, c.Result.Failure.Message)
	}
	if failed == 0 {
		b.WriteString("None.\n")
	}
	b.WriteString("\n## Case trace\n\n")
	for _, c := range artifact.Cases {
		fmt.Fprintf(&b, "- `%s`: recall=%.3f, rr=%.3f, ndcg=%.3f\n", c.CaseID, c.Metric.RecallAtK, c.Metric.ReciprocalRank, c.Metric.NDCGAtK)
	}
	return b.String()
}

func renderMetricTable(b *strings.Builder, values map[string]MetricResult) {
	b.WriteString("| Key | Cases | Failed | Failure Rate | Recall@K | MRR | nDCG@K | Context Precision@K | Complete Evidence Recall | Answerability F1 |\n")
	b.WriteString("| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |\n")
	for _, key := range sortedMetricKeys(values) {
		m := values[key]
		fmt.Fprintf(b, "| %s | %d | %d | %.3f | %.3f | %.3f | %.3f | %.3f | %.3f | %.3f |\n", key, m.Cases, m.FailedCases, m.FailureRate, m.RecallAtK, m.MRR, m.NDCGAtK, m.ContextPrecisionAtK, m.CompleteEvidenceRecall, m.AnswerabilityF1)
	}
}

func sortedMetricKeys(values map[string]MetricResult) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func formatFloat(value float64) string { return strconv.FormatFloat(value, 'f', 9, 64) }
