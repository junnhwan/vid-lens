package eval

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateSHA256DigestRequiresLowercaseHex(t *testing.T) {
	valid := strings.Repeat("ab", sha256.Size)
	if err := ValidateSHA256Digest("artifact", valid); err != nil {
		t.Fatalf("ValidateSHA256Digest(valid) error = %v", err)
	}
	for _, value := range []string{
		strings.Repeat("a", 63),
		strings.Repeat("g", 64),
		strings.ToUpper(valid),
		" " + strings.Repeat("a", 64),
	} {
		if err := ValidateSHA256Digest("artifact", value); err == nil {
			t.Fatalf("ValidateSHA256Digest(%q) error = nil", value)
		}
	}
}

func TestDatasetRejectsMalformedManifestDigests(t *testing.T) {
	for _, tt := range []struct {
		name string
		edit func(*Dataset)
		want string
	}{
		{name: "manifest", edit: func(d *Dataset) { d.Manifest.SHA256 = strings.Repeat("g", 64) }, want: "manifest sha256"},
		{name: "test content", edit: func(d *Dataset) {
			definition := d.Manifest.Splits[SplitTest]
			definition.ContentSHA256 = strings.Repeat("A", 64)
			d.Manifest.Splits[SplitTest] = definition
		}, want: "test content sha256"},
		{name: "test token", edit: func(d *Dataset) {
			definition := d.Manifest.Splits[SplitTest]
			definition.AccessTokenSHA256 = strings.Repeat("z", 64)
			d.Manifest.Splits[SplitTest] = definition
		}, want: "access token sha256"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dataset := validStrictDataset(t)
			tt.edit(&dataset)
			err := ValidateDataset(dataset, ValidationOptions{ExpectedVersion: dataset.DatasetVersion})
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("ValidateDataset() error = %v, want malformed %s", err, tt.want)
			}
		})
	}
}

func TestBindAndVerifyArtifactFileDigestsUsesActualBytes(t *testing.T) {
	dir := t.TempDir()
	write := func(name, value string) string {
		t.Helper()
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(value), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	files := ArtifactFileSet{
		CorpusPath:         write("corpus.jsonl", "corpus-v1"),
		ChunkManifestPath:  write("chunks.json", "chunks-v1"),
		VectorArtifactPath: write("vectors.json", "vectors-v1"),
		ConfigPath:         write("config.yaml", "top_k: 5"),
		PromptPath:         write("prompt.txt", "answer with citations"),
	}
	metadata := RunMetadata{}
	if err := BindArtifactFileDigests(&metadata, files); err != nil {
		t.Fatalf("BindArtifactFileDigests() error = %v", err)
	}
	wantCorpus := sha256.Sum256([]byte("corpus-v1"))
	if metadata.CorpusSHA256 != hex.EncodeToString(wantCorpus[:]) {
		t.Fatalf("CorpusSHA256 = %s, want digest of actual file", metadata.CorpusSHA256)
	}
	if err := VerifyArtifactFileDigests(metadata, files); err != nil {
		t.Fatalf("VerifyArtifactFileDigests() error = %v", err)
	}
	if err := os.WriteFile(files.CorpusPath, []byte("tampered"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := VerifyArtifactFileDigests(metadata, files); err == nil || !strings.Contains(err.Error(), "corpus") || !strings.Contains(err.Error(), "mismatch") {
		t.Fatalf("VerifyArtifactFileDigests(tampered) error = %v", err)
	}
}

func TestBindArtifactFileDigestsFailsClosedForMissingFile(t *testing.T) {
	files := ArtifactFileSet{CorpusPath: filepath.Join(t.TempDir(), "missing")}
	if err := BindArtifactFileDigests(&RunMetadata{}, files); err == nil || !strings.Contains(err.Error(), "corpus") {
		t.Fatalf("BindArtifactFileDigests() error = %v", err)
	}
}
