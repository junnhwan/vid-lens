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

const (
	RecipeV1 = "study-v1"
	RecipeV2 = "study-v2"
	Recipe   = "study-v3"
)

type Error struct {
	Code   string
	Status int
}

func (e *Error) Error() string          { return e.Code }
func Err(code string, status int) error { return &Error{code, status} }

type BudgetError struct{ Reason string }

func (e *BudgetError) Error() string { return "budget_exhausted: " + e.Reason }
func (e *BudgetError) Unwrap() error { return Err("budget_exhausted", 422) }
func Exhausted(reason string) error  { return &BudgetError{Reason: reason} }

var ErrLease = errors.New("artifact run lease lost")

type RetryWait struct{ Until time.Time }

func (e *RetryWait) Error() string { return "artifact provider retry is not due" }

type Ref struct {
	EvidenceID string `json:"evidence_id"`
	Relation   string `json:"relation"`
	CitationID string `json:"chat_citation_id,omitempty"`
}
type Block struct {
	BlockID      string  `json:"block_id"`
	ParentID     *string `json:"parent_id"`
	Type         string  `json:"type"`
	Title        string  `json:"title"`
	Content      string  `json:"content"`
	ClaimOrigin  string  `json:"claim_origin"`
	EvidenceRefs []Ref   `json:"evidence_refs"`
	// SourceBlockIDs records the v2 global organization lineage. It is omitted
	// from user-authored and legacy blocks.
	SourceBlockIDs []string `json:"source_block_ids,omitempty"`
}

// Relation is semantic content, not a visual connector or layout hint.
type Relation struct {
	ID            string `json:"id"`
	SourceBlockID string `json:"source_block_id"`
	TargetBlockID string `json:"target_block_id"`
	Type          string `json:"type"`
	Origin        string `json:"origin"`
	EvidenceRefs  []Ref  `json:"evidence_refs"`
}
type Body struct {
	SchemaVersion int        `json:"schema_version"`
	Kind          string     `json:"kind"`
	Title         string     `json:"title"`
	Blocks        []Block    `json:"blocks"`
	Relations     []Relation `json:"relations,omitempty"`
	Warnings      []string   `json:"warnings"`
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
	if (b.SchemaVersion != 1 && b.SchemaVersion != 2) || b.SchemaVersion == 1 && len(b.Relations) > 0 || b.Kind != "study" || strings.TrimSpace(b.Title) == "" || utf8.RuneCountInString(b.Title) > 200 || len(b.Blocks) < 1 || len(b.Blocks) > 200 || len(JSON(b)) > 512*1024 || b.Warnings == nil || len(b.Warnings) > 100 {
		return Err("invalid_request", 400)
	}
	depth := map[string]int{}
	for _, n := range b.Blocks {
		if n.BlockID == "" || len(n.BlockID) > 100 || depth[n.BlockID] > 0 || utf8.RuneCountInString(n.Title) > 200 || strings.TrimSpace(n.Title) == "" || utf8.RuneCountInString(n.Content) > 8000 || n.EvidenceRefs == nil || len(n.EvidenceRefs) > 100 {
			return Err("invalid_request", 400)
		}
		if len(n.SourceBlockIDs) > 200 {
			return Err("invalid_request", 400)
		}
		for _, id := range n.SourceBlockIDs {
			if id == "" || len(id) > 100 {
				return Err("invalid_request", 400)
			}
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
			if !allowed[r.EvidenceID] || len(r.CitationID) > 32 || (r.Relation != "supports" && r.Relation != "context" && r.Relation != "contradicts") {
				return Err("invalid_evidence", 400)
			}
		}
	}
	for _, w := range b.Warnings {
		if len(w) > 2000 {
			return Err("invalid_request", 400)
		}
	}
	if len(b.Relations) > 300 {
		return Err("invalid_request", 400)
	}
	ids, keys, dependencies := map[string]bool{}, map[string]bool{}, map[string][]string{}
	for _, rel := range b.Relations {
		if rel.ID == "" || len(rel.ID) > 100 || ids[rel.ID] || rel.SourceBlockID == rel.TargetBlockID || depth[rel.SourceBlockID] == 0 || depth[rel.TargetBlockID] == 0 || (rel.Type != "related_to" && rel.Type != "depends_on" && rel.Type != "contrasts_with") || (rel.Origin != "user" && rel.Origin != "synthesis") || rel.EvidenceRefs == nil || len(rel.EvidenceRefs) > 100 {
			return Err("invalid_request", 400)
		}
		ids[rel.ID] = true
		source, target := rel.SourceBlockID, rel.TargetBlockID
		if rel.Type != "depends_on" && source > target {
			source, target = target, source
		}
		key := rel.Type + ":" + source + ":" + target
		if keys[key] {
			return Err("invalid_request", 400)
		}
		keys[key] = true
		if rel.Type == "depends_on" {
			dependencies[rel.SourceBlockID] = append(dependencies[rel.SourceBlockID], rel.TargetBlockID)
		}
		if rel.Origin == "synthesis" && len(rel.EvidenceRefs) == 0 {
			return Err("invalid_evidence", 400)
		}
		for _, ref := range rel.EvidenceRefs {
			if !allowed[ref.EvidenceID] || len(ref.CitationID) > 32 || (ref.Relation != "supports" && ref.Relation != "context" && ref.Relation != "contradicts") {
				return Err("invalid_evidence", 400)
			}
		}
	}
	visited, active := map[string]bool{}, map[string]bool{}
	var visit func(string) bool
	visit = func(id string) bool {
		if active[id] {
			return false
		}
		if visited[id] {
			return true
		}
		active[id] = true
		for _, next := range dependencies[id] {
			if !visit(next) {
				return false
			}
		}
		active[id] = false
		visited[id] = true
		return true
	}
	for id := range dependencies {
		if !visit(id) {
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
