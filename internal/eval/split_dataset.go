package eval

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// DatasetManifestDocument contains split ownership and digests only. It never
// contains questions, answer points, or evidence from any split.
type DatasetManifestDocument struct {
	SchemaVersion  string        `json:"schema_version" yaml:"schema_version"`
	DatasetVersion string        `json:"dataset_version" yaml:"dataset_version"`
	Manifest       SplitManifest `json:"manifest" yaml:"manifest"`
}

// SplitDatasetDocument is the physical case file for exactly one split.
type SplitDatasetDocument struct {
	SchemaVersion  string `json:"schema_version" yaml:"schema_version"`
	DatasetVersion string `json:"dataset_version" yaml:"dataset_version"`
	Split          Split  `json:"split" yaml:"split"`
	Cases          []Case `json:"cases" yaml:"cases"`
}

type SplitLoadOptions struct {
	ExpectedVersion    string
	Split              Split
	SealedToken        string
	AccessRegistryPath string
	AccessEvent        SealedAccessEvent
}

func MarshalDatasetManifestYAML(dataset Dataset) ([]byte, error) {
	document := DatasetManifestDocument{
		SchemaVersion: dataset.SchemaVersion, DatasetVersion: dataset.DatasetVersion, Manifest: dataset.Manifest,
	}
	raw, err := yaml.Marshal(document)
	if err != nil {
		return nil, fmt.Errorf("marshal dataset manifest: %w", err)
	}
	return raw, nil
}

func MarshalSplitDatasetYAML(dataset Dataset, split Split) ([]byte, error) {
	if !validSplit(split) {
		return nil, fmt.Errorf("invalid split %q", split)
	}
	cases := make([]Case, 0)
	for _, c := range dataset.Cases {
		if c.Split == split {
			cases = append(cases, c)
		}
	}
	return MarshalSplitDatasetDocumentYAML(SplitDatasetDocument{
		SchemaVersion: dataset.SchemaVersion, DatasetVersion: dataset.DatasetVersion, Split: split, Cases: cases,
	})
}

func MarshalSplitDatasetDocumentYAML(document SplitDatasetDocument) ([]byte, error) {
	raw, err := yaml.Marshal(document)
	if err != nil {
		return nil, fmt.Errorf("marshal split dataset: %w", err)
	}
	return raw, nil
}

// LoadSplitDataset parses only the supplied split case file. A caller loading
// train or dev never needs access to the sealed test file. Loading test is
// fail-closed: both a valid token and a writable append-only access registry
// are required before cases are returned.
func LoadSplitDataset(manifestRaw, splitRaw []byte, opts SplitLoadOptions) (Dataset, error) {
	if !validSplit(opts.Split) {
		return Dataset{}, fmt.Errorf("invalid requested split %q", opts.Split)
	}
	var manifestDocument DatasetManifestDocument
	if err := yaml.Unmarshal(manifestRaw, &manifestDocument); err != nil {
		return Dataset{}, fmt.Errorf("parse dataset manifest: %w", err)
	}
	var splitDocument SplitDatasetDocument
	if err := yaml.Unmarshal(splitRaw, &splitDocument); err != nil {
		return Dataset{}, fmt.Errorf("parse %s split dataset: %w", opts.Split, err)
	}
	if splitDocument.Split != opts.Split {
		return Dataset{}, fmt.Errorf("split file contains %q, requested %q", splitDocument.Split, opts.Split)
	}
	if manifestDocument.SchemaVersion != splitDocument.SchemaVersion {
		return Dataset{}, fmt.Errorf("split schema_version %q does not match manifest %q", splitDocument.SchemaVersion, manifestDocument.SchemaVersion)
	}
	if manifestDocument.DatasetVersion != splitDocument.DatasetVersion {
		return Dataset{}, fmt.Errorf("split dataset_version %q does not match manifest %q", splitDocument.DatasetVersion, manifestDocument.DatasetVersion)
	}
	if opts.ExpectedVersion != "" && manifestDocument.DatasetVersion != opts.ExpectedVersion {
		return Dataset{}, fmt.Errorf("dataset version %q does not match requested %q", manifestDocument.DatasetVersion, opts.ExpectedVersion)
	}
	for i, c := range splitDocument.Cases {
		if c.Split != opts.Split {
			return Dataset{}, fmt.Errorf("case %d belongs to split %q, expected %q", i+1, c.Split, opts.Split)
		}
	}

	dataset := Dataset{
		SchemaVersion: manifestDocument.SchemaVersion, DatasetVersion: manifestDocument.DatasetVersion,
		Manifest: manifestDocument.Manifest, Cases: splitDocument.Cases, loadedSplit: opts.Split,
	}
	if err := ValidateDataset(dataset, ValidationOptions{ExpectedVersion: opts.ExpectedVersion}); err != nil {
		return Dataset{}, err
	}
	definition := dataset.Manifest.Splits[opts.Split]
	if strings.TrimSpace(definition.ContentSHA256) == "" {
		return Dataset{}, fmt.Errorf("%s split missing content sha256 for separate file", opts.Split)
	}
	contentHash, err := ComputeSplitContentSHA256(dataset, opts.Split)
	if err != nil {
		return Dataset{}, fmt.Errorf("compute %s split content sha256: %w", opts.Split, err)
	}
	if definition.ContentSHA256 != contentHash {
		return Dataset{}, fmt.Errorf("%s content sha256 mismatch: manifest=%s computed=%s", opts.Split, definition.ContentSHA256, contentHash)
	}

	if opts.Split == SplitTest {
		if err := AuthorizeSealedTest(dataset, opts.SealedToken); err != nil {
			return Dataset{}, err
		}
		if strings.TrimSpace(opts.AccessRegistryPath) == "" {
			return Dataset{}, fmt.Errorf("sealed test access registry path is required")
		}
		event := opts.AccessEvent
		if event.DatasetVersion != "" && event.DatasetVersion != dataset.DatasetVersion {
			return Dataset{}, fmt.Errorf("sealed access event dataset_version %q does not match %q", event.DatasetVersion, dataset.DatasetVersion)
		}
		event.DatasetVersion = dataset.DatasetVersion
		event.TestContentSHA256 = contentHash
		datasetHash, err := ComputeDatasetSHA256(dataset)
		if err != nil {
			return Dataset{}, fmt.Errorf("compute sealed dataset sha256: %w", err)
		}
		event.DatasetSHA256 = datasetHash
		if err := AppendSealedAccess(opts.AccessRegistryPath, event); err != nil {
			return Dataset{}, fmt.Errorf("append sealed test access registry: %w", err)
		}
		dataset.sealedAccessRegistered = true
	}
	return dataset, nil
}
