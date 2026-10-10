package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"vid-lens/internal/summarydoc"
)

// Only server-defined codes and field names / numeric indexes are persisted.
// Validator errors can contain provider-controlled IDs; never store them raw.
func summaryGenerationDiagnostic(raw []byte, validationErr error, ctx summarydoc.ValidationContext) summaryGenerationCheckpoint {
	result := summaryGenerationCheckpoint{Invalid: true, ValidationCode: "invalid_summary_structure", ValidationPath: "document"}
	var doc summarydoc.Document
	if json.Unmarshal(raw, &doc) != nil {
		result.ValidationCode = "invalid_summary_json"
		return result
	}
	var policy *summarydoc.GenerationContentError
	if errors.As(validationErr, &policy) {
		result.ValidationCode = policy.Code
		result.ValidationPath = "document.blocks"
		if policy.Code == "body_source_refs_missing" {
			for i, b := range doc.Blocks {
				if strings.TrimSpace(b.BodyMarkdown) != "" && len(b.SourceRefs) == 0 {
					result.ValidationPath = fmt.Sprintf("document.blocks[%d].source_refs", i)
					break
				}
			}
		}
		return result
	}
	if doc.SchemaVersion != summarydoc.SchemaVersion {
		result.ValidationPath = "document.schema_version"
		return result
	}
	if ctx.SourceID != "" && (doc.SourceID != ctx.SourceID || doc.SourceDigest != ctx.SourceDigest || doc.MediaRevision != ctx.MediaRevision) {
		result.ValidationCode = "frozen_source_identity_mismatch"
		return result
	}
	ids := map[string]bool{}
	for i, b := range doc.Blocks {
		if ids[b.ID] {
			result.ValidationPath = fmt.Sprintf("document.blocks[%d].id", i)
			return result
		}
		ids[b.ID] = true
	}
	for i, b := range doc.Blocks {
		if b.ParentID != nil && !ids[*b.ParentID] {
			result.ValidationPath = fmt.Sprintf("document.blocks[%d].parent_id", i)
			return result
		}
		if ctx.SourceID == "" {
			continue
		}
		for j, ref := range b.SourceRefs {
			probe := doc
			probe.Overview = ""
			probe.Blocks = []summarydoc.Block{{ID: "diagnostic", Title: "验证", BodyMarkdown: "验证", SourceRefs: []summarydoc.SourceRef{ref}}}
			probe.PresentationMode = "text"
			if summarydoc.Validate(probe, ctx) != nil {
				result.ValidationCode = "invalid_source_reference"
				result.ValidationPath = fmt.Sprintf("document.blocks[%d].source_refs[%d]", i, j)
				for _, id := range ref.CueIDs {
					if _, ok := ctx.Cues[id]; !ok {
						result.ValidationCode = "unknown_source_cue"
						result.ValidationPath += ".cue_ids"
						return result
					}
				}
				return result
			}
		}
	}
	return result
}
