package eval

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"strings"
)

// ValidateSHA256Digest enforces the canonical representation used by manifests
// and run artifacts: exactly 64 lowercase hexadecimal characters.
func ValidateSHA256Digest(field, value string) error {
	if len(value) != sha256.Size*2 || value != strings.ToLower(value) {
		return fmt.Errorf("%s must be a 64-character lowercase SHA-256 hex digest", field)
	}
	if _, err := hex.DecodeString(value); err != nil {
		return fmt.Errorf("%s must be a 64-character lowercase SHA-256 hex digest", field)
	}
	return nil
}

func ComputeFileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

type ArtifactFileSet struct {
	CorpusPath         string
	ChunkManifestPath  string
	VectorArtifactPath string
	ConfigPath         string
	PromptPath         string
}

type artifactFileDigest struct {
	field string
	path  string
	value string
}

func BindArtifactFileDigests(metadata *RunMetadata, files ArtifactFileSet) error {
	if metadata == nil {
		return fmt.Errorf("run metadata is nil")
	}
	digests, err := computeArtifactFileDigests(files)
	if err != nil {
		return err
	}
	metadata.CorpusSHA256 = digests[0].value
	metadata.ChunkManifestSHA256 = digests[1].value
	metadata.VectorArtifactSHA256 = digests[2].value
	metadata.ConfigSHA256 = digests[3].value
	metadata.Prompt.SHA256 = digests[4].value
	return nil
}

func VerifyArtifactFileDigests(metadata RunMetadata, files ArtifactFileSet) error {
	digests, err := computeArtifactFileDigests(files)
	if err != nil {
		return err
	}
	expected := []string{
		metadata.CorpusSHA256,
		metadata.ChunkManifestSHA256,
		metadata.VectorArtifactSHA256,
		metadata.ConfigSHA256,
		metadata.Prompt.SHA256,
	}
	for i, digest := range digests {
		if err := ValidateSHA256Digest(digest.field+" sha256", expected[i]); err != nil {
			return err
		}
		if digest.value != expected[i] {
			return fmt.Errorf("%s sha256 mismatch: metadata=%s actual=%s", digest.field, expected[i], digest.value)
		}
	}
	return nil
}

func computeArtifactFileDigests(files ArtifactFileSet) ([]artifactFileDigest, error) {
	items := []artifactFileDigest{
		{field: "corpus", path: files.CorpusPath},
		{field: "chunk manifest", path: files.ChunkManifestPath},
		{field: "vector artifact", path: files.VectorArtifactPath},
		{field: "config", path: files.ConfigPath},
		{field: "prompt", path: files.PromptPath},
	}
	for i := range items {
		if strings.TrimSpace(items[i].path) == "" {
			return nil, fmt.Errorf("%s artifact file path is required", items[i].field)
		}
		digest, err := ComputeFileSHA256(items[i].path)
		if err != nil {
			return nil, fmt.Errorf("hash %s artifact file: %w", items[i].field, err)
		}
		items[i].value = digest
	}
	return items, nil
}
