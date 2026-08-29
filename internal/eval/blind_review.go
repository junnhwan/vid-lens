package eval

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"math/rand"
	"sort"
	"strconv"
	"strings"
)

// BuildBlindReviewInputsFromArtifacts pairs two frozen run artifacts without
// exposing their variant identities in the eventual public review sheet.
// Retrieved contexts stay attached to the answer produced from them.
func BuildBlindReviewInputsFromArtifacts(baseline, candidate RunArtifact) ([]BlindReviewInput, error) {
	if strings.TrimSpace(baseline.Metadata.VariantID) == "" || strings.TrimSpace(candidate.Metadata.VariantID) == "" || baseline.Metadata.VariantID == candidate.Metadata.VariantID {
		return nil, fmt.Errorf("baseline and candidate must have distinct variant_id values")
	}
	for name, values := range map[string][2]string{
		"dataset version": {baseline.Metadata.DatasetVersion, candidate.Metadata.DatasetVersion},
		"dataset":         {baseline.Metadata.DatasetSHA256, candidate.Metadata.DatasetSHA256},
		"source manifest": {baseline.Metadata.SourceManifestSHA256, candidate.Metadata.SourceManifestSHA256},
		"corpus":          {baseline.Metadata.CorpusSHA256, candidate.Metadata.CorpusSHA256},
		"chunk manifest":  {baseline.Metadata.ChunkManifestSHA256, candidate.Metadata.ChunkManifestSHA256},
		"vector artifact": {baseline.Metadata.VectorArtifactSHA256, candidate.Metadata.VectorArtifactSHA256},
		"split":           {string(baseline.Metadata.Split), string(candidate.Metadata.Split)},
		"experiment":      {baseline.Metadata.ExperimentID, candidate.Metadata.ExperimentID},
	} {
		if strings.TrimSpace(values[0]) == "" || values[0] != values[1] {
			return nil, fmt.Errorf("baseline/candidate %s mismatch", name)
		}
	}
	baselineCases, err := indexCaseArtifacts(baseline.Cases)
	if err != nil {
		return nil, fmt.Errorf("baseline: %w", err)
	}
	candidateCases, err := indexCaseArtifacts(candidate.Cases)
	if err != nil {
		return nil, fmt.Errorf("candidate: %w", err)
	}
	if len(baselineCases) != len(candidateCases) {
		return nil, fmt.Errorf("baseline/candidate case set mismatch")
	}
	ids := make([]string, 0, len(baselineCases))
	for id := range baselineCases {
		if _, ok := candidateCases[id]; !ok {
			return nil, fmt.Errorf("baseline/candidate case set mismatch: candidate missing %q", id)
		}
		ids = append(ids, id)
	}
	sort.Strings(ids)

	inputs := make([]BlindReviewInput, 0, len(ids))
	for _, id := range ids {
		base := baselineCases[id].Result
		cand := candidateCases[id].Result
		if base.Failure != nil || cand.Failure != nil {
			return nil, fmt.Errorf("case %q has failed generation and cannot enter blind review", id)
		}
		if strings.TrimSpace(base.Response) == "" || strings.TrimSpace(cand.Response) == "" {
			return nil, fmt.Errorf("case %q missing generated response", id)
		}
		if base.Case.CaseID != cand.Case.CaseID || base.Case.VideoID != cand.Case.VideoID || base.Case.SourceGroup != cand.Case.SourceGroup || base.Case.Question != cand.Case.Question || base.Case.Answerable != cand.Case.Answerable {
			return nil, fmt.Errorf("case %q identity or question drift", id)
		}
		baseReference, err := blindReference(base.Case)
		if err != nil {
			return nil, fmt.Errorf("case %q: %w", id, err)
		}
		candidateReference, err := blindReference(cand.Case)
		if err != nil {
			return nil, fmt.Errorf("case %q candidate: %w", id, err)
		}
		if baseReference != candidateReference {
			return nil, fmt.Errorf("case %q reference drift", id)
		}
		inputs = append(inputs, BlindReviewInput{
			CaseID: id, SourceGroup: base.Case.SourceGroup, Category: base.Case.Category,
			Question: base.Case.Question, Reference: baseReference,
			Outputs: [2]BlindVariantOutput{
				{VariantID: baseline.Metadata.VariantID, Response: base.Response, Contexts: blindContextTexts(base.Retrieved)},
				{VariantID: candidate.Metadata.VariantID, Response: cand.Response, Contexts: blindContextTexts(cand.Retrieved)},
			},
		})
	}
	return inputs, nil
}

func blindReference(c Case) (string, error) {
	if !c.Answerable {
		return "UNANSWERABLE", nil
	}
	parts := make([]string, 0, len(c.AnswerPoints))
	for _, point := range c.AnswerPoints {
		if text := strings.TrimSpace(point.Text); text != "" {
			parts = append(parts, text)
		}
	}
	if len(parts) == 0 {
		return "", fmt.Errorf("answerable case has no reference answer points")
	}
	return strings.Join(parts, "\n"), nil
}

func blindContextTexts(contexts []RetrievedContext) []string {
	out := make([]string, 0, len(contexts))
	for _, context := range contexts {
		text := strings.TrimSpace(context.Text)
		if text != "" {
			out = append(out, text)
		}
	}
	return out
}

type BlindVariantOutput struct {
	VariantID string   `json:"variant_id"`
	Response  string   `json:"response"`
	Contexts  []string `json:"contexts"`
}

type BlindReviewInput struct {
	CaseID      string                `json:"case_id"`
	SourceGroup string                `json:"source_group"`
	Category    string                `json:"category"`
	Question    string                `json:"question"`
	Reference   string                `json:"reference"`
	Outputs     [2]BlindVariantOutput `json:"outputs"`
}

type BlindReviewRow struct {
	BlindID         string   `json:"blind_id"`
	CaseID          string   `json:"case_id"`
	SourceGroup     string   `json:"source_group"`
	Category        string   `json:"category"`
	Question        string   `json:"question"`
	Reference       string   `json:"reference"`
	ContextsA       []string `json:"contexts_a"`
	OutputA         string   `json:"output_a"`
	ContextsB       []string `json:"contexts_b"`
	OutputB         string   `json:"output_b"`
	HumanPreference string   `json:"human_preference,omitempty"`
	HumanNotes      string   `json:"human_notes,omitempty"`
}

type BlindReviewKeyEntry struct {
	BlindID  string `json:"blind_id"`
	CaseID   string `json:"case_id"`
	VariantA string `json:"variant_a"`
	VariantB string `json:"variant_b"`
}

type BlindReviewBatch struct {
	Rows []BlindReviewRow      `json:"rows"`
	Key  []BlindReviewKeyEntry `json:"key"`
}

func BuildBlindReviewBatch(inputs []BlindReviewInput, sampleSize int, seed int64) (BlindReviewBatch, error) {
	if sampleSize < 50 || sampleSize > 100 {
		return BlindReviewBatch{}, fmt.Errorf("sample_size must be between 50 and 100")
	}
	if len(inputs) < sampleSize {
		return BlindReviewBatch{}, fmt.Errorf("need at least %d cases, got %d", sampleSize, len(inputs))
	}
	seen := make(map[string]bool, len(inputs))
	for i, input := range inputs {
		if strings.TrimSpace(input.CaseID) == "" || strings.TrimSpace(input.SourceGroup) == "" || strings.TrimSpace(input.Question) == "" || strings.TrimSpace(input.Reference) == "" {
			return BlindReviewBatch{}, fmt.Errorf("inputs[%d] missing case/source/question/reference", i)
		}
		if seen[input.CaseID] {
			return BlindReviewBatch{}, fmt.Errorf("duplicate case_id %q", input.CaseID)
		}
		seen[input.CaseID] = true
		if strings.TrimSpace(input.Outputs[0].VariantID) == "" || strings.TrimSpace(input.Outputs[1].VariantID) == "" || input.Outputs[0].VariantID == input.Outputs[1].VariantID {
			return BlindReviewBatch{}, fmt.Errorf("case %q must contain two distinct variants", input.CaseID)
		}
		if strings.TrimSpace(input.Outputs[0].Response) == "" || strings.TrimSpace(input.Outputs[1].Response) == "" {
			return BlindReviewBatch{}, fmt.Errorf("case %q has empty response", input.CaseID)
		}
	}

	rng := rand.New(rand.NewSource(seed))
	selected := stratifiedBlindSample(inputs, sampleSize, rng)
	batch := BlindReviewBatch{Rows: make([]BlindReviewRow, 0, sampleSize), Key: make([]BlindReviewKeyEntry, 0, sampleSize)}
	for i, input := range selected {
		first, second := input.Outputs[0], input.Outputs[1]
		if rng.Intn(2) == 1 {
			first, second = second, first
		}
		blindID := fmt.Sprintf("blind-%03d", i+1)
		batch.Rows = append(batch.Rows, BlindReviewRow{
			BlindID: blindID, CaseID: input.CaseID, SourceGroup: input.SourceGroup, Category: input.Category,
			Question: input.Question, Reference: input.Reference, ContextsA: append([]string(nil), first.Contexts...), OutputA: first.Response,
			ContextsB: append([]string(nil), second.Contexts...), OutputB: second.Response,
		})
		batch.Key = append(batch.Key, BlindReviewKeyEntry{BlindID: blindID, CaseID: input.CaseID, VariantA: first.VariantID, VariantB: second.VariantID})
	}
	return batch, nil
}

func stratifiedBlindSample(inputs []BlindReviewInput, sampleSize int, rng *rand.Rand) []BlindReviewInput {
	groups := make(map[string][]BlindReviewInput)
	names := make([]string, 0)
	for _, input := range inputs {
		if _, ok := groups[input.SourceGroup]; !ok {
			names = append(names, input.SourceGroup)
		}
		groups[input.SourceGroup] = append(groups[input.SourceGroup], input)
	}
	sort.Strings(names)
	rng.Shuffle(len(names), func(i, j int) { names[i], names[j] = names[j], names[i] })
	for _, name := range names {
		values := groups[name]
		rng.Shuffle(len(values), func(i, j int) { values[i], values[j] = values[j], values[i] })
		groups[name] = values
	}
	positions := make(map[string]int, len(names))
	selected := make([]BlindReviewInput, 0, sampleSize)
	for len(selected) < sampleSize {
		progressed := false
		for _, name := range names {
			position := positions[name]
			if position >= len(groups[name]) {
				continue
			}
			selected = append(selected, groups[name][position])
			positions[name] = position + 1
			progressed = true
			if len(selected) == sampleSize {
				break
			}
		}
		if !progressed {
			break
		}
	}
	return selected
}

func WriteBlindReviewCSV(w io.Writer, rows []BlindReviewRow) error {
	writer := csv.NewWriter(w)
	if err := writer.Write([]string{"blind_id", "case_id", "source_group", "category", "question", "reference", "contexts_a", "output_a", "contexts_b", "output_b", "human_preference", "human_notes"}); err != nil {
		return err
	}
	for _, row := range rows {
		if err := writer.Write([]string{row.BlindID, row.CaseID, row.SourceGroup, row.Category, row.Question, row.Reference, strings.Join(row.ContextsA, "\n---\n"), row.OutputA, strings.Join(row.ContextsB, "\n---\n"), row.OutputB, row.HumanPreference, row.HumanNotes}); err != nil {
			return err
		}
	}
	writer.Flush()
	return writer.Error()
}

func WriteBlindReviewKeyJSON(w io.Writer, key []BlindReviewKeyEntry) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(key)
}

type BlindPreference string

const (
	PreferenceA   BlindPreference = "a"
	PreferenceB   BlindPreference = "b"
	PreferenceTie BlindPreference = "tie"
)

type BlindRating struct {
	BlindID    string          `json:"blind_id"`
	RaterID    string          `json:"rater_id"`
	Preference BlindPreference `json:"preference"`
	Notes      string          `json:"notes,omitempty"`
}

type CalibrationDisagreement struct {
	BlindID      string          `json:"blind_id"`
	CaseID       string          `json:"case_id"`
	Human        BlindPreference `json:"human"`
	Judge        BlindPreference `json:"judge"`
	HumanVariant string          `json:"human_variant"`
	JudgeVariant string          `json:"judge_variant"`
}

type CalibrationReport struct {
	Compared      int                       `json:"compared"`
	Agreements    int                       `json:"agreements"`
	AgreementRate float64                   `json:"agreement_rate"`
	CohensKappa   float64                   `json:"cohens_kappa"`
	HumanCounts   map[BlindPreference]int   `json:"human_counts"`
	JudgeCounts   map[BlindPreference]int   `json:"judge_counts"`
	Disagreements []CalibrationDisagreement `json:"disagreements"`
}

func CalibrateJudge(key []BlindReviewKeyEntry, humanRatings, judgeRatings []BlindRating) (CalibrationReport, error) {
	keys, err := indexBlindKeys(key)
	if err != nil {
		return CalibrationReport{}, err
	}
	human, err := indexBlindRatings(humanRatings, keys, "human")
	if err != nil {
		return CalibrationReport{}, err
	}
	judge, err := indexBlindRatings(judgeRatings, keys, "judge")
	if err != nil {
		return CalibrationReport{}, err
	}
	if len(human) != len(judge) {
		return CalibrationReport{}, fmt.Errorf("human and judge rating sets differ in size")
	}

	report := CalibrationReport{HumanCounts: make(map[BlindPreference]int), JudgeCounts: make(map[BlindPreference]int)}
	ids := make([]string, 0, len(human))
	for id := range human {
		if _, ok := judge[id]; !ok {
			return CalibrationReport{}, fmt.Errorf("judge rating missing blind_id %q", id)
		}
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		h, j := human[id], judge[id]
		report.Compared++
		report.HumanCounts[h.Preference]++
		report.JudgeCounts[j.Preference]++
		if h.Preference == j.Preference {
			report.Agreements++
			continue
		}
		entry := keys[id]
		report.Disagreements = append(report.Disagreements, CalibrationDisagreement{
			BlindID: id, CaseID: entry.CaseID, Human: h.Preference, Judge: j.Preference,
			HumanVariant: preferenceVariant(h.Preference, entry), JudgeVariant: preferenceVariant(j.Preference, entry),
		})
	}
	if report.Compared == 0 {
		return CalibrationReport{}, fmt.Errorf("no comparable ratings")
	}
	report.AgreementRate = float64(report.Agreements) / float64(report.Compared)
	expected := 0.0
	for _, preference := range []BlindPreference{PreferenceA, PreferenceB, PreferenceTie} {
		expected += float64(report.HumanCounts[preference]) / float64(report.Compared) * float64(report.JudgeCounts[preference]) / float64(report.Compared)
	}
	if math.Abs(1-expected) < 1e-12 {
		if report.AgreementRate == 1 {
			report.CohensKappa = 1
		}
	} else {
		report.CohensKappa = (report.AgreementRate - expected) / (1 - expected)
	}
	return report, nil
}

func indexBlindKeys(entries []BlindReviewKeyEntry) (map[string]BlindReviewKeyEntry, error) {
	out := make(map[string]BlindReviewKeyEntry, len(entries))
	for i, entry := range entries {
		if entry.BlindID == "" || entry.CaseID == "" || entry.VariantA == "" || entry.VariantB == "" || entry.VariantA == entry.VariantB {
			return nil, fmt.Errorf("key[%d] is invalid", i)
		}
		if _, exists := out[entry.BlindID]; exists {
			return nil, fmt.Errorf("duplicate blind_id %q in key", entry.BlindID)
		}
		out[entry.BlindID] = entry
	}
	return out, nil
}

func indexBlindRatings(ratings []BlindRating, keys map[string]BlindReviewKeyEntry, label string) (map[string]BlindRating, error) {
	out := make(map[string]BlindRating, len(ratings))
	for i, rating := range ratings {
		if _, ok := keys[rating.BlindID]; !ok {
			return nil, fmt.Errorf("%s rating[%d] references unknown blind_id %q", label, i, rating.BlindID)
		}
		if rating.RaterID == "" {
			return nil, fmt.Errorf("%s rating[%d] missing rater_id", label, i)
		}
		if rating.Preference != PreferenceA && rating.Preference != PreferenceB && rating.Preference != PreferenceTie {
			return nil, fmt.Errorf("%s rating[%d] has invalid preference %q", label, i, rating.Preference)
		}
		if _, exists := out[rating.BlindID]; exists {
			return nil, fmt.Errorf("duplicate %s rating for %q", label, rating.BlindID)
		}
		out[rating.BlindID] = rating
	}
	return out, nil
}

func preferenceVariant(preference BlindPreference, entry BlindReviewKeyEntry) string {
	switch preference {
	case PreferenceA:
		return entry.VariantA
	case PreferenceB:
		return entry.VariantB
	default:
		return string(PreferenceTie)
	}
}

func (r CalibrationReport) String() string {
	return "agreement=" + strconv.FormatFloat(r.AgreementRate, 'f', 4, 64) + " kappa=" + strconv.FormatFloat(r.CohensKappa, 'f', 4, 64)
}
