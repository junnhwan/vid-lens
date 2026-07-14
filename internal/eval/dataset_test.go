package eval

import (
	"strings"
	"testing"
)

func TestDatasetLegacyModeAcceptsExistingCaseList(t *testing.T) {
	raw := []byte(`
- task_id: 5
  task_hint: sample
  category: keyword_exact
  question: Which show mentions Avatar?
  expected_chunk_keywords: [Avatar, four nations]
  expected_answer_points: [Avatar is mentioned.]
`)

	dataset, err := LoadDataset(raw, LoadOptions{Mode: LoadModeLegacy})
	if err != nil {
		t.Fatalf("LoadDataset() legacy error = %v", err)
	}
	if !dataset.Legacy || len(dataset.Cases) != 1 {
		t.Fatalf("dataset = %+v, want one legacy case", dataset)
	}
	if dataset.Cases[0].TaskID != 5 || dataset.Cases[0].Question != "Which show mentions Avatar?" {
		t.Fatalf("legacy case = %+v", dataset.Cases[0])
	}
}

func TestDatasetStrictModeRequiresExplicitVersion(t *testing.T) {
	raw := mustStrictDatasetYAML(t, validStrictDataset(t))

	_, err := LoadDataset(raw, LoadOptions{Mode: LoadModeStrict})
	if err == nil || !strings.Contains(err.Error(), "dataset version") {
		t.Fatalf("LoadDataset() error = %v, want explicit dataset version error", err)
	}
}

func TestDatasetStrictModeRejectsLegacyFieldsWithoutIdentity(t *testing.T) {
	raw := []byte(`
- task_id: 5
  question: Which show mentions Avatar?
  expected_chunk_keywords: [Avatar]
`)

	_, err := LoadDataset(raw, LoadOptions{Mode: LoadModeStrict, DatasetVersion: "rag-v1"})
	if err == nil {
		t.Fatal("LoadDataset() error = nil, want strict schema rejection")
	}
	for _, field := range []string{"case_id", "video_id", "source_group", "split"} {
		if !strings.Contains(err.Error(), field) {
			t.Fatalf("LoadDataset() error = %q, want missing %s", err, field)
		}
	}
}

func TestDatasetRejectsSourceGroupAcrossSplits(t *testing.T) {
	dataset := validStrictDataset(t)
	dataset.Manifest.Splits[SplitDev] = SplitDefinition{
		Sources: []SourceGroup{{ID: "series-train", VideoIDs: []string{"video-dev"}}},
	}
	dataset.Manifest.SHA256 = mustManifestHash(t, dataset.DatasetVersion, dataset.Manifest.Splits)

	err := ValidateDataset(dataset, ValidationOptions{ExpectedVersion: dataset.DatasetVersion})
	if err == nil || !strings.Contains(err.Error(), "source_group") || !strings.Contains(err.Error(), "multiple splits") {
		t.Fatalf("ValidateDataset() error = %v, want source-group leakage error", err)
	}
}

func TestDatasetRejectsVideoAcrossSplits(t *testing.T) {
	dataset := validStrictDataset(t)
	dataset.Manifest.Splits[SplitDev] = SplitDefinition{
		Sources: []SourceGroup{{ID: "series-dev", VideoIDs: []string{"video-train"}}},
	}
	dataset.Manifest.SHA256 = mustManifestHash(t, dataset.DatasetVersion, dataset.Manifest.Splits)

	err := ValidateDataset(dataset, ValidationOptions{ExpectedVersion: dataset.DatasetVersion})
	if err == nil || !strings.Contains(err.Error(), "video_id") || !strings.Contains(err.Error(), "multiple splits") {
		t.Fatalf("ValidateDataset() error = %v, want video leakage error", err)
	}
}

func TestDatasetRejectsDuplicateCaseID(t *testing.T) {
	dataset := validStrictDataset(t)
	duplicate := dataset.Cases[0]
	duplicate.VideoID = "video-dev"
	duplicate.SourceGroup = "series-dev"
	duplicate.Split = SplitDev
	dataset.Cases = append(dataset.Cases, duplicate)

	err := ValidateDataset(dataset, ValidationOptions{ExpectedVersion: dataset.DatasetVersion})
	if err == nil || !strings.Contains(err.Error(), "duplicate case_id") {
		t.Fatalf("ValidateDataset() error = %v, want duplicate case ID error", err)
	}
}

func TestDatasetRejectsAnswerableCaseWithoutEvidence(t *testing.T) {
	dataset := validStrictDataset(t)
	dataset.Cases[0].EvidenceRanges = nil

	err := ValidateDataset(dataset, ValidationOptions{ExpectedVersion: dataset.DatasetVersion})
	if err == nil || !strings.Contains(err.Error(), "evidence_ranges") {
		t.Fatalf("ValidateDataset() error = %v, want missing evidence error", err)
	}
}

func TestDatasetRejectsUnsealedTestManifest(t *testing.T) {
	dataset := validStrictDataset(t)
	testSplit := dataset.Manifest.Splits[SplitTest]
	testSplit.Sealed = false
	testSplit.ContentSHA256 = ""
	testSplit.AccessTokenSHA256 = ""
	dataset.Manifest.Splits[SplitTest] = testSplit

	err := ValidateDataset(dataset, ValidationOptions{ExpectedVersion: dataset.DatasetVersion})
	if err == nil || !strings.Contains(err.Error(), "test split") || !strings.Contains(err.Error(), "sealed") {
		t.Fatalf("ValidateDataset() error = %v, want unsealed test error", err)
	}
}

func TestDatasetRejectsManifestOrSealedContentHashMismatch(t *testing.T) {
	t.Run("source manifest", func(t *testing.T) {
		dataset := validStrictDataset(t)
		dataset.Manifest.SHA256 = strings.Repeat("0", 64)
		err := ValidateDataset(dataset, ValidationOptions{ExpectedVersion: dataset.DatasetVersion})
		if err == nil || !strings.Contains(err.Error(), "manifest sha256") {
			t.Fatalf("ValidateDataset() error = %v, want manifest hash mismatch", err)
		}
	})

	t.Run("sealed test content", func(t *testing.T) {
		dataset := validStrictDataset(t)
		testSplit := dataset.Manifest.Splits[SplitTest]
		testSplit.ContentSHA256 = strings.Repeat("0", 64)
		dataset.Manifest.Splits[SplitTest] = testSplit
		err := ValidateDataset(dataset, ValidationOptions{ExpectedVersion: dataset.DatasetVersion})
		if err == nil || !strings.Contains(err.Error(), "content sha256") {
			t.Fatalf("ValidateDataset() error = %v, want sealed content hash mismatch", err)
		}
	})
}

func TestDatasetSourceManifestHashIsCanonical(t *testing.T) {
	splitsA := map[Split]SplitDefinition{
		SplitTrain: {Sources: []SourceGroup{{ID: "b", VideoIDs: []string{"v2", "v1"}}, {ID: "a", VideoIDs: []string{"v3"}}}},
		SplitDev:   {Sources: []SourceGroup{{ID: "c", VideoIDs: []string{"v4"}}}},
		SplitTest:  {Sources: []SourceGroup{{ID: "d", VideoIDs: []string{"v5"}}}, Sealed: true},
	}
	splitsB := map[Split]SplitDefinition{
		SplitTest:  {Sources: []SourceGroup{{ID: "d", VideoIDs: []string{"v5"}}}, Sealed: false, ContentSHA256: "ignored"},
		SplitTrain: {Sources: []SourceGroup{{ID: "a", VideoIDs: []string{"v3"}}, {ID: "b", VideoIDs: []string{"v1", "v2"}}}},
		SplitDev:   {Sources: []SourceGroup{{ID: "c", VideoIDs: []string{"v4"}}}},
	}

	hashA, err := ComputeManifestSHA256("rag-v1", splitsA)
	if err != nil {
		t.Fatalf("ComputeManifestSHA256(A) error = %v", err)
	}
	hashB, err := ComputeManifestSHA256("rag-v1", splitsB)
	if err != nil {
		t.Fatalf("ComputeManifestSHA256(B) error = %v", err)
	}
	if hashA != hashB {
		t.Fatalf("canonical hashes differ: %s != %s", hashA, hashB)
	}
}

func validStrictDataset(t *testing.T) Dataset {
	t.Helper()
	dataset := Dataset{
		SchemaVersion:  "1",
		DatasetVersion: "rag-v1",
		Manifest: SplitManifest{Splits: map[Split]SplitDefinition{
			SplitTrain: {Sources: []SourceGroup{{ID: "series-train", VideoIDs: []string{"video-train"}}}},
			SplitDev:   {Sources: []SourceGroup{{ID: "series-dev", VideoIDs: []string{"video-dev"}}}},
			SplitTest:  {Sources: []SourceGroup{{ID: "series-test", VideoIDs: []string{"video-test"}}}},
		}},
		Cases: []Case{
			{
				CaseID: "rag-train-001", VideoID: "video-train", SourceGroup: "series-train", Split: SplitTrain,
				Question: "What was introduced?", Category: "direct_fact", Difficulty: "medium", Answerable: true,
				AnswerPoints:   []AnswerPoint{{ID: "ap-1", Text: "A strict evaluation baseline.", Required: true}},
				EvidenceRanges: []EvidenceRange{{ID: "ev-1", GroupID: "g-1", StartMS: 1_000, EndMS: 5_000, Source: EvidenceSourceASR, Relevance: 3}},
			},
			{
				CaseID: "rag-test-001", VideoID: "video-test", SourceGroup: "series-test", Split: SplitTest,
				Question: "Which holdout fact is present?", Category: "direct_fact", Difficulty: "hard", Answerable: true,
				AnswerPoints:   []AnswerPoint{{ID: "ap-1", Text: "A sealed fact.", Required: true}},
				EvidenceRanges: []EvidenceRange{{ID: "ev-1", GroupID: "g-1", StartMS: 10_000, EndMS: 12_000, Source: EvidenceSourceASR, Relevance: 3}},
			},
		},
	}
	manifestHash, err := ComputeManifestSHA256(dataset.DatasetVersion, dataset.Manifest.Splits)
	if err != nil {
		t.Fatalf("ComputeManifestSHA256() error = %v", err)
	}
	dataset.Manifest.SHA256 = manifestHash
	if err := SealSplit(&dataset, SplitTest, "test-only-token"); err != nil {
		t.Fatalf("SealSplit() error = %v", err)
	}
	return dataset
}

func mustStrictDatasetYAML(t *testing.T, dataset Dataset) []byte {
	t.Helper()
	raw, err := MarshalDatasetYAML(dataset)
	if err != nil {
		t.Fatalf("MarshalDatasetYAML() error = %v", err)
	}
	return raw
}

func mustManifestHash(t *testing.T, version string, splits map[Split]SplitDefinition) string {
	t.Helper()
	hash, err := ComputeManifestSHA256(version, splits)
	if err != nil {
		t.Fatalf("ComputeManifestSHA256() error = %v", err)
	}
	return hash
}

func TestDatasetRejectsUnsupportedSchemaVersionCategoryAndDifficulty(t *testing.T) {
	for _, tt := range []struct {
		name string
		edit func(*Dataset)
		want string
	}{
		{name: "schema version", edit: func(d *Dataset) { d.SchemaVersion = "2" }, want: "schema_version"},
		{name: "missing category", edit: func(d *Dataset) { d.Cases[0].Category = " " }, want: "category"},
		{name: "invalid difficulty", edit: func(d *Dataset) { d.Cases[0].Difficulty = "extreme" }, want: "difficulty"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dataset := validStrictDataset(t)
			tt.edit(&dataset)
			err := ValidateDataset(dataset, ValidationOptions{ExpectedVersion: dataset.DatasetVersion})
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("ValidateDataset() error = %v, want %q validation", err, tt.want)
			}
		})
	}
}

func TestDatasetRejectsInvalidOrDuplicateAnswerPoints(t *testing.T) {
	for _, tt := range []struct {
		name string
		edit func(*Case)
		want string
	}{
		{name: "missing id", edit: func(c *Case) { c.AnswerPoints[0].ID = " " }, want: "answer_points[0] missing id"},
		{name: "missing text", edit: func(c *Case) { c.AnswerPoints[0].Text = " " }, want: "answer_points[0] missing text"},
		{name: "duplicate id", edit: func(c *Case) {
			c.AnswerPoints = append(c.AnswerPoints, AnswerPoint{ID: c.AnswerPoints[0].ID, Text: "another", Required: true})
		}, want: "duplicate answer point id"},
		{name: "no required point", edit: func(c *Case) { c.AnswerPoints[0].Required = false }, want: "required answer point"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dataset := validStrictDataset(t)
			tt.edit(&dataset.Cases[0])
			err := ValidateDataset(dataset, ValidationOptions{ExpectedVersion: dataset.DatasetVersion})
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("ValidateDataset() error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestDatasetRejectsDuplicateEvidenceIDsWithinCase(t *testing.T) {
	dataset := validStrictDataset(t)
	dataset.Cases[0].NegativeConfusers = []EvidenceRange{{
		ID:      dataset.Cases[0].EvidenceRanges[0].ID,
		StartMS: 20_000, EndMS: 21_000, Source: EvidenceSourceASR,
	}}

	err := ValidateDataset(dataset, ValidationOptions{ExpectedVersion: dataset.DatasetVersion})
	if err == nil || !strings.Contains(err.Error(), "duplicate evidence id") {
		t.Fatalf("ValidateDataset() error = %v, want duplicate evidence id", err)
	}
}

func TestDatasetRejectsDuplicateManifestEntries(t *testing.T) {
	for _, tt := range []struct {
		name string
		edit func(*Dataset)
		want string
	}{
		{name: "source group", edit: func(d *Dataset) {
			train := d.Manifest.Splits[SplitTrain]
			train.Sources = append(train.Sources, SourceGroup{ID: "series-train", VideoIDs: []string{"video-extra"}})
			d.Manifest.Splits[SplitTrain] = train
		}, want: "duplicate source_group"},
		{name: "video in same source", edit: func(d *Dataset) {
			train := d.Manifest.Splits[SplitTrain]
			train.Sources[0].VideoIDs = append(train.Sources[0].VideoIDs, "video-train")
			d.Manifest.Splits[SplitTrain] = train
		}, want: "duplicate video_id"},
		{name: "video in another source of same split", edit: func(d *Dataset) {
			train := d.Manifest.Splits[SplitTrain]
			train.Sources = append(train.Sources, SourceGroup{ID: "series-extra", VideoIDs: []string{"video-train"}})
			d.Manifest.Splits[SplitTrain] = train
		}, want: "duplicate video_id"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dataset := validStrictDataset(t)
			tt.edit(&dataset)
			dataset.Manifest.SHA256 = mustManifestHash(t, dataset.DatasetVersion, dataset.Manifest.Splits)
			err := ValidateDataset(dataset, ValidationOptions{ExpectedVersion: dataset.DatasetVersion})
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("ValidateDataset() error = %v, want %q", err, tt.want)
			}
		})
	}
}
