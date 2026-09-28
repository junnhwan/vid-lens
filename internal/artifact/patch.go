package artifact

import (
	"encoding/json"
	"slices"
	"strconv"
	"strings"
)

const (
	PatchBasisUserInstruction   = "user_instruction"
	PatchBasisEvidenceSupported = "evidence_supported"
	PatchBasisEvidenceConflict  = "evidence_conflict"

	PatchOpUpdateTitle   = "update_title"
	PatchOpUpdateBlock   = "update_block"
	PatchOpInsertBlock   = "insert_block"
	PatchOpDeleteSubtree = "delete_subtree"
	PatchOpMoveSubtree   = "move_subtree"
	PatchOpSplitBlock    = "split_block"
	PatchOpMergeSiblings = "merge_siblings"
)

// Patch is the bounded, declarative edit format accepted from the edit planner.
// Artifact and version fields are untrusted until matched against PatchAuthorization.
type Patch struct {
	SchemaVersion int              `json:"schema_version"`
	ArtifactID    string           `json:"artifact_id"`
	BaseVersionID string           `json:"base_version_id"`
	BaseVersion   int64            `json:"base_version"`
	Basis         string           `json:"basis"`
	EvidenceIDs   []string         `json:"evidence_ids"`
	Operations    []PatchOperation `json:"operations"`
}

// PatchOperation is a closed tagged union. Fields that can be omitted by an
// update use pointers so omission is distinct from a requested empty value.
type PatchOperation struct {
	Op             string      `json:"op"`
	ExpectedHash   string      `json:"expected_hash,omitempty"`
	ExpectedHashes []string    `json:"expected_hashes,omitempty"`
	BlockID        string      `json:"block_id,omitempty"`
	BlockIDs       []string    `json:"block_ids,omitempty"`
	Key            string      `json:"key,omitempty"`
	ParentID       *string     `json:"parent_id,omitempty"`
	AfterBlockID   *string     `json:"after_block_id,omitempty"`
	Type           *string     `json:"type,omitempty"`
	Title          *string     `json:"title,omitempty"`
	Content        *string     `json:"content,omitempty"`
	EvidenceRefs   *[]Ref      `json:"evidence_refs,omitempty"`
	Parts          []SplitPart `json:"parts,omitempty"`
}

type SplitPart struct {
	Type         string `json:"type"`
	Title        string `json:"title"`
	Content      string `json:"content"`
	EvidenceRefs []Ref  `json:"evidence_refs"`
}

// PatchAuthorization is server-owned state frozen with the edit run.
type PatchAuthorization struct {
	OperationID        string
	ArtifactID         string
	BaseVersionID      string
	BaseVersion        int64
	SelectedBlockIDs   []string
	AllowedEvidenceIDs map[string]bool
}

type PatchCounts struct {
	Added   int `json:"added"`
	Updated int `json:"updated"`
	Deleted int `json:"deleted"`
	Moved   int `json:"moved"`
}

type PatchChange struct {
	Kind        string  `json:"kind"`
	BlockID     string  `json:"block_id,omitempty"`
	Before      *Block  `json:"before,omitempty"`
	After       *Block  `json:"after,omitempty"`
	BeforeTitle *string `json:"before_title,omitempty"`
	AfterTitle  *string `json:"after_title,omitempty"`
	BeforeIndex *int    `json:"before_index,omitempty"`
	AfterIndex  *int    `json:"after_index,omitempty"`
}

type BlockMapping struct {
	Kind         string   `json:"kind"`
	FromBlockIDs []string `json:"from_block_ids"`
	ToBlockIDs   []string `json:"to_block_ids"`
}

type PatchDiff struct {
	Counts        PatchCounts    `json:"counts"`
	Changes       []PatchChange  `json:"changes"`
	BlockMappings []BlockMapping `json:"block_mappings"`
}

type PatchResult struct {
	Body Body      `json:"body"`
	Diff PatchDiff `json:"diff"`
}

func BlockHash(block Block) string { return Hash(JSON(block)) }

// CanonicalPatch returns the stable persisted representation and its hash.
func CanonicalPatch(patch Patch) (string, string, error) {
	b, err := json.Marshal(patch)
	if err != nil {
		return "", "", Err("invalid_patch", 400)
	}
	canonical := string(b)
	return canonical, Hash(canonical), nil
}

// EditPatch applies the whole operation group to an in-memory copy. It never
// mutates base; callers can publish the returned body only after transaction
// checks have succeeded.
func EditPatch(base Body, patch Patch, auth PatchAuthorization) (PatchResult, error) {
	result := PatchResult{Body: cloneBody(base), Diff: PatchDiff{Changes: []PatchChange{}, BlockMappings: []BlockMapping{}}}
	if err := validatePatchEnvelope(base, patch, auth); err != nil {
		return PatchResult{}, err
	}

	authorized := authorizedBlockIDs(base, auth.SelectedBlockIDs)
	touched := make(map[string]bool)
	titleTouched := false
	for opIndex, op := range patch.Operations {
		switch op.Op {
		case PatchOpUpdateTitle:
			if titleTouched {
				return PatchResult{}, Err("invalid_patch", 400)
			}
			if err := applyUpdateTitle(&result, op, len(auth.SelectedBlockIDs) == 0); err != nil {
				return PatchResult{}, err
			}
			titleTouched = true
		case PatchOpUpdateBlock:
			if err := applyUpdateBlock(&result, op, patch.Basis, authorized, touched, auth.AllowedEvidenceIDs); err != nil {
				return PatchResult{}, err
			}
		case PatchOpInsertBlock:
			if err := applyInsertBlock(&result, op, patch.Basis, auth.OperationID, len(auth.SelectedBlockIDs) == 0, authorized, touched, auth.AllowedEvidenceIDs); err != nil {
				return PatchResult{}, err
			}
		case PatchOpDeleteSubtree:
			if err := applyDeleteSubtree(&result, op, authorized, touched); err != nil {
				return PatchResult{}, err
			}
		case PatchOpMoveSubtree:
			if err := applyMoveSubtree(&result, op, len(auth.SelectedBlockIDs) == 0, authorized, touched); err != nil {
				return PatchResult{}, err
			}
		case PatchOpSplitBlock:
			if err := applySplitBlock(&result, op, patch.Basis, auth.OperationID, opIndex, authorized, touched, auth.AllowedEvidenceIDs); err != nil {
				return PatchResult{}, err
			}
		case PatchOpMergeSiblings:
			if err := applyMergeSiblings(&result, op, patch.Basis, authorized, touched); err != nil {
				return PatchResult{}, err
			}
		default:
			return PatchResult{}, Err("invalid_patch", 400)
		}
	}
	if err := result.Body.Validate(auth.AllowedEvidenceIDs); err != nil {
		if artifactErr, ok := err.(*Error); ok && artifactErr.Code == "invalid_evidence" {
			return PatchResult{}, err
		}
		return PatchResult{}, Err("invalid_patch", 400)
	}
	return result, nil
}

func applyMergeSiblings(result *PatchResult, op PatchOperation, basis string, authorized, touched map[string]bool) error {
	if len(op.BlockIDs) < 2 || len(op.BlockIDs) > 50 || len(op.BlockIDs) != len(op.ExpectedHashes) || op.ExpectedHash != "" || op.BlockID != "" || op.Key != "" || op.ParentID != nil || op.AfterBlockID != nil || op.Type != nil || op.Content != nil || op.EvidenceRefs != nil || len(op.Parts) != 0 {
		return Err("invalid_patch", 400)
	}
	indices := make([]int, len(op.BlockIDs))
	blocks := make([]Block, len(op.BlockIDs))
	seen := make(map[string]bool, len(op.BlockIDs))
	for i, id := range op.BlockIDs {
		idx := blockIndex(result.Body.Blocks, id)
		if id == "" || seen[id] {
			return Err("invalid_patch", 400)
		}
		if idx < 0 || !authorized[id] {
			return Err("target_scope_mismatch", 409)
		}
		if touched[id] || BlockHash(result.Body.Blocks[idx]) != op.ExpectedHashes[i] || !blockIsLeaf(result.Body.Blocks, idx) {
			return Err("invalid_patch", 400)
		}
		if i > 0 && !sameStringPtr(result.Body.Blocks[idx].ParentID, result.Body.Blocks[indices[0]].ParentID) {
			return Err("invalid_patch", 400)
		}
		seen[id] = true
		indices[i] = idx
		blocks[i] = cloneBlock(result.Body.Blocks[idx])
	}
	if !adjacentSiblingIDs(result.Body.Blocks, op.BlockIDs) {
		return Err("invalid_patch", 400)
	}

	merged := cloneBlock(blocks[0])
	if op.Title != nil {
		merged.Title = *op.Title
	}
	contents := make([]string, len(blocks))
	for i := range blocks {
		contents[i] = blocks[i].Content
	}
	merged.Content = mergeUniqueParagraphs(contents)
	merged.EvidenceRefs = unionRefs(blocks)
	merged.SourceBlockIDs = unionSourceBlockIDs(blocks)
	merged.ClaimOrigin = "user"
	if basis == PatchBasisEvidenceSupported {
		merged.ClaimOrigin = "synthesis"
		for _, block := range blocks {
			if block.ClaimOrigin == "user" {
				merged.ClaimOrigin = "user"
				break
			}
		}
	}
	result.Body.Blocks[indices[0]] = merged
	result.Body.Blocks = removeBlockIndices(result.Body.Blocks, indices[1:])
	firstBefore, firstAfter := cloneBlock(blocks[0]), cloneBlock(merged)
	result.Diff.Counts.Updated++
	result.Diff.Changes = append(result.Diff.Changes, PatchChange{Kind: "updated", BlockID: merged.BlockID, Before: &firstBefore, After: &firstAfter})
	for i := 1; i < len(blocks); i++ {
		deleted := cloneBlock(blocks[i])
		result.Diff.Counts.Deleted++
		result.Diff.Changes = append(result.Diff.Changes, PatchChange{Kind: "deleted", BlockID: deleted.BlockID, Before: &deleted})
	}
	result.Diff.BlockMappings = append(result.Diff.BlockMappings, BlockMapping{Kind: "merge", FromBlockIDs: slices.Clone(op.BlockIDs), ToBlockIDs: []string{merged.BlockID}})
	for _, id := range op.BlockIDs {
		touched[id] = true
	}
	return nil
}

func mergeUniqueParagraphs(contents []string) string {
	seen := make(map[string]bool)
	paragraphs := make([]string, 0)
	for _, content := range contents {
		for _, paragraph := range strings.Split(content, "\n\n") {
			if seen[paragraph] {
				continue
			}
			seen[paragraph] = true
			paragraphs = append(paragraphs, paragraph)
		}
	}
	return strings.Join(paragraphs, "\n\n")
}

func unionRefs(blocks []Block) []Ref {
	seen := make(map[Ref]bool)
	refs := make([]Ref, 0)
	for _, block := range blocks {
		for _, ref := range block.EvidenceRefs {
			if seen[ref] {
				continue
			}
			seen[ref] = true
			refs = append(refs, ref)
		}
	}
	return refs
}

func unionSourceBlockIDs(blocks []Block) []string {
	seen := make(map[string]bool)
	ids := make([]string, 0)
	for _, block := range blocks {
		for _, id := range block.SourceBlockIDs {
			if seen[id] {
				continue
			}
			seen[id] = true
			ids = append(ids, id)
		}
	}
	return ids
}

func applySplitBlock(result *PatchResult, op PatchOperation, basis, operationID string, opIndex int, authorized, touched map[string]bool, allowed map[string]bool) error {
	if op.BlockID == "" || op.ExpectedHash == "" || len(op.Parts) < 2 || len(op.Parts) > 50 || len(op.ExpectedHashes) != 0 || len(op.BlockIDs) != 0 || op.Key != "" || op.ParentID != nil || op.AfterBlockID != nil || op.Type != nil || op.Title != nil || op.Content != nil || op.EvidenceRefs != nil {
		return Err("invalid_patch", 400)
	}
	if !authorized[op.BlockID] {
		return Err("target_scope_mismatch", 409)
	}
	idx := blockIndex(result.Body.Blocks, op.BlockID)
	if idx < 0 || touched[op.BlockID] || BlockHash(result.Body.Blocks[idx]) != op.ExpectedHash || !blockIsLeaf(result.Body.Blocks, idx) {
		return Err("invalid_patch", 400)
	}
	for _, part := range op.Parts {
		if part.EvidenceRefs == nil {
			return Err("invalid_patch", 400)
		}
		if err := validateRefs(part.EvidenceRefs, allowed); err != nil {
			return err
		}
	}
	original := cloneBlock(result.Body.Blocks[idx])
	claimOrigin := "user"
	if original.ClaimOrigin != "user" && basis == PatchBasisEvidenceSupported {
		claimOrigin = "synthesis"
	}
	pieces := make([]Block, len(op.Parts))
	toIDs := make([]string, len(op.Parts))
	for i, part := range op.Parts {
		blockID := original.BlockID
		if i > 0 {
			blockID = derivedBlockID(operationID, "split", strconv.Itoa(opIndex), strconv.Itoa(i))
			if touched[blockID] || blockIndex(result.Body.Blocks, blockID) >= 0 {
				return Err("invalid_patch", 400)
			}
		}
		pieces[i] = Block{
			BlockID:        blockID,
			ParentID:       cloneStringPtr(original.ParentID),
			Type:           part.Type,
			Title:          part.Title,
			Content:        part.Content,
			ClaimOrigin:    claimOrigin,
			EvidenceRefs:   slices.Clone(part.EvidenceRefs),
			SourceBlockIDs: slices.Clone(original.SourceBlockIDs),
		}
		toIDs[i] = blockID
	}
	result.Body.Blocks = slices.Replace(result.Body.Blocks, idx, idx+1, pieces...)
	first := cloneBlock(pieces[0])
	result.Diff.Counts.Updated++
	result.Diff.Changes = append(result.Diff.Changes, PatchChange{Kind: "updated", BlockID: original.BlockID, Before: &original, After: &first})
	touched[original.BlockID] = true
	for i := 1; i < len(pieces); i++ {
		piece := cloneBlock(pieces[i])
		result.Diff.Counts.Added++
		result.Diff.Changes = append(result.Diff.Changes, PatchChange{Kind: "added", BlockID: piece.BlockID, After: &piece})
		touched[piece.BlockID] = true
		authorized[piece.BlockID] = true
	}
	result.Diff.BlockMappings = append(result.Diff.BlockMappings, BlockMapping{Kind: "split", FromBlockIDs: []string{original.BlockID}, ToBlockIDs: toIDs})
	return nil
}

func applyMoveSubtree(result *PatchResult, op PatchOperation, fullScope bool, authorized, touched map[string]bool) error {
	if op.BlockID == "" || op.ExpectedHash == "" || len(op.ExpectedHashes) != 0 || len(op.BlockIDs) != 0 || op.Key != "" || op.Type != nil || op.Title != nil || op.Content != nil || op.EvidenceRefs != nil || len(op.Parts) != 0 {
		return Err("invalid_patch", 400)
	}
	if !authorized[op.BlockID] {
		return Err("target_scope_mismatch", 409)
	}
	start := blockIndex(result.Body.Blocks, op.BlockID)
	if start < 0 || touched[op.BlockID] || BlockHash(result.Body.Blocks[start]) != op.ExpectedHash {
		return Err("invalid_patch", 400)
	}
	indices := subtreeIndices(result.Body.Blocks, start)
	movingIDs := make(map[string]bool, len(indices))
	for _, idx := range indices {
		block := result.Body.Blocks[idx]
		movingIDs[block.BlockID] = true
		if !authorized[block.BlockID] {
			return Err("target_scope_mismatch", 409)
		}
	}
	if op.ParentID == nil {
		if !fullScope {
			return Err("target_scope_mismatch", 409)
		}
	} else if movingIDs[*op.ParentID] {
		return Err("invalid_patch", 400)
	} else if blockIndex(result.Body.Blocks, *op.ParentID) < 0 || !authorized[*op.ParentID] {
		return Err("target_scope_mismatch", 409)
	}
	if op.AfterBlockID != nil {
		if movingIDs[*op.AfterBlockID] {
			return Err("invalid_patch", 400)
		}
		if !authorized[*op.AfterBlockID] {
			return Err("target_scope_mismatch", 409)
		}
	}

	before := cloneBlock(result.Body.Blocks[start])
	moving := make([]Block, len(indices))
	for i, idx := range indices {
		moving[i] = cloneBlock(result.Body.Blocks[idx])
	}
	remaining := removeBlockIndices(cloneBlocks(result.Body.Blocks), indices)
	insertAt, err := insertionIndex(remaining, op.ParentID, op.AfterBlockID)
	if err != nil {
		return err
	}
	moving[0].ParentID = cloneStringPtr(op.ParentID)
	resultBlocks := slices.Insert(remaining, insertAt, moving...)
	newIndex := blockIndex(resultBlocks, op.BlockID)
	if JSON(resultBlocks) == JSON(result.Body.Blocks) {
		return Err("invalid_patch", 400)
	}
	result.Body.Blocks = resultBlocks
	after := cloneBlock(moving[0])
	beforeIndex, afterIndex := start, newIndex
	result.Diff.Counts.Moved++
	result.Diff.Changes = append(result.Diff.Changes, PatchChange{Kind: "moved", BlockID: op.BlockID, Before: &before, After: &after, BeforeIndex: &beforeIndex, AfterIndex: &afterIndex})
	touched[op.BlockID] = true
	return nil
}

func applyDeleteSubtree(result *PatchResult, op PatchOperation, authorized, touched map[string]bool) error {
	if op.BlockID == "" || op.ExpectedHash == "" || len(op.ExpectedHashes) != 0 || len(op.BlockIDs) != 0 || op.Key != "" || op.ParentID != nil || op.AfterBlockID != nil || op.Type != nil || op.Title != nil || op.Content != nil || op.EvidenceRefs != nil || len(op.Parts) != 0 {
		return Err("invalid_patch", 400)
	}
	if !authorized[op.BlockID] {
		return Err("target_scope_mismatch", 409)
	}
	start := blockIndex(result.Body.Blocks, op.BlockID)
	if start < 0 || BlockHash(result.Body.Blocks[start]) != op.ExpectedHash {
		return Err("invalid_patch", 400)
	}
	indices := subtreeIndices(result.Body.Blocks, start)
	if len(indices) == len(result.Body.Blocks) {
		return Err("invalid_patch", 400)
	}
	for _, idx := range indices {
		block := result.Body.Blocks[idx]
		if touched[block.BlockID] {
			return Err("invalid_patch", 400)
		}
		if !authorized[block.BlockID] {
			return Err("target_scope_mismatch", 409)
		}
	}
	removed := make([]Block, len(indices))
	for i, idx := range indices {
		removed[i] = result.Body.Blocks[idx]
	}
	result.Body.Blocks = removeBlockIndices(result.Body.Blocks, indices)
	for _, block := range removed {
		cloned := cloneBlock(block)
		result.Diff.Counts.Deleted++
		result.Diff.Changes = append(result.Diff.Changes, PatchChange{Kind: "deleted", BlockID: block.BlockID, Before: &cloned})
		touched[block.BlockID] = true
	}
	return nil
}

func applyInsertBlock(result *PatchResult, op PatchOperation, basis, operationID string, fullScope bool, authorized, touched map[string]bool, allowed map[string]bool) error {
	if !nonBlank(op.Key) || len(op.Key) > 100 || op.Type == nil || op.Title == nil || op.Content == nil || op.EvidenceRefs == nil || op.ExpectedHash != "" || len(op.ExpectedHashes) != 0 || op.BlockID != "" || len(op.BlockIDs) != 0 || len(op.Parts) != 0 {
		return Err("invalid_patch", 400)
	}
	if err := validateRefs(*op.EvidenceRefs, allowed); err != nil {
		return err
	}
	if op.ParentID == nil {
		if !fullScope {
			return Err("target_scope_mismatch", 409)
		}
	} else {
		if blockIndex(result.Body.Blocks, *op.ParentID) < 0 || !authorized[*op.ParentID] {
			return Err("target_scope_mismatch", 409)
		}
	}
	insertAt, err := insertionIndex(result.Body.Blocks, op.ParentID, op.AfterBlockID)
	if err != nil {
		return err
	}
	if op.AfterBlockID != nil && !authorized[*op.AfterBlockID] {
		return Err("target_scope_mismatch", 409)
	}
	blockID := derivedBlockID(operationID, "insert", op.Key)
	if touched[blockID] || blockIndex(result.Body.Blocks, blockID) >= 0 {
		return Err("invalid_patch", 400)
	}
	claimOrigin := "user"
	if basis == PatchBasisEvidenceSupported {
		claimOrigin = "synthesis"
	}
	block := Block{
		BlockID:      blockID,
		ParentID:     cloneStringPtr(op.ParentID),
		Type:         *op.Type,
		Title:        *op.Title,
		Content:      *op.Content,
		ClaimOrigin:  claimOrigin,
		EvidenceRefs: slices.Clone(*op.EvidenceRefs),
	}
	result.Body.Blocks = slices.Insert(result.Body.Blocks, insertAt, block)
	result.Diff.Counts.Added++
	result.Diff.Changes = append(result.Diff.Changes, PatchChange{Kind: "added", BlockID: blockID, After: blockPtr(block)})
	touched[blockID] = true
	return nil
}

func insertionIndex(blocks []Block, parentID, afterBlockID *string) (int, error) {
	if afterBlockID != nil {
		idx := blockIndex(blocks, *afterBlockID)
		if idx < 0 || !sameStringPtr(blocks[idx].ParentID, parentID) {
			return 0, Err("invalid_patch", 400)
		}
		// Schema v1 orders direct siblings by their occurrence in the flat
		// parent-before-child list, but does not require descendants to be
		// contiguous. Insert before the next direct sibling so `after A` cannot
		// silently become `after B` merely because an A descendant appears late.
		for next := idx + 1; next < len(blocks); next++ {
			if sameStringPtr(blocks[next].ParentID, parentID) {
				return next, nil
			}
		}
		return subtreeEnd(blocks, idx), nil
	}
	if parentID == nil {
		return 0, nil
	}
	idx := blockIndex(blocks, *parentID)
	if idx < 0 {
		return 0, Err("invalid_patch", 400)
	}
	return idx + 1, nil
}

func subtreeEnd(blocks []Block, rootIndex int) int {
	indices := subtreeIndices(blocks, rootIndex)
	return indices[len(indices)-1] + 1
}

func subtreeIndices(blocks []Block, rootIndex int) []int {
	rootID := blocks[rootIndex].BlockID
	indices := make([]int, 0, len(blocks)-rootIndex)
	for i := rootIndex; i < len(blocks); i++ {
		if i == rootIndex || blockHasAncestor(blocks, blocks[i], map[string]bool{rootID: true}) {
			indices = append(indices, i)
		}
	}
	return indices
}

func blockIsLeaf(blocks []Block, index int) bool {
	return len(subtreeIndices(blocks, index)) == 1
}

func adjacentSiblingIDs(blocks []Block, ids []string) bool {
	if len(ids) == 0 {
		return false
	}
	first := blockIndex(blocks, ids[0])
	if first < 0 {
		return false
	}
	parent := blocks[first].ParentID
	wanted := 0
	started := false
	for i := range blocks {
		if !sameStringPtr(blocks[i].ParentID, parent) {
			continue
		}
		if blocks[i].BlockID == ids[wanted] {
			started = true
			wanted++
			if wanted == len(ids) {
				return true
			}
			continue
		}
		if started {
			return false
		}
	}
	return false
}

func removeBlockIndices(blocks []Block, indices []int) []Block {
	removed := make(map[int]bool, len(indices))
	for _, index := range indices {
		removed[index] = true
	}
	kept := make([]Block, 0, len(blocks)-len(removed))
	for i := range blocks {
		if !removed[i] {
			kept = append(kept, blocks[i])
		}
	}
	return kept
}

func derivedBlockID(operationID string, parts ...string) string {
	return "agent-" + Hash(operationID + "\x00" + strings.Join(parts, "\x00"))[:32]
}

func cloneStringPtr(value *string) *string {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func sameStringPtr(left, right *string) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func applyUpdateTitle(result *PatchResult, op PatchOperation, fullScope bool) error {
	if !fullScope {
		return Err("target_scope_mismatch", 409)
	}
	if op.ExpectedHash == "" || op.Title == nil || op.BlockID != "" || len(op.BlockIDs) != 0 || op.Key != "" || op.ParentID != nil || op.AfterBlockID != nil || op.Type != nil || op.Content != nil || op.EvidenceRefs != nil || len(op.Parts) != 0 || len(op.ExpectedHashes) != 0 || Hash(result.Body.Title) != op.ExpectedHash || *op.Title == result.Body.Title {
		return Err("invalid_patch", 400)
	}
	before, after := result.Body.Title, *op.Title
	result.Body.Title = after
	result.Diff.Counts.Updated++
	result.Diff.Changes = append(result.Diff.Changes, PatchChange{Kind: "title_updated", BeforeTitle: &before, AfterTitle: &after})
	return nil
}

func validatePatchEnvelope(base Body, patch Patch, auth PatchAuthorization) error {
	if patch.SchemaVersion != 1 || patch.ArtifactID == "" || patch.BaseVersionID == "" || patch.BaseVersion <= 0 || !nonBlank(auth.OperationID) || len(auth.OperationID) > 128 || len(patch.Operations) == 0 || len(patch.Operations) > 50 || len(patch.EvidenceIDs) > 100 {
		return Err("invalid_patch", 400)
	}
	if patch.ArtifactID != auth.ArtifactID || patch.BaseVersionID != auth.BaseVersionID || patch.BaseVersion != auth.BaseVersion {
		return Err("target_scope_mismatch", 409)
	}
	if patch.Basis != PatchBasisUserInstruction && patch.Basis != PatchBasisEvidenceSupported && patch.Basis != PatchBasisEvidenceConflict {
		return Err("invalid_patch", 400)
	}
	if patch.EvidenceIDs == nil || auth.AllowedEvidenceIDs == nil {
		return Err("invalid_patch", 400)
	}
	if patch.Basis != PatchBasisUserInstruction && len(patch.EvidenceIDs) == 0 {
		return Err("invalid_patch", 400)
	}
	seenEvidence := make(map[string]bool, len(patch.EvidenceIDs))
	for _, id := range patch.EvidenceIDs {
		if id == "" || seenEvidence[id] || !auth.AllowedEvidenceIDs[id] {
			return Err("invalid_evidence", 400)
		}
		seenEvidence[id] = true
	}
	if len(auth.SelectedBlockIDs) > 20 {
		return Err("target_scope_mismatch", 409)
	}
	seenSelected := make(map[string]bool, len(auth.SelectedBlockIDs))
	baseIDs := make(map[string]bool, len(base.Blocks))
	for _, block := range base.Blocks {
		baseIDs[block.BlockID] = true
	}
	for _, id := range auth.SelectedBlockIDs {
		if id == "" || seenSelected[id] || !baseIDs[id] {
			return Err("target_scope_mismatch", 409)
		}
		seenSelected[id] = true
	}
	if err := base.Validate(auth.AllowedEvidenceIDs); err != nil {
		return Err("invalid_patch", 400)
	}
	return nil
}

func applyUpdateBlock(result *PatchResult, op PatchOperation, basis string, authorized, touched map[string]bool, allowed map[string]bool) error {
	if op.BlockID == "" || op.ExpectedHash == "" || touched[op.BlockID] || op.Title == nil && op.Content == nil && op.Type == nil && op.EvidenceRefs == nil || len(op.BlockIDs) != 0 || op.Key != "" || op.ParentID != nil || op.AfterBlockID != nil || len(op.Parts) != 0 || len(op.ExpectedHashes) != 0 {
		return Err("invalid_patch", 400)
	}
	if !authorized[op.BlockID] {
		return Err("target_scope_mismatch", 409)
	}
	idx := blockIndex(result.Body.Blocks, op.BlockID)
	if idx < 0 || BlockHash(result.Body.Blocks[idx]) != op.ExpectedHash {
		return Err("invalid_patch", 400)
	}
	before := cloneBlock(result.Body.Blocks[idx])
	after := cloneBlock(before)
	if op.Title != nil {
		after.Title = *op.Title
	}
	if op.Content != nil {
		after.Content = *op.Content
	}
	if op.Type != nil {
		after.Type = *op.Type
	}
	if op.EvidenceRefs != nil {
		if err := validateRefs(*op.EvidenceRefs, allowed); err != nil {
			return err
		}
		after.EvidenceRefs = slices.Clone(*op.EvidenceRefs)
	}
	if JSON(before) == JSON(after) {
		return Err("invalid_patch", 400)
	}
	if before.ClaimOrigin == "user" || basis != PatchBasisEvidenceSupported {
		after.ClaimOrigin = "user"
	} else {
		after.ClaimOrigin = "synthesis"
	}
	result.Body.Blocks[idx] = after
	result.Diff.Counts.Updated++
	result.Diff.Changes = append(result.Diff.Changes, PatchChange{Kind: "updated", BlockID: op.BlockID, Before: blockPtr(before), After: blockPtr(after)})
	touched[op.BlockID] = true
	return nil
}

func validateRefs(refs []Ref, allowed map[string]bool) error {
	if refs == nil || len(refs) > 100 {
		return Err("invalid_patch", 400)
	}
	for _, ref := range refs {
		if !allowed[ref.EvidenceID] || len(ref.CitationID) > 32 || ref.Relation != "supports" && ref.Relation != "context" && ref.Relation != "contradicts" {
			return Err("invalid_evidence", 400)
		}
	}
	return nil
}

func authorizedBlockIDs(body Body, selected []string) map[string]bool {
	result := make(map[string]bool, len(body.Blocks))
	if len(selected) == 0 {
		for _, block := range body.Blocks {
			result[block.BlockID] = true
		}
		return result
	}
	selectedSet := make(map[string]bool, len(selected))
	for _, id := range selected {
		selectedSet[id] = true
	}
	for _, block := range body.Blocks {
		if selectedSet[block.BlockID] || blockHasAncestor(body.Blocks, block, selectedSet) {
			result[block.BlockID] = true
		}
	}
	return result
}

func blockHasAncestor(blocks []Block, block Block, ancestors map[string]bool) bool {
	for block.ParentID != nil {
		if ancestors[*block.ParentID] {
			return true
		}
		idx := blockIndex(blocks, *block.ParentID)
		if idx < 0 {
			return false
		}
		block = blocks[idx]
	}
	return false
}

func blockIndex(blocks []Block, id string) int {
	for i := range blocks {
		if blocks[i].BlockID == id {
			return i
		}
	}
	return -1
}

func blockPtr(block Block) *Block { cloned := cloneBlock(block); return &cloned }

func cloneBlock(block Block) Block {
	if block.ParentID != nil {
		parent := *block.ParentID
		block.ParentID = &parent
	}
	block.EvidenceRefs = slices.Clone(block.EvidenceRefs)
	block.SourceBlockIDs = slices.Clone(block.SourceBlockIDs)
	return block
}

func cloneBody(body Body) Body {
	cloned := body
	cloned.Blocks = cloneBlocks(body.Blocks)
	cloned.Warnings = slices.Clone(body.Warnings)
	return cloned
}

func cloneBlocks(blocks []Block) []Block {
	cloned := make([]Block, len(blocks))
	for i := range blocks {
		cloned[i] = cloneBlock(blocks[i])
	}
	return cloned
}

func nonBlank(value string) bool { return strings.TrimSpace(value) != "" }
