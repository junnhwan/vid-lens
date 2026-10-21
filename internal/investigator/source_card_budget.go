package investigator

import (
	"slices"
	"unicode/utf8"

	"vidlens/internal/domain"
)

const adaptiveSourceCardInstructions = `材料卡是全片导航，不是缩写整篇讲稿。按source_card_constraints的target_summary_characters组织主题、关键数字/条件和转折，留出纠错余量，不能超过max_summary_characters。原文始终完整保留，细节在写作/审核时回读；不用把每个例子都塞入卡片。被拒时只纠正当前卡，按返回目标合并重复说明；不能截断原文、改写已保存卡或将未读段标为完成。`
const sourceCardDescription = "Chinese navigation only. Follow source_card_constraints.target_summary_characters; preserve topics, key numbers/conditions and transitions. Originals remain available through read_source. Never replace source evidence with this card."

type sourceCardBounds struct {
	Minimum        int `json:"min_summary_characters"`
	Maximum        int `json:"max_summary_characters"`
	Target         int `json:"target_summary_characters"`
	NavigationMax  int `json:"navigation_max_characters"`
	NavigationUsed int `json:"navigation_saved_characters"`
}

func (s *session) adaptiveSourceCards() bool {
	return s.cp.ExecutionContract != nil && (s.cp.ExecutionContract.Version == domain.ExecutionContractV5 || s.cp.ExecutionContract.Version == domain.ExecutionContractV6 || s.cp.ExecutionContract.Version == domain.ExecutionContractV7 || s.cp.ExecutionContract.Version == domain.ExecutionContractV8)
}

func (s *session) sourceInstructions() string {
	instructions := sourceContextInstructions
	if s.adaptiveSourceCards() {
		instructions += "\n" + adaptiveSourceCardInstructions
	}
	if s.repairsReviewCoverage() {
		instructions += "\n" + reviewCoverageRepairInstructions
	}
	return instructions
}

func sourceNoteMaximum(chunk sourceChunk) int {
	if chunk.NoteMaxCharacters > 0 {
		return chunk.NoteMaxCharacters
	}
	return 1600
}

// Navigation gets at most a quarter of the input character allowance and,
// when configured, a quarter of context at the conservative two tokens/rune
// estimate. The normal call budget still checks the complete physical input.
func (s *session) sourceNavigationLimit() int {
	limit := min(24000, separateInputLimit(s.profile)/4)
	if s.profile.ContextTokens > 0 {
		limit = min(limit, s.profile.ContextTokens/8)
	}
	return max(0, limit)
}

// Keep immutable cards exactly as saved. Reserve enough serialized space for
// every remaining card, and give the model a target with headroom below its
// actual ceiling. This bounds the entire derived ledger, not the originals.
func (s *session) sourceCardLimits(chunk sourceChunk) sourceCardBounds {
	b := sourceCardBounds{Minimum: 30, Maximum: sourceNoteMaximum(chunk), NavigationMax: s.sourceNavigationLimit()}
	b.NavigationUsed = utf8.RuneCount(domain.JSON(s.cp.SourceNotes))
	if len(s.cp.SourceNotes) == 0 {
		b.NavigationUsed = 2 // [] rather than nil's null
	}
	chunks := s.sourceChunks()
	reserve := 0
	for _, future := range chunks[min(len(s.cp.SourceNotes)+1, len(chunks)):] {
		reserve += sourceCardOverhead(future) + b.Minimum
	}
	available := b.NavigationMax - b.NavigationUsed - reserve - sourceCardOverhead(chunk)
	b.Maximum = min(b.Maximum, max(0, available))
	// Match the existing source-survey output reservation. Leave room for
	// the tool envelope and do not promise more Chinese text than can fit.
	b.Maximum = min(b.Maximum, max(0, (min(s.profile.MaxOutputTokens, 8192)-512)/2))
	share := max(0, b.NavigationMax-b.NavigationUsed) / max(1, len(chunks)-len(s.cp.SourceNotes))
	b.Target = min(b.Maximum, max(b.Minimum, min(b.Maximum*3/4, share*3/4)))
	return b
}

func (s *session) sourceCardFitsNavigation(note sourceNote) bool {
	prospective := append(append([]sourceNote{}, s.cp.SourceNotes...), note)
	used := utf8.RuneCount(domain.JSON(prospective))
	for _, future := range s.sourceChunks()[min(len(prospective), len(s.sourceChunks())):] {
		used += sourceCardOverhead(future) + 30
	}
	return used <= s.sourceNavigationLimit()
}

func sourceCardOverhead(chunk sourceChunk) int {
	aliases := []string{}
	for _, p := range chunk.Passages {
		if !slices.Contains(aliases, p.Alias) && len(aliases) < 12 {
			aliases = append(aliases, p.Alias)
		}
	}
	// Include the comma between cards. Reserve the maximum legal reference
	// set even when a card ultimately selects fewer original aliases.
	return utf8.RuneCount(domain.JSON(sourceNote{ChunkID: chunk.ID, Summary: "", EvidenceIDs: aliases})) + 1
}

func (s *session) checkSourceCardCapacity() error {
	if s.adaptiveSourceCards() && s.sourceSurveyPending() {
		chunk := s.sourceChunks()[len(s.cp.SourceNotes)]
		if s.sourceCardLimits(chunk).Maximum < 30 {
			return budgetProblem("source_navigation_exceeded", "已保存材料卡与剩余导航超过当前冻结输入预算；原文、合法卡及草稿完整保留，未跳过剩余来源。")
		}
	}
	return nil
}
