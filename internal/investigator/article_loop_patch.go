package investigator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/jackc/pgx/v5"
	"vidlens/internal/domain"
	"vidlens/internal/store"
)

const loopPatchTool = "revise_blocks"

type loopBlockReplacement struct {
	BlockID string      `json:"block_id" jsonschema:"description=Exact existing block ID from canonical_locations; never invent an ID"`
	Content loopContent `json:"content" jsonschema:"description=Complete replacement of this one block, exactly one semantic constructor and its actual sources"`
}
type loopLimitReplacement struct {
	SectionID   string                  `json:"section_id"`
	Limitations []loopSectionLimitation `json:"limitations" jsonschema:"description=Complete scoped limitations for this chapter; an empty array removes obsolete limitations"`
}
type loopPatchInput struct {
	ExpectedVersion    int                    `json:"expected_version"`
	Finish             bool                   `json:"finish,omitempty"`
	Replacements       []loopBlockReplacement `json:"replacements,omitempty"`
	RemoveBlockIDs     []string               `json:"remove_block_ids,omitempty"`
	SectionLimitations []loopLimitReplacement `json:"section_limitations,omitempty"`
}

// 将块级语义意图确定性展开为原有章节事务，不让模型重发未修改正文。
func (s *session) patchLoopBlocks(ctx context.Context, key string, in loopPatchInput) (any, bool, error) {
	planKey := "article-loop:patch-plan:" + key
	var plan struct {
		InputHash string        `json:"input_hash"`
		Save      loopSaveInput `json:"save"`
	}
	op, err := s.engine.Store.Operation(ctx, s.claim.Run.ID, planKey)
	if err == nil && op.Status == "completed" {
		if json.Unmarshal(op.Result, &plan) != nil || plan.InputHash != store.Hash(in) {
			return nil, false, domain.ErrFenced
		}
		return s.saveLoopSections(ctx, key, plan.Save)
	}
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, false, err
	}
	saved, d, err := s.loadLoopDraft(ctx)
	if err != nil {
		return nil, false, err
	}
	if saved.Version != in.ExpectedVersion {
		return nil, false, domain.Err(409, "DRAFT_VERSION_CONFLICT", "局部修订版本已变化。")
	}
	reject := func(message string) (any, bool, error) { return map[string]any{"error": message}, false, nil }
	if len(domain.JSON(in)) > loopWriteBytes || len(in.Replacements)+len(in.RemoveBlockIDs)+len(in.SectionLimitations) == 0 {
		return reject("revise_blocks requires a nonempty bounded edit")
	}
	semantic, err := s.loopAuthorDraft(d.Article, d.Notes, saved.Version)
	if err != nil {
		return nil, false, err
	}
	type position struct{ section, block int }
	locations := map[string]position{}
	for i, section := range d.Article.Sections {
		for j, block := range section.Blocks {
			locations[block.ID] = position{i, j}
		}
	}
	changes := map[string]loopContent{}
	removed := map[string]bool{}
	touched := map[int]bool{}
	for _, replacement := range in.Replacements {
		pos, ok := locations[replacement.BlockID]
		if !ok {
			return reject("unknown block_id: " + replacement.BlockID)
		}
		if _, duplicate := changes[replacement.BlockID]; duplicate {
			return reject("duplicate block_id: " + replacement.BlockID)
		}
		changes[replacement.BlockID] = replacement.Content
		touched[pos.section] = true
	}
	for _, id := range in.RemoveBlockIDs {
		pos, ok := locations[id]
		if !ok {
			return reject("unknown remove_block_id: " + id)
		}
		if _, replaced := changes[id]; replaced || removed[id] {
			return reject("duplicate or conflicting block edit: " + id)
		}
		removed[id] = true
		touched[pos.section] = true
	}
	seenLimits := map[string]bool{}
	for _, limits := range in.SectionLimitations {
		index := slices.IndexFunc(semantic.Sections, func(section loopSection) bool { return section.SectionID == limits.SectionID })
		if index < 0 || seenLimits[limits.SectionID] {
			return reject("unknown/duplicate limitation section_id: " + limits.SectionID)
		}
		seenLimits[limits.SectionID] = true
		touched[index] = true
		semantic.Sections[index].Limitations = limits.Limitations
	}
	expanded := loopSaveInput{ExpectedVersion: in.ExpectedVersion, Finish: in.Finish, Sections: []loopSection{}}
	for i, section := range semantic.Sections {
		expanded.Order = append(expanded.Order, section.SectionID)
		if !touched[i] {
			continue
		}
		section.Content = nil
		for j, content := range semantic.Sections[i].Content {
			id := d.Article.Sections[i].Blocks[j].ID
			if removed[id] {
				continue
			}
			if replacement, ok := changes[id]; ok {
				content = replacement
			}
			section.Content = append(section.Content, content)
		}
		if len(section.Content) == 0 {
			return reject(fmt.Sprintf("cannot empty chapter %s; use save_sections for an explicit structural edit", section.SectionID))
		}
		expanded.Sections = append(expanded.Sections, section)
	}
	plan.InputHash, plan.Save = store.Hash(in), expanded
	if err := s.loopSave(ctx, planKey, "article_loop_patch_plan", plan); err != nil {
		return nil, false, err
	}
	return s.saveLoopSections(ctx, key, expanded)
}
