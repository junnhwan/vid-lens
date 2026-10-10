package summarydoc

import (
	"encoding/json"
	"fmt"
	"sort"
)

const (
	OpUpdateDocumentTitle = "update_document_title"
	OpUpdateOverview      = "update_overview"
	OpUpdateBlock         = "update_block"
	OpInsertBlock         = "insert_block"
	OpDeleteBlock         = "delete_block"
	OpMoveBlock           = "move_block"
	OpUpdateFigureCaption = "update_figure_caption"
)

type Operation struct {
	Op           string  `json:"op"`
	BlockID      string  `json:"block_id,omitempty"`
	FigureID     string  `json:"figure_id,omitempty"`
	Title        *string `json:"title,omitempty"`
	Overview     *string `json:"overview,omitempty"`
	BodyMarkdown *string `json:"body_markdown,omitempty"`
	Block        *Block  `json:"block,omitempty"`
	// For move_block, null/absent means move to document root.
	ParentID *string `json:"parent_id,omitempty"`
	Order    *int    `json:"order,omitempty"`
	Caption  *string `json:"caption,omitempty"`
	Alt      *string `json:"alt,omitempty"`
	Supports *string `json:"supports,omitempty"`
}

type Patch struct {
	BaseContentHashKind string      `json:"base_content_hash_kind"`
	BaseContentHash     string      `json:"base_content_hash"`
	Operations          []Operation `json:"operations"`
}

type Change struct {
	Kind     string          `json:"kind"`
	BlockID  string          `json:"block_id,omitempty"`
	FigureID string          `json:"figure_id,omitempty"`
	Before   json.RawMessage `json:"before,omitempty"`
	After    json.RawMessage `json:"after,omitempty"`
}

type Preview struct {
	Changes           []Change `json:"changes"`
	AffectedFigureIDs []string `json:"affected_figure_ids"`
	Markdown          string   `json:"markdown"`
	ContentDigest     string   `json:"content_digest"`
	ContentHashKind   string   `json:"content_hash_kind"`
}

func ParsePatch(data []byte) (Patch, error) {
	var patch Patch
	err := strictDecode(data, &patch)
	if err == nil && (patch.BaseContentHashKind != HashKind || patch.BaseContentHash == "" || len(patch.Operations) < 1 || len(patch.Operations) > 50) {
		err = fmt.Errorf("invalid summary patch")
	}
	return patch, err
}

func ApplyPatch(base Document, patch Patch, ctx ValidationContext) (Document, Preview, error) {
	hash, err := Digest(base)
	if err != nil {
		return Document{}, Preview{}, err
	}
	if patch.BaseContentHashKind != HashKind || patch.BaseContentHash != hash {
		return Document{}, Preview{}, fmt.Errorf("stale summary patch base")
	}
	return Apply(base, patch.Operations, ctx)
}

// Apply is atomic: all operations work on a deep copy and the final document
// must pass full source/resource validation. Deletion removes the whole subtree
// including figures; retained blocks keep their IDs and immutable references.
func Apply(base Document, operations []Operation, ctx ValidationContext) (Document, Preview, error) {
	if err := Validate(base, ctx); err != nil {
		return Document{}, Preview{}, err
	}
	if len(operations) < 1 || len(operations) > 50 {
		return Document{}, Preview{}, fmt.Errorf("invalid operation count")
	}
	next := clone(base)
	preview := Preview{Changes: []Change{}, AffectedFigureIDs: []string{}, ContentHashKind: HashKind}
	for _, op := range operations {
		if err := validateOperation(op); err != nil {
			return Document{}, Preview{}, err
		}
		change := Change{Kind: op.Op, BlockID: op.BlockID, FigureID: op.FigureID}
		switch op.Op {
		case OpUpdateDocumentTitle:
			change.Before = raw(next.Title)
			next.Title = *op.Title
			change.After = raw(next.Title)
		case OpUpdateOverview:
			change.Before = raw(next.Overview)
			next.Overview = *op.Overview
			change.After = raw(next.Overview)
		case OpInsertBlock:
			for _, b := range next.Blocks {
				if b.ID == op.Block.ID {
					return Document{}, Preview{}, fmt.Errorf("insert duplicates existing block")
				}
			}
			next.Blocks = append(next.Blocks, *op.Block)
			change.BlockID = op.Block.ID
			change.After = raw(op.Block)
			for _, f := range op.Block.Figures {
				preview.AffectedFigureIDs = append(preview.AffectedFigureIDs, f.ID)
			}
		case OpUpdateBlock, OpMoveBlock, OpDeleteBlock:
			index := -1
			for i := range next.Blocks {
				if next.Blocks[i].ID == op.BlockID {
					index = i
					break
				}
			}
			if index < 0 {
				return Document{}, Preview{}, fmt.Errorf("unknown block %q", op.BlockID)
			}
			change.Before = raw(next.Blocks[index])
			if op.Op == OpDeleteBlock {
				remove := map[string]bool{op.BlockID: true}
				for {
					grew := false
					for _, b := range next.Blocks {
						if b.ParentID != nil && remove[*b.ParentID] && !remove[b.ID] {
							remove[b.ID] = true
							grew = true
						}
					}
					if !grew {
						break
					}
				}
				var kept []Block
				for _, b := range next.Blocks {
					if remove[b.ID] {
						for _, f := range b.Figures {
							preview.AffectedFigureIDs = append(preview.AffectedFigureIDs, f.ID)
						}
						if b.ID != op.BlockID {
							preview.Changes = append(preview.Changes, Change{Kind: OpDeleteBlock, BlockID: b.ID, Before: raw(b)})
						}
					} else {
						kept = append(kept, b)
					}
				}
				next.Blocks = kept
			} else {
				b := &next.Blocks[index]
				if op.Op == OpMoveBlock {
					b.ParentID = op.ParentID
					b.Order = *op.Order
				} else {
					if op.Title != nil {
						b.Title = *op.Title
					}
					if op.BodyMarkdown != nil {
						b.BodyMarkdown = *op.BodyMarkdown
					}
				}
				change.After = raw(*b)
			}
		case OpUpdateFigureCaption:
			found := false
			for i := range next.Blocks {
				for j := range next.Blocks[i].Figures {
					f := &next.Blocks[i].Figures[j]
					if f.ID != op.FigureID {
						continue
					}
					found = true
					change.BlockID = next.Blocks[i].ID
					change.Before = raw(*f)
					if op.Caption != nil {
						f.Caption = *op.Caption
					}
					if op.Alt != nil {
						f.Alt = *op.Alt
					}
					if op.Supports != nil {
						f.Supports = *op.Supports
					}
					change.After = raw(*f)
					preview.AffectedFigureIDs = append(preview.AffectedFigureIDs, f.ID)
				}
			}
			if !found {
				return Document{}, Preview{}, fmt.Errorf("unknown figure %q", op.FigureID)
			}
		}
		preview.Changes = append(preview.Changes, change)
	}
	// A planner cannot silently leave a figure-only presentation after deleting
	// its last image. The server explicitly records the truthful text fallback.
	figureCount := 0
	for _, b := range next.Blocks {
		figureCount += len(b.Figures)
	}
	if figureCount == 0 && next.PresentationMode != "text" {
		before := next.PresentationMode
		next.PresentationMode = "text"
		preview.Changes = append(preview.Changes, Change{Kind: "presentation_mode_changed", Before: raw(before), After: raw("text")})
	}
	if err := Validate(next, ctx); err != nil {
		return Document{}, Preview{}, err
	}
	baseHash, _ := Digest(base)
	preview.ContentDigest, _ = Digest(next)
	if baseHash == preview.ContentDigest {
		return Document{}, Preview{}, fmt.Errorf("nothing to change")
	}
	preview.Markdown, _ = Markdown(next)
	sort.Strings(preview.AffectedFigureIDs)
	unique := preview.AffectedFigureIDs[:0]
	for _, id := range preview.AffectedFigureIDs {
		if len(unique) == 0 || unique[len(unique)-1] != id {
			unique = append(unique, id)
		}
	}
	preview.AffectedFigureIDs = unique
	return clone(next), preview, nil
}

func raw(value any) json.RawMessage { b, _ := json.Marshal(value); return b }

func validateOperation(op Operation) error {
	valid := false
	// Reject fields unrelated to the operation instead of silently ignoring them.
	copy := op
	copy.Op = ""
	switch op.Op {
	case OpUpdateDocumentTitle:
		valid = op.Title != nil
		copy.Title = nil
	case OpUpdateOverview:
		valid = op.Overview != nil
		copy.Overview = nil
	case OpUpdateBlock:
		valid = validID(op.BlockID) && (op.Title != nil || op.BodyMarkdown != nil)
		copy.BlockID = ""
		copy.Title = nil
		copy.BodyMarkdown = nil
	case OpInsertBlock:
		valid = op.Block != nil
		copy.Block = nil
	case OpDeleteBlock:
		valid = validID(op.BlockID)
		copy.BlockID = ""
	case OpMoveBlock:
		valid = validID(op.BlockID) && op.Order != nil
		copy.BlockID = ""
		copy.Order = nil
		copy.ParentID = nil
	case OpUpdateFigureCaption:
		valid = validID(op.FigureID) && (op.Caption != nil || op.Alt != nil || op.Supports != nil)
		copy.FigureID = ""
		copy.Caption = nil
		copy.Alt = nil
		copy.Supports = nil
	}
	if !valid || string(raw(copy)) != "{\"op\":\"\"}" {
		return fmt.Errorf("invalid operation fields for %q", op.Op)
	}
	return nil
}
