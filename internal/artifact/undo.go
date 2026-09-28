package artifact

import "slices"

// SafeUndo computes an inverse against the current head. It restores only
// fields changed between base and result, and refuses to overwrite a later
// value of any such field.
func SafeUndo(base, result, current Body) (PatchResult, error) {
	allowed := collectEvidenceIDs(base, result, current)
	if base.Validate(allowed) != nil || result.Validate(allowed) != nil {
		return PatchResult{}, Err("invalid_patch", 400)
	}
	if current.Validate(allowed) != nil {
		return PatchResult{}, Err("undo_conflict", 409)
	}
	undone := cloneBody(current)
	if base.Title != result.Title {
		if current.Title != result.Title {
			return PatchResult{}, Err("undo_conflict", 409)
		}
		undone.Title = base.Title
	}

	baseByID := blocksByID(base.Blocks)
	resultByID := blocksByID(result.Blocks)
	currentByID := blocksByID(current.Blocks)
	added := make(map[string]bool)
	restoredByID := make(map[string]Block, len(current.Blocks)+len(base.Blocks))
	for id, baseBlock := range baseByID {
		resultBlock, resultOK := resultByID[id]
		if !resultOK {
			if _, exists := currentByID[id]; exists {
				return PatchResult{}, Err("undo_conflict", 409)
			}
			restoredByID[id] = cloneBlock(baseBlock)
			continue
		}
		currentBlock, currentOK := currentByID[id]
		if !currentOK {
			if blockTouchedByResult(base, result, id) {
				return PatchResult{}, Err("undo_conflict", 409)
			}
			continue
		}
		restored, err := restoreTouchedBlockFields(baseBlock, resultBlock, currentBlock)
		if err != nil {
			return PatchResult{}, err
		}
		restoredByID[id] = restored
	}
	for id, resultBlock := range resultByID {
		if _, ok := baseByID[id]; !ok {
			currentBlock, exists := currentByID[id]
			if !exists || JSON(currentBlock) != JSON(resultBlock) {
				return PatchResult{}, Err("undo_conflict", 409)
			}
			added[id] = true
		}
	}
	for id, block := range currentByID {
		if added[id] {
			continue
		}
		if _, exists := restoredByID[id]; !exists {
			restoredByID[id] = cloneBlock(block)
		}
		if _, exists := resultByID[id]; !exists && hasAncestorInSet(currentByID, block, added) {
			return PatchResult{}, Err("undo_conflict", 409)
		}
	}
	baseChildren := directChildren(base.Blocks)
	resultChildren := directChildren(result.Blocks)
	currentChildren := directChildren(current.Blocks)
	orders := make(map[parentIdentity][]string)
	parents := make(map[parentIdentity]bool)
	for parent := range baseChildren {
		parents[parent] = true
	}
	for parent := range resultChildren {
		parents[parent] = true
	}
	for parent := range currentChildren {
		parents[parent] = true
	}
	for parent := range parents {
		baseIDs, resultIDs, currentIDs := baseChildren[parent], resultChildren[parent], currentChildren[parent]
		if !slices.Equal(baseIDs, resultIDs) {
			if unsafeRestorationAnchor(baseIDs, resultIDs, currentIDs, restoredByID, parent) {
				return PatchResult{}, Err("undo_conflict", 409)
			}
			expectedResultIDs := make([]string, 0, len(resultIDs))
			for _, id := range resultIDs {
				block, exists := currentByID[id]
				if exists && parentOf(block) == parent {
					expectedResultIDs = append(expectedResultIDs, id)
				}
			}
			if !slices.Equal(projectIDs(currentIDs, expectedResultIDs), expectedResultIDs) {
				return PatchResult{}, Err("undo_conflict", 409)
			}
			desiredBaseIDs := make([]string, 0, len(baseIDs))
			for _, id := range baseIDs {
				block, exists := restoredByID[id]
				if exists && parentOf(block) == parent {
					desiredBaseIDs = append(desiredBaseIDs, id)
				}
			}
			orders[parent] = inverseChildOrder(desiredBaseIDs, expectedResultIDs, currentIDs)
			continue
		}
		for _, id := range currentIDs {
			if !added[id] {
				orders[parent] = append(orders[parent], id)
			}
		}
	}
	flattened, err := flattenBlocks(restoredByID, orders, base.Blocks, result.Blocks, current.Blocks)
	if err != nil {
		return PatchResult{}, err
	}
	undone.Blocks = flattened
	if undone.Validate(allowed) != nil {
		return PatchResult{}, Err("undo_conflict", 409)
	}
	return PatchResult{Body: undone, Diff: diffBodies(current, undone)}, nil
}

func unsafeRestorationAnchor(baseIDs, resultIDs, currentIDs []string, restored map[string]Block, parent parentIdentity) bool {
	resultSet := make(map[string]bool, len(resultIDs))
	for _, id := range resultIDs {
		resultSet[id] = true
	}
	hasRestoredRemoval, hasAnchor, hasLaterOnly := false, false, false
	for _, id := range baseIDs {
		block, exists := restored[id]
		if !exists || parentOf(block) != parent {
			continue
		}
		if resultSet[id] {
			hasAnchor = true
		} else {
			hasRestoredRemoval = true
		}
	}
	for _, id := range currentIDs {
		if !resultSet[id] {
			hasLaterOnly = true
			break
		}
	}
	return hasRestoredRemoval && !hasAnchor && hasLaterOnly
}

func blockTouchedByResult(base, result Body, id string) bool {
	baseIndex, resultIndex := blockIndex(base.Blocks, id), blockIndex(result.Blocks, id)
	if baseIndex < 0 || resultIndex < 0 {
		return true
	}
	if JSON(base.Blocks[baseIndex]) != JSON(result.Blocks[resultIndex]) {
		return true
	}
	return relativeCommonSiblingIndex(base, result, id) != relativeCommonSiblingIndex(result, base, id)
}

type parentIdentity struct {
	Root bool
	ID   string
}

func parentOf(block Block) parentIdentity {
	if block.ParentID == nil {
		return parentIdentity{Root: true}
	}
	return parentIdentity{ID: *block.ParentID}
}

func directChildren(blocks []Block) map[parentIdentity][]string {
	children := make(map[parentIdentity][]string)
	for _, block := range blocks {
		parent := parentOf(block)
		children[parent] = append(children[parent], block.BlockID)
	}
	return children
}

func projectIDs(current, wanted []string) []string {
	wantedSet := make(map[string]bool, len(wanted))
	for _, id := range wanted {
		wantedSet[id] = true
	}
	projected := make([]string, 0, len(wanted))
	for _, id := range current {
		if wantedSet[id] {
			projected = append(projected, id)
		}
	}
	return projected
}

func inverseChildOrder(baseIDs, resultIDs, currentIDs []string) []string {
	resultSet := make(map[string]bool, len(resultIDs))
	basePosition := make(map[string]int, len(baseIDs))
	for _, id := range resultIDs {
		resultSet[id] = true
	}
	for i, id := range baseIDs {
		basePosition[id] = i
	}
	gaps := make([][]string, len(resultIDs)+1)
	resultIndex := 0
	for _, id := range currentIDs {
		if resultSet[id] {
			resultIndex++
			continue
		}
		gaps[resultIndex] = append(gaps[resultIndex], id)
	}
	before := make(map[string][]string)
	after := make(map[string][]string)
	unanchored := make([]string, 0)
	for gapIndex, gap := range gaps {
		if len(gap) == 0 {
			continue
		}
		anchored := false
		for i := gapIndex; i < len(resultIDs); i++ {
			if _, ok := basePosition[resultIDs[i]]; ok {
				before[resultIDs[i]] = append(before[resultIDs[i]], gap...)
				anchored = true
				break
			}
		}
		if anchored {
			continue
		}
		for i := gapIndex - 1; i >= 0; i-- {
			if _, ok := basePosition[resultIDs[i]]; ok {
				after[resultIDs[i]] = append(after[resultIDs[i]], gap...)
				anchored = true
				break
			}
		}
		if !anchored {
			unanchored = append(unanchored, gap...)
		}
	}
	ordered := append([]string(nil), unanchored...)
	for _, id := range baseIDs {
		ordered = append(ordered, before[id]...)
		ordered = append(ordered, id)
		ordered = append(ordered, after[id]...)
	}
	return ordered
}

func flattenBlocks(byID map[string]Block, orders map[parentIdentity][]string, base, result, current []Block) ([]Block, error) {
	// Body schema v1 requires only parent-before-child ordering; it does not
	// require a contiguous DFS layout. Rebuild the inverse with a stable
	// topological order so constraints changed by the edit are restored while a
	// valid current non-DFS order is left byte-for-byte stable where possible.
	edges := make(map[string]map[string]bool, len(byID))
	indegree := make(map[string]int, len(byID))
	ordered := make(map[string]bool, len(byID))
	for id := range byID {
		indegree[id] = 0
	}
	addEdge := func(before, after string) error {
		if before == after {
			return Err("undo_conflict", 409)
		}
		if _, ok := byID[before]; !ok {
			return Err("undo_conflict", 409)
		}
		if _, ok := byID[after]; !ok {
			return Err("undo_conflict", 409)
		}
		if edges[before] == nil {
			edges[before] = make(map[string]bool)
		}
		if !edges[before][after] {
			edges[before][after] = true
			indegree[after]++
		}
		return nil
	}
	for parent, ids := range orders {
		for i, id := range ids {
			block, exists := byID[id]
			if !exists || ordered[id] || parentOf(block) != parent {
				return nil, Err("undo_conflict", 409)
			}
			ordered[id] = true
			if i > 0 {
				if err := addEdge(ids[i-1], id); err != nil {
					return nil, err
				}
			}
		}
	}
	for id, block := range byID {
		if !ordered[id] {
			return nil, Err("undo_conflict", 409)
		}
		if block.ParentID != nil {
			if err := addEdge(*block.ParentID, id); err != nil {
				return nil, err
			}
		}
	}

	preference := undoBlockPreference(byID, base, current, structuralUndoAffected(base, result))
	if len(preference) != len(byID) {
		return nil, Err("undo_conflict", 409)
	}
	rank := make(map[string]int, len(preference))
	for i, id := range preference {
		rank[id] = i
	}
	flattened := make([]Block, 0, len(byID))
	emitted := make(map[string]bool, len(byID))
	for len(flattened) < len(byID) {
		chosen := ""
		for id, degree := range indegree {
			if degree != 0 || emitted[id] {
				continue
			}
			if chosen == "" || rank[id] < rank[chosen] || rank[id] == rank[chosen] && id < chosen {
				chosen = id
			}
		}
		if chosen == "" {
			return nil, Err("undo_conflict", 409)
		}
		emitted[chosen] = true
		flattened = append(flattened, cloneBlock(byID[chosen]))
		for after := range edges[chosen] {
			indegree[after]--
		}
	}
	return flattened, nil
}

func undoBlockPreference(byID map[string]Block, base, current []Block, affected map[string]bool) []string {
	preference := make([]string, 0, len(byID))
	present := make(map[string]bool, len(byID))
	for _, block := range current {
		if _, ok := byID[block.BlockID]; ok && !affected[block.BlockID] {
			preference = append(preference, block.BlockID)
			present[block.BlockID] = true
		}
	}
	baseIDs := make([]string, 0, len(base))
	for _, block := range base {
		if _, ok := byID[block.BlockID]; ok {
			baseIDs = append(baseIDs, block.BlockID)
		}
	}
	position := func(id string) int {
		for i := range preference {
			if preference[i] == id {
				return i
			}
		}
		return -1
	}
	for i, id := range baseIDs {
		if present[id] {
			continue
		}
		insertAt := -1
		for before := i - 1; before >= 0; before-- {
			if pos := position(baseIDs[before]); pos >= 0 {
				insertAt = pos + 1
				break
			}
		}
		if insertAt < 0 {
			for after := i + 1; after < len(baseIDs); after++ {
				if pos := position(baseIDs[after]); pos >= 0 {
					insertAt = pos
					break
				}
			}
		}
		if insertAt < 0 {
			insertAt = len(preference)
		}
		preference = slices.Insert(preference, insertAt, id)
		present[id] = true
	}
	return preference
}

func structuralUndoAffected(base, result []Block) map[string]bool {
	baseChildren := directChildren(base)
	resultChildren := directChildren(result)
	parents := make(map[parentIdentity]bool, len(baseChildren)+len(resultChildren))
	for parent := range baseChildren {
		parents[parent] = true
	}
	for parent := range resultChildren {
		parents[parent] = true
	}
	roots := make(map[string]bool)
	for parent := range parents {
		baseIDs, resultIDs := baseChildren[parent], resultChildren[parent]
		stable := longestCommonIDSet(baseIDs, resultIDs)
		for _, id := range baseIDs {
			if !stable[id] {
				roots[id] = true
			}
		}
		for _, id := range resultIDs {
			if !stable[id] {
				roots[id] = true
			}
		}
	}
	affected := make(map[string]bool, len(roots))
	for _, block := range base {
		if roots[block.BlockID] || blockHasAncestor(base, block, roots) {
			affected[block.BlockID] = true
		}
	}
	return affected
}

func longestCommonIDSet(left, right []string) map[string]bool {
	dp := make([][]int, len(left)+1)
	for i := range dp {
		dp[i] = make([]int, len(right)+1)
	}
	for i := len(left) - 1; i >= 0; i-- {
		for j := len(right) - 1; j >= 0; j-- {
			if left[i] == right[j] {
				dp[i][j] = dp[i+1][j+1] + 1
			} else if dp[i+1][j] >= dp[i][j+1] {
				dp[i][j] = dp[i+1][j]
			} else {
				dp[i][j] = dp[i][j+1]
			}
		}
	}
	stable := make(map[string]bool, dp[0][0])
	for i, j := 0, 0; i < len(left) && j < len(right); {
		if left[i] == right[j] {
			stable[left[i]] = true
			i++
			j++
		} else if dp[i+1][j] >= dp[i][j+1] {
			i++
		} else {
			j++
		}
	}
	return stable
}

func hasAncestorInSet(byID map[string]Block, block Block, ancestors map[string]bool) bool {
	seen := make(map[string]bool)
	for block.ParentID != nil {
		if ancestors[*block.ParentID] {
			return true
		}
		if seen[*block.ParentID] {
			return false
		}
		seen[*block.ParentID] = true
		parent, exists := byID[*block.ParentID]
		if !exists {
			return false
		}
		block = parent
	}
	return false
}

func restoreTouchedBlockFields(base, result, current Block) (Block, error) {
	restored := cloneBlock(current)
	if !sameStringPtr(base.ParentID, result.ParentID) {
		if !sameStringPtr(current.ParentID, result.ParentID) {
			return Block{}, Err("undo_conflict", 409)
		}
		restored.ParentID = cloneStringPtr(base.ParentID)
	}
	if base.Type != result.Type {
		if current.Type != result.Type {
			return Block{}, Err("undo_conflict", 409)
		}
		restored.Type = base.Type
	}
	if base.Title != result.Title {
		if current.Title != result.Title {
			return Block{}, Err("undo_conflict", 409)
		}
		restored.Title = base.Title
	}
	if base.Content != result.Content {
		if current.Content != result.Content {
			return Block{}, Err("undo_conflict", 409)
		}
		restored.Content = base.Content
	}
	if base.ClaimOrigin != result.ClaimOrigin {
		if current.ClaimOrigin != result.ClaimOrigin {
			return Block{}, Err("undo_conflict", 409)
		}
		restored.ClaimOrigin = base.ClaimOrigin
	}
	if !slices.Equal(base.EvidenceRefs, result.EvidenceRefs) {
		if !slices.Equal(current.EvidenceRefs, result.EvidenceRefs) {
			return Block{}, Err("undo_conflict", 409)
		}
		restored.EvidenceRefs = slices.Clone(base.EvidenceRefs)
	}
	if !slices.Equal(base.SourceBlockIDs, result.SourceBlockIDs) {
		if !slices.Equal(current.SourceBlockIDs, result.SourceBlockIDs) {
			return Block{}, Err("undo_conflict", 409)
		}
		restored.SourceBlockIDs = slices.Clone(base.SourceBlockIDs)
	}
	return restored, nil
}

func collectEvidenceIDs(bodies ...Body) map[string]bool {
	allowed := make(map[string]bool)
	for _, body := range bodies {
		for _, block := range body.Blocks {
			for _, ref := range block.EvidenceRefs {
				allowed[ref.EvidenceID] = true
			}
		}
	}
	return allowed
}

func blocksByID(blocks []Block) map[string]Block {
	byID := make(map[string]Block, len(blocks))
	for _, block := range blocks {
		byID[block.BlockID] = block
	}
	return byID
}

func diffBodies(before, after Body) PatchDiff {
	diff := PatchDiff{Changes: []PatchChange{}, BlockMappings: []BlockMapping{}}
	if before.Title != after.Title {
		oldTitle, newTitle := before.Title, after.Title
		diff.Counts.Updated++
		diff.Changes = append(diff.Changes, PatchChange{Kind: "title_updated", BeforeTitle: &oldTitle, AfterTitle: &newTitle})
	}
	beforeByID, afterByID := blocksByID(before.Blocks), blocksByID(after.Blocks)
	for i, block := range before.Blocks {
		afterBlock, ok := afterByID[block.BlockID]
		if !ok {
			old := cloneBlock(block)
			diff.Counts.Deleted++
			diff.Changes = append(diff.Changes, PatchChange{Kind: "deleted", BlockID: block.BlockID, Before: &old})
			continue
		}
		newIndex := blockIndex(after.Blocks, block.BlockID)
		contentChanged := !equalBlockIgnoringParent(block, afterBlock)
		moved := !sameStringPtr(block.ParentID, afterBlock.ParentID) || relativeCommonSiblingIndex(before, after, block.BlockID) != relativeCommonSiblingIndex(after, before, block.BlockID)
		if contentChanged {
			old, updated := cloneBlock(block), cloneBlock(afterBlock)
			diff.Counts.Updated++
			diff.Changes = append(diff.Changes, PatchChange{Kind: "updated", BlockID: block.BlockID, Before: &old, After: &updated})
		}
		if moved {
			old, updated := cloneBlock(block), cloneBlock(afterBlock)
			oldIndex, updatedIndex := i, newIndex
			diff.Counts.Moved++
			diff.Changes = append(diff.Changes, PatchChange{Kind: "moved", BlockID: block.BlockID, Before: &old, After: &updated, BeforeIndex: &oldIndex, AfterIndex: &updatedIndex})
		}
	}
	for _, block := range after.Blocks {
		if _, ok := beforeByID[block.BlockID]; ok {
			continue
		}
		added := cloneBlock(block)
		diff.Counts.Added++
		diff.Changes = append(diff.Changes, PatchChange{Kind: "added", BlockID: block.BlockID, After: &added})
	}
	return diff
}

func relativeCommonSiblingIndex(body, other Body, id string) int {
	targetIndex := blockIndex(body.Blocks, id)
	if targetIndex < 0 {
		return -1
	}
	parent := body.Blocks[targetIndex].ParentID
	position := 0
	for _, block := range body.Blocks {
		if !sameStringPtr(block.ParentID, parent) {
			continue
		}
		otherIndex := blockIndex(other.Blocks, block.BlockID)
		if otherIndex < 0 || !sameStringPtr(other.Blocks[otherIndex].ParentID, parent) {
			continue
		}
		if block.BlockID == id {
			return position
		}
		position++
	}
	return -1
}

func equalBlockIgnoringParent(left, right Block) bool {
	left.ParentID, right.ParentID = nil, nil
	return JSON(left) == JSON(right)
}
