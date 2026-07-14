package eval

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

type LoadMode string

const (
	LoadModeLegacy LoadMode = "legacy"
	LoadModeStrict LoadMode = "strict"
)

type Split string

const (
	SplitTrain Split = "train"
	SplitDev   Split = "dev"
	SplitTest  Split = "test"
)

type EvidenceSource string

const (
	EvidenceSourceASR  EvidenceSource = "asr"
	EvidenceSourceOCR  EvidenceSource = "ocr"
	EvidenceSourceBoth EvidenceSource = "both"
)

type LoadOptions struct {
	Mode           LoadMode
	DatasetVersion string
}

type Dataset struct {
	SchemaVersion          string        `json:"schema_version" yaml:"schema_version"`
	DatasetVersion         string        `json:"dataset_version" yaml:"dataset_version"`
	Manifest               SplitManifest `json:"manifest" yaml:"manifest"`
	Cases                  []Case        `json:"cases" yaml:"cases"`
	Legacy                 bool          `json:"legacy,omitempty" yaml:"-"`
	loadedSplit            Split
	sealedAccessRegistered bool
}

func (d Dataset) LoadedSplit() (Split, bool) {
	return d.loadedSplit, validSplit(d.loadedSplit)
}

type SplitManifest struct {
	SHA256 string                    `json:"sha256" yaml:"sha256"`
	Splits map[Split]SplitDefinition `json:"splits" yaml:"splits"`
}

type SplitDefinition struct {
	Sources           []SourceGroup `json:"sources" yaml:"sources"`
	Sealed            bool          `json:"sealed,omitempty" yaml:"sealed,omitempty"`
	ContentSHA256     string        `json:"content_sha256,omitempty" yaml:"content_sha256,omitempty"`
	AccessTokenSHA256 string        `json:"access_token_sha256,omitempty" yaml:"access_token_sha256,omitempty"`
}

type SourceGroup struct {
	ID       string   `json:"source_group" yaml:"source_group"`
	VideoIDs []string `json:"video_ids" yaml:"video_ids"`
}

type Case struct {
	CaseID      string `json:"case_id,omitempty" yaml:"case_id,omitempty"`
	VideoID     string `json:"video_id,omitempty" yaml:"video_id,omitempty"`
	SourceGroup string `json:"source_group,omitempty" yaml:"source_group,omitempty"`
	Split       Split  `json:"split,omitempty" yaml:"split,omitempty"`

	TaskID     int64  `json:"task_id,omitempty" yaml:"task_id,omitempty"`
	TaskHint   string `json:"task_hint,omitempty" yaml:"task_hint,omitempty"`
	Question   string `json:"question" yaml:"question"`
	Category   string `json:"category,omitempty" yaml:"category,omitempty"`
	Difficulty string `json:"difficulty,omitempty" yaml:"difficulty,omitempty"`
	Answerable bool   `json:"answerable" yaml:"answerable"`

	AnswerPoints      []AnswerPoint   `json:"answer_points,omitempty" yaml:"answer_points,omitempty"`
	EvidenceRanges    []EvidenceRange `json:"evidence_ranges,omitempty" yaml:"evidence_ranges,omitempty"`
	NegativeConfusers []EvidenceRange `json:"negative_confusers,omitempty" yaml:"negative_confusers,omitempty"`
	Notes             string          `json:"notes,omitempty" yaml:"notes,omitempty"`

	ExpectedChunkKeywords []string `json:"expected_chunk_keywords,omitempty" yaml:"expected_chunk_keywords,omitempty"`
	ExpectedAnswerPoints  []string `json:"expected_answer_points,omitempty" yaml:"expected_answer_points,omitempty"`
}

type AnswerPoint struct {
	ID       string `json:"id" yaml:"id"`
	Text     string `json:"text" yaml:"text"`
	Required bool   `json:"required" yaml:"required"`
}

type EvidenceRange struct {
	ID         string         `json:"id" yaml:"id"`
	GroupID    string         `json:"group_id" yaml:"group_id"`
	StartMS    int64          `json:"start_ms,omitempty" yaml:"start_ms,omitempty"`
	EndMS      int64          `json:"end_ms,omitempty" yaml:"end_ms,omitempty"`
	ContextIDs []string       `json:"context_ids,omitempty" yaml:"context_ids,omitempty"`
	Source     EvidenceSource `json:"source" yaml:"source"`
	Relevance  int            `json:"relevance" yaml:"relevance"`
}

type ValidationOptions struct {
	ExpectedVersion string
}

func LoadDataset(raw []byte, opts LoadOptions) (Dataset, error) {
	mode := opts.Mode
	if mode == "" {
		mode = LoadModeLegacy
	}
	switch mode {
	case LoadModeLegacy:
		var cases []Case
		if err := yaml.Unmarshal(raw, &cases); err != nil {
			return Dataset{}, fmt.Errorf("parse legacy dataset: %w", err)
		}
		for i, c := range cases {
			if c.TaskID <= 0 {
				return Dataset{}, fmt.Errorf("case %d missing task_id", i+1)
			}
			if strings.TrimSpace(c.Question) == "" {
				return Dataset{}, fmt.Errorf("case %d missing question", i+1)
			}
			if len(c.ExpectedChunkKeywords) == 0 {
				return Dataset{}, fmt.Errorf("case %d missing expected_chunk_keywords", i+1)
			}
		}
		return Dataset{SchemaVersion: "legacy", Legacy: true, Cases: cases}, nil
	case LoadModeStrict:
		if strings.TrimSpace(opts.DatasetVersion) == "" {
			return Dataset{}, fmt.Errorf("strict mode requires explicit dataset version")
		}
		var root yaml.Node
		if err := yaml.Unmarshal(raw, &root); err != nil {
			return Dataset{}, fmt.Errorf("parse strict dataset: %w", err)
		}
		if isYAMLSequence(root) {
			return Dataset{}, fmt.Errorf("strict case missing required fields: case_id, video_id, source_group, split")
		}
		var dataset Dataset
		if err := yaml.Unmarshal(raw, &dataset); err != nil {
			return Dataset{}, fmt.Errorf("parse strict dataset: %w", err)
		}
		if err := ValidateDataset(dataset, ValidationOptions{ExpectedVersion: opts.DatasetVersion}); err != nil {
			return Dataset{}, err
		}
		return dataset, nil
	default:
		return Dataset{}, fmt.Errorf("unsupported load mode %q", mode)
	}
}

func MarshalDatasetYAML(dataset Dataset) ([]byte, error) {
	raw, err := yaml.Marshal(dataset)
	if err != nil {
		return nil, fmt.Errorf("marshal dataset: %w", err)
	}
	return raw, nil
}

func ValidateDataset(dataset Dataset, opts ValidationOptions) error {
	var problems []string
	if dataset.loadedSplit != "" && !validSplit(dataset.loadedSplit) {
		problems = append(problems, fmt.Sprintf("invalid loaded split %q", dataset.loadedSplit))
	}
	if dataset.SchemaVersion != "1" {
		problems = append(problems, fmt.Sprintf("schema_version must be %q", "1"))
	}
	if strings.TrimSpace(dataset.DatasetVersion) == "" {
		problems = append(problems, "missing dataset_version")
	}
	if opts.ExpectedVersion != "" && dataset.DatasetVersion != opts.ExpectedVersion {
		problems = append(problems, fmt.Sprintf("dataset version %q does not match requested %q", dataset.DatasetVersion, opts.ExpectedVersion))
	}
	for _, split := range []Split{SplitTrain, SplitDev, SplitTest} {
		if _, ok := dataset.Manifest.Splits[split]; !ok {
			problems = append(problems, fmt.Sprintf("manifest missing %s split", split))
		}
	}
	for split := range dataset.Manifest.Splits {
		if !validSplit(split) {
			problems = append(problems, fmt.Sprintf("manifest contains invalid split %q", split))
		}
	}

	sourceSplits := make(map[string]Split)
	seenSources := make(map[string]struct{})
	videoSplits := make(map[string]Split)
	seenVideos := make(map[string]string)
	sourceVideos := make(map[string]map[string]struct{})
	for _, split := range []Split{SplitTrain, SplitDev, SplitTest} {
		definition := dataset.Manifest.Splits[split]
		if definition.ContentSHA256 != "" {
			if err := ValidateSHA256Digest(fmt.Sprintf("%s content sha256", split), definition.ContentSHA256); err != nil {
				problems = append(problems, err.Error())
			}
		}
		for _, source := range definition.Sources {
			sourceID := strings.TrimSpace(source.ID)
			if sourceID == "" {
				problems = append(problems, fmt.Sprintf("%s split contains empty source_group", split))
				continue
			}
			if _, exists := seenSources[sourceID]; exists {
				problems = append(problems, fmt.Sprintf("duplicate source_group %q in manifest", sourceID))
			}
			seenSources[sourceID] = struct{}{}
			if prior, ok := sourceSplits[sourceID]; ok && prior != split {
				problems = append(problems, fmt.Sprintf("source_group %q appears in multiple splits: %s and %s", sourceID, prior, split))
			} else {
				sourceSplits[sourceID] = split
			}
			if sourceVideos[sourceID] == nil {
				sourceVideos[sourceID] = make(map[string]struct{})
			}
			for _, rawVideoID := range source.VideoIDs {
				videoID := strings.TrimSpace(rawVideoID)
				if videoID == "" {
					problems = append(problems, fmt.Sprintf("source_group %q contains empty video_id", sourceID))
					continue
				}
				if owner, exists := seenVideos[videoID]; exists {
					problems = append(problems, fmt.Sprintf("duplicate video_id %q in manifest source_groups %q and %q", videoID, owner, sourceID))
				} else {
					seenVideos[videoID] = sourceID
				}
				if prior, ok := videoSplits[videoID]; ok && prior != split {
					problems = append(problems, fmt.Sprintf("video_id %q appears in multiple splits: %s and %s", videoID, prior, split))
				} else {
					videoSplits[videoID] = split
				}
				sourceVideos[sourceID][videoID] = struct{}{}
			}
		}
	}

	caseIDs := make(map[string]struct{}, len(dataset.Cases))
	for i, c := range dataset.Cases {
		label := fmt.Sprintf("case %d", i+1)
		if validSplit(dataset.loadedSplit) && c.Split != dataset.loadedSplit {
			problems = append(problems, fmt.Sprintf("%s belongs to %s, but loaded split is %s", label, c.Split, dataset.loadedSplit))
		}
		if strings.TrimSpace(c.CaseID) == "" {
			problems = append(problems, label+" missing case_id")
		} else if _, exists := caseIDs[c.CaseID]; exists {
			problems = append(problems, fmt.Sprintf("duplicate case_id %q", c.CaseID))
		} else {
			caseIDs[c.CaseID] = struct{}{}
		}
		if strings.TrimSpace(c.VideoID) == "" {
			problems = append(problems, label+" missing video_id")
		}
		if strings.TrimSpace(c.SourceGroup) == "" {
			problems = append(problems, label+" missing source_group")
		}
		if !validSplit(c.Split) {
			problems = append(problems, label+" missing or invalid split")
		}
		if strings.TrimSpace(c.Question) == "" {
			problems = append(problems, label+" missing question")
		}
		if strings.TrimSpace(c.Category) == "" {
			problems = append(problems, label+" missing category")
		}
		if c.Difficulty != "easy" && c.Difficulty != "medium" && c.Difficulty != "hard" {
			problems = append(problems, label+" missing or invalid difficulty")
		}
		expectedSplit, sourceKnown := sourceSplits[c.SourceGroup]
		if !sourceKnown && strings.TrimSpace(c.SourceGroup) != "" {
			problems = append(problems, fmt.Sprintf("%s source_group %q is absent from manifest", label, c.SourceGroup))
		} else if sourceKnown && expectedSplit != c.Split {
			problems = append(problems, fmt.Sprintf("%s source_group %q belongs to %s, not %s", label, c.SourceGroup, expectedSplit, c.Split))
		}
		if videos := sourceVideos[c.SourceGroup]; videos != nil {
			if _, ok := videos[c.VideoID]; !ok {
				problems = append(problems, fmt.Sprintf("%s video_id %q is absent from source_group %q manifest", label, c.VideoID, c.SourceGroup))
			}
		}
		if c.Answerable {
			if len(c.AnswerPoints) == 0 {
				problems = append(problems, label+" answerable case missing answer_points")
			}
			if len(c.EvidenceRanges) == 0 {
				problems = append(problems, label+" answerable case missing evidence_ranges")
			}
		}
		answerPointIDs := make(map[string]struct{}, len(c.AnswerPoints))
		hasRequiredAnswerPoint := false
		for j, point := range c.AnswerPoints {
			pointID := strings.TrimSpace(point.ID)
			if pointID == "" {
				problems = append(problems, fmt.Sprintf("%s answer_points[%d] missing id", label, j))
			} else if _, exists := answerPointIDs[pointID]; exists {
				problems = append(problems, fmt.Sprintf("%s duplicate answer point id %q", label, pointID))
			} else {
				answerPointIDs[pointID] = struct{}{}
			}
			if strings.TrimSpace(point.Text) == "" {
				problems = append(problems, fmt.Sprintf("%s answer_points[%d] missing text", label, j))
			}
			if point.Required {
				hasRequiredAnswerPoint = true
			}
		}
		if c.Answerable && len(c.AnswerPoints) > 0 && !hasRequiredAnswerPoint {
			problems = append(problems, label+" answerable case missing required answer point")
		}
		evidenceIDs := make(map[string]struct{}, len(c.EvidenceRanges)+len(c.NegativeConfusers))
		for j, evidence := range c.EvidenceRanges {
			if err := validateEvidenceRange(evidence, true); err != nil {
				problems = append(problems, fmt.Sprintf("%s evidence_ranges[%d]: %v", label, j, err))
			}
			evidenceID := strings.TrimSpace(evidence.ID)
			if evidenceID != "" {
				if _, exists := evidenceIDs[evidenceID]; exists {
					problems = append(problems, fmt.Sprintf("%s duplicate evidence id %q", label, evidenceID))
				}
				evidenceIDs[evidenceID] = struct{}{}
			}
		}
		for j, evidence := range c.NegativeConfusers {
			if err := validateEvidenceRange(evidence, false); err != nil {
				problems = append(problems, fmt.Sprintf("%s negative_confusers[%d]: %v", label, j, err))
			}
			evidenceID := strings.TrimSpace(evidence.ID)
			if evidenceID != "" {
				if _, exists := evidenceIDs[evidenceID]; exists {
					problems = append(problems, fmt.Sprintf("%s duplicate evidence id %q", label, evidenceID))
				}
				evidenceIDs[evidenceID] = struct{}{}
			}
		}
	}

	testDefinition, hasTest := dataset.Manifest.Splits[SplitTest]
	if hasTest {
		if !testDefinition.Sealed {
			problems = append(problems, "test split must be sealed")
		} else {
			if strings.TrimSpace(testDefinition.ContentSHA256) == "" {
				problems = append(problems, "sealed test split missing content sha256")
			} else if ValidateSHA256Digest("test content sha256", testDefinition.ContentSHA256) == nil && (dataset.loadedSplit == "" || dataset.loadedSplit == SplitTest) {
				if contentHash, err := ComputeSplitContentSHA256(dataset, SplitTest); err != nil {
					problems = append(problems, fmt.Sprintf("compute test content sha256: %v", err))
				} else if contentHash != testDefinition.ContentSHA256 {
					problems = append(problems, fmt.Sprintf("test content sha256 mismatch: manifest=%s computed=%s", testDefinition.ContentSHA256, contentHash))
				}
			}
			if strings.TrimSpace(testDefinition.AccessTokenSHA256) == "" {
				problems = append(problems, "sealed test split missing access token sha256")
			} else if err := ValidateSHA256Digest("test access token sha256", testDefinition.AccessTokenSHA256); err != nil {
				problems = append(problems, err.Error())
			}
		}
	}

	if err := ValidateSHA256Digest("manifest sha256", dataset.Manifest.SHA256); err != nil {
		problems = append(problems, err.Error())
	} else if expectedHash, err := ComputeManifestSHA256(dataset.DatasetVersion, dataset.Manifest.Splits); err != nil {
		problems = append(problems, fmt.Sprintf("compute manifest sha256: %v", err))
	} else if dataset.Manifest.SHA256 != expectedHash {
		problems = append(problems, fmt.Sprintf("manifest sha256 mismatch: manifest=%s computed=%s", dataset.Manifest.SHA256, expectedHash))
	}

	if len(problems) > 0 {
		return fmt.Errorf("invalid strict dataset: %s", strings.Join(problems, "; "))
	}
	return nil
}

func ComputeManifestSHA256(datasetVersion string, splits map[Split]SplitDefinition) (string, error) {
	type canonicalSource struct {
		ID       string   `json:"source_group"`
		VideoIDs []string `json:"video_ids"`
	}
	type canonicalSplit struct {
		Split   Split             `json:"split"`
		Sources []canonicalSource `json:"sources"`
	}
	payload := struct {
		DatasetVersion string           `json:"dataset_version"`
		Splits         []canonicalSplit `json:"splits"`
	}{DatasetVersion: datasetVersion}

	for _, split := range []Split{SplitTrain, SplitDev, SplitTest} {
		definition := splits[split]
		item := canonicalSplit{Split: split, Sources: make([]canonicalSource, 0, len(definition.Sources))}
		for _, source := range definition.Sources {
			videos := append([]string(nil), source.VideoIDs...)
			sort.Strings(videos)
			item.Sources = append(item.Sources, canonicalSource{ID: source.ID, VideoIDs: videos})
		}
		sort.Slice(item.Sources, func(i, j int) bool { return item.Sources[i].ID < item.Sources[j].ID })
		payload.Splits = append(payload.Splits, item)
	}
	return hashJSON(payload)
}

func ComputeSplitContentSHA256(dataset Dataset, split Split) (string, error) {
	cases := make([]Case, 0)
	for _, c := range dataset.Cases {
		if c.Split == split {
			cases = append(cases, c)
		}
	}
	sort.Slice(cases, func(i, j int) bool { return cases[i].CaseID < cases[j].CaseID })
	return hashJSON(struct {
		DatasetVersion string `json:"dataset_version"`
		Split          Split  `json:"split"`
		Cases          []Case `json:"cases"`
	}{DatasetVersion: dataset.DatasetVersion, Split: split, Cases: cases})
}

func SealSplit(dataset *Dataset, split Split, token string) error {
	if dataset == nil {
		return fmt.Errorf("dataset is nil")
	}
	if split != SplitTest {
		return fmt.Errorf("only test split can be sealed")
	}
	if strings.TrimSpace(token) == "" {
		return fmt.Errorf("sealed test token is empty")
	}
	definition, ok := dataset.Manifest.Splits[split]
	if !ok {
		return fmt.Errorf("manifest missing %s split", split)
	}
	contentHash, err := ComputeSplitContentSHA256(*dataset, split)
	if err != nil {
		return err
	}
	tokenHash := sha256.Sum256([]byte(token))
	definition.Sealed = true
	definition.ContentSHA256 = contentHash
	definition.AccessTokenSHA256 = hex.EncodeToString(tokenHash[:])
	dataset.Manifest.Splits[split] = definition
	return nil
}

func AuthorizeSealedTest(dataset Dataset, token string) error {
	definition, ok := dataset.Manifest.Splits[SplitTest]
	if !ok || !definition.Sealed {
		return fmt.Errorf("test split is not sealed")
	}
	if strings.TrimSpace(token) == "" {
		return fmt.Errorf("sealed test token is required")
	}
	sum := sha256.Sum256([]byte(token))
	if hex.EncodeToString(sum[:]) != definition.AccessTokenSHA256 {
		return fmt.Errorf("sealed test token is invalid")
	}
	return nil
}

func validSplit(split Split) bool {
	return split == SplitTrain || split == SplitDev || split == SplitTest
}

func validateEvidenceRange(e EvidenceRange, positive bool) error {
	if strings.TrimSpace(e.ID) == "" {
		return fmt.Errorf("missing id")
	}
	if positive && strings.TrimSpace(e.GroupID) == "" {
		return fmt.Errorf("missing group_id")
	}
	hasTimeRange := e.StartMS != 0 || e.EndMS != 0
	hasContextIDs := false
	seenContextIDs := make(map[string]struct{}, len(e.ContextIDs))
	for _, rawID := range e.ContextIDs {
		contextID := strings.TrimSpace(rawID)
		if contextID == "" {
			return fmt.Errorf("context_ids contains an empty identity")
		}
		if _, exists := seenContextIDs[contextID]; exists {
			return fmt.Errorf("duplicate context_id %q", contextID)
		}
		seenContextIDs[contextID] = struct{}{}
		hasContextIDs = true
	}
	if hasTimeRange && (e.StartMS < 0 || e.EndMS <= e.StartMS) {
		return fmt.Errorf("invalid time range [%d,%d]", e.StartMS, e.EndMS)
	}
	if !hasTimeRange && !hasContextIDs {
		return fmt.Errorf("missing evidence locator: provide a valid time range or stable context_ids")
	}
	if e.Source != EvidenceSourceASR && e.Source != EvidenceSourceOCR && e.Source != EvidenceSourceBoth {
		return fmt.Errorf("invalid source %q", e.Source)
	}
	if positive && (e.Relevance < 1 || e.Relevance > 3) {
		return fmt.Errorf("relevance must be between 1 and 3")
	}
	return nil
}

func hashJSON(value any) (string, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func isYAMLSequence(root yaml.Node) bool {
	if len(root.Content) == 0 {
		return false
	}
	node := root.Content[0]
	return node.Kind == yaml.SequenceNode
}
