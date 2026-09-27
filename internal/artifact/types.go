// Package artifact defines the versioned product contract independently of UI and orchestration.
package artifact

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"
	"unicode/utf8"
)

const Recipe = "study-v1"

type Error struct {
	Code   string
	Status int
}

func (e *Error) Error() string          { return e.Code }
func Err(code string, status int) error { return &Error{code, status} }

var ErrLease = errors.New("artifact run lease lost")

type RetryWait struct{ Until time.Time }

func (e *RetryWait) Error() string { return "artifact provider retry is not due" }

type Ref struct {
	EvidenceID string `json:"evidence_id"`
	Relation   string `json:"relation"`
}
type Block struct {
	BlockID      string  `json:"block_id"`
	ParentID     *string `json:"parent_id"`
	Type         string  `json:"type"`
	Title        string  `json:"title"`
	Content      string  `json:"content"`
	ClaimOrigin  string  `json:"claim_origin"`
	EvidenceRefs []Ref   `json:"evidence_refs"`
}
type Body struct {
	SchemaVersion int      `json:"schema_version"`
	Kind          string   `json:"kind"`
	Title         string   `json:"title"`
	Blocks        []Block  `json:"blocks"`
	Warnings      []string `json:"warnings"`
}
type GenerationRequest struct {
	Kind        string  `json:"kind"`
	Scope       string  `json:"scope"`
	SourceIDs   []int64 `json:"source_ids"`
	Goal        string  `json:"goal"`
	ArtifactID  *string `json:"artifact_id"`
	BaseVersion int64   `json:"base_version"`
}

func (r GenerationRequest) Validate() error {
	if r.Kind != "study" || r.Scope != "video" {
		return Err("unsupported_recipe", 400)
	}
	if len(r.SourceIDs) != 1 || r.SourceIDs[0] <= 0 || utf8.RuneCountInString(r.Goal) > 2000 || r.BaseVersion < 0 || (r.ArtifactID == nil && r.BaseVersion != 0) {
		return Err("invalid_request", 400)
	}
	if r.ArtifactID != nil && strings.TrimSpace(*r.ArtifactID) == "" {
		return Err("invalid_request", 400)
	}
	return nil
}
func ValidateKey(key string) error {
	if len(key) < 1 || len(key) > 128 {
		return Err("invalid_request", 400)
	}
	for _, c := range key {
		if c < 33 || c > 126 {
			return Err("invalid_request", 400)
		}
	}
	return nil
}
func JSON(v any) string    { b, _ := json.Marshal(v); return string(b) }
func Hash(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }
func Decode(data []byte, v any) error {
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		return Err("invalid_request", 400)
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return Err("invalid_request", 400)
	}
	return nil
}
func (b Body) Validate(allowed map[string]bool) error {
	if b.SchemaVersion != 1 || b.Kind != "study" || strings.TrimSpace(b.Title) == "" || utf8.RuneCountInString(b.Title) > 200 || len(b.Blocks) < 1 || len(b.Blocks) > 200 || len(JSON(b)) > 512*1024 || b.Warnings == nil || len(b.Warnings) > 100 {
		return Err("invalid_request", 400)
	}
	depth := map[string]int{}
	for _, n := range b.Blocks {
		if n.BlockID == "" || len(n.BlockID) > 100 || depth[n.BlockID] > 0 || utf8.RuneCountInString(n.Title) > 200 || strings.TrimSpace(n.Title) == "" || utf8.RuneCountInString(n.Content) > 8000 || n.EvidenceRefs == nil || len(n.EvidenceRefs) > 100 {
			return Err("invalid_request", 400)
		}
		if n.Type != "section" && n.Type != "concept" && n.Type != "example" && n.Type != "note" {
			return Err("invalid_request", 400)
		}
		if n.ClaimOrigin != "source" && n.ClaimOrigin != "synthesis" && n.ClaimOrigin != "user" {
			return Err("invalid_request", 400)
		}
		d := 1
		if n.ParentID != nil {
			if depth[*n.ParentID] == 0 {
				return Err("invalid_request", 400)
			}
			d = depth[*n.ParentID] + 1
		}
		if d > 8 {
			return Err("invalid_request", 400)
		}
		depth[n.BlockID] = d
		if n.ClaimOrigin == "source" && len(n.EvidenceRefs) == 0 {
			return Err("invalid_evidence", 400)
		}
		for _, r := range n.EvidenceRefs {
			if !allowed[r.EvidenceID] || (r.Relation != "supports" && r.Relation != "context" && r.Relation != "contradicts") {
				return Err("invalid_evidence", 400)
			}
		}
	}
	for _, w := range b.Warnings {
		if len(w) > 2000 {
			return Err("invalid_request", 400)
		}
	}
	return nil
}
func StampEdits(body *Body, previous *Body) {
	old := map[string]string{}
	if previous != nil {
		for _, b := range previous.Blocks {
			old[b.BlockID] = JSON(b)
		}
	}
	changed := false
	for i, b := range body.Blocks {
		if old[b.BlockID] != JSON(b) {
			body.Blocks[i].ClaimOrigin = "user"
			changed = true
		}
	}
	if changed {
		for _, w := range body.Warnings {
			if w == "human_edited_unverified" {
				return
			}
		}
		body.Warnings = append(body.Warnings, "human_edited_unverified")
	}
}
