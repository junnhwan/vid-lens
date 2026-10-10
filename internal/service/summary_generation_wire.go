package service

import (
	"encoding/json"
	"fmt"
	"vid-lens/internal/artifact"
	"vid-lens/internal/summarydoc"
)

// The model supplies semantic content and opaque cue selections only. Frozen
// resource identity and exact timing are assembled exclusively by the server.
type summaryGenerationWireDocument struct {
	Title    string                       `json:"title"`
	Overview string                       `json:"overview"`
	Blocks   []summaryGenerationWireBlock `json:"blocks"`
}
type summaryGenerationWireBlock struct {
	ID           string   `json:"id"`
	ParentID     *string  `json:"parent_id"`
	Order        int      `json:"order"`
	Title        string   `json:"title"`
	BodyMarkdown string   `json:"body_markdown"`
	CueIDs       []string `json:"cue_ids"`
}

func (e *summaryGenerationExecution) parseGeneratedDocument(raw json.RawMessage) (summarydoc.Document, summaryGenerationCheckpoint) {
	var shape map[string]json.RawMessage
	if json.Unmarshal(raw, &shape) != nil || shape == nil {
		return summarydoc.Document{}, summaryGenerationCheckpoint{Invalid: true, ValidationCode: "invalid_generation_document", ValidationPath: "document"}
	}
	// Historical canonical responses remain subject to the original strict
	// identity, structure and provenance checks; no fields are silently ignored.
	if _, canonical := shape["schema_version"]; canonical {
		doc, err := summarydoc.Parse(raw)
		if err != nil {
			return doc, summaryGenerationDiagnostic(raw, err, summarydoc.ValidationContext{})
		}
		return doc, summaryGenerationCheckpoint{}
	}
	var wire summaryGenerationWireDocument
	if artifact.Decode(raw, &wire) != nil || len(wire.Blocks) > 512 {
		return summarydoc.Document{}, summaryGenerationCheckpoint{Invalid: true, ValidationCode: "invalid_generation_document", ValidationPath: "document"}
	}
	cues := map[string]summarydoc.Cue{}
	for _, cue := range e.source.Cues {
		cues[cue.ID] = summarydoc.Cue{StartMS: cue.StartMS, EndMS: cue.EndMS, TimingMethod: cue.TimingMethod}
	}
	doc := summarydoc.Document{SchemaVersion: summarydoc.SchemaVersion, DocumentID: e.run.ID, SourceID: e.source.ID, SourceDigest: e.source.SourceDigest, MediaRevision: e.source.Identity.MediaFingerprint, PresentationMode: "text", Title: wire.Title, Overview: wire.Overview, Blocks: make([]summarydoc.Block, 0, len(wire.Blocks))}
	for index, block := range wire.Blocks {
		path := fmt.Sprintf("document.blocks[%d].cue_ids", index)
		if len(block.CueIDs) > 128 {
			return summarydoc.Document{}, summaryGenerationCheckpoint{Invalid: true, ValidationCode: "source_ref_limit", ValidationPath: path}
		}
		result := summarydoc.Block{ID: block.ID, ParentID: block.ParentID, Order: block.Order, Title: block.Title, BodyMarkdown: block.BodyMarkdown, SourceRefs: []summarydoc.SourceRef{}, Figures: []summarydoc.Figure{}}
		seen := map[string]bool{}
		for _, id := range block.CueIDs {
			cue, ok := cues[id]
			if !ok || seen[id] {
				return summarydoc.Document{}, summaryGenerationCheckpoint{Invalid: true, ValidationCode: "invalid_source_cue_selection", ValidationPath: path}
			}
			seen[id] = true
			result.SourceRefs = append(result.SourceRefs, summarydoc.SourceRef{SourceID: e.source.ID, CueIDs: []string{id}, StartMS: cue.StartMS, EndMS: cue.EndMS, TimingMethod: cue.TimingMethod})
		}
		doc.Blocks = append(doc.Blocks, result)
	}
	// Reuse canonical structural limits before the full frozen-source validation.
	checked, err := summarydoc.Parse([]byte(artifact.JSON(doc)))
	if err != nil {
		return summarydoc.Document{}, summaryGenerationDiagnostic([]byte(artifact.JSON(doc)), err, summarydoc.ValidationContext{})
	}
	return checked, summaryGenerationCheckpoint{}
}
