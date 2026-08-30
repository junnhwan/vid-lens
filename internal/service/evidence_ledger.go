package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"vid-lens/internal/model"
	"vid-lens/internal/repository"
)

type EvidenceLedgerService struct {
	repos *repository.Repositories
}

type EvidenceLedgerRecordRequest struct {
	UserID    int64
	SessionID int64
	MessageID int64
	TaskID    int64
	RunID     string
	RawAnswer string
	Evidence  []Citation // candidates whose Cn identifiers appear in RawAnswer
	Retrieved []Citation // all retrieval artifacts observed during the run
}

type EvidenceLedgerView struct {
	RunID         string                     `json:"run_id"`
	Claims        []model.AgentClaim         `json:"claims"`
	Evidence      []model.AgentEvidence      `json:"evidence"`
	ClaimEvidence []model.AgentClaimEvidence `json:"claim_evidence"`
}

type EvidenceClaimCorrectionRequest struct {
	Text   string `json:"text"`
	Reason string `json:"reason"`
}

type EvidenceHypothesisRequest struct {
	UserID     int64
	SessionID  int64
	MessageID  int64
	RunID      string
	Text       string
	Confidence float64
}

func NewEvidenceLedgerService(repos *repository.Repositories) *EvidenceLedgerService {
	return &EvidenceLedgerService{repos: repos}
}

// RecordAnswer extracts only user-visible answer statements and citation
// bindings. It persists no prompt, planner scratchpad, or chain-of-thought.
func (s *EvidenceLedgerService) RecordAnswer(ctx context.Context, req EvidenceLedgerRecordRequest) error {
	req.RunID, req.RawAnswer = strings.TrimSpace(req.RunID), strings.TrimSpace(req.RawAnswer)
	if s == nil || s.repos == nil || s.repos.EvidenceLedger == nil || req.UserID <= 0 || req.SessionID <= 0 || req.MessageID <= 0 || req.TaskID <= 0 || req.RunID == "" || req.RawAnswer == "" {
		return gorm.ErrInvalidData
	}
	if err := validateLedgerEvidenceTask(req.TaskID, req.Evidence, "Evidence"); err != nil {
		return err
	}
	if err := validateLedgerEvidenceTask(req.TaskID, req.Retrieved, "Retrieved"); err != nil {
		return err
	}

	now := time.Now().UTC()
	artifacts := make([]model.AgentEvidence, 0, len(req.Evidence)+len(req.Retrieved))
	byCitationID := make(map[string]model.AgentEvidence, len(req.Evidence))
	bySourceRef := make(map[string]model.AgentEvidence, len(req.Evidence)+len(req.Retrieved))
	allEvidence := append(append([]Citation(nil), req.Retrieved...), req.Evidence...)
	for _, citation := range allEvidence {
		artifact, err := s.buildEvidence(req, citation, now)
		if err != nil {
			return err
		}
		if _, exists := bySourceRef[artifact.SourceRef]; exists {
			continue
		}
		bySourceRef[artifact.SourceRef] = artifact
		artifacts = append(artifacts, artifact)
	}
	for i, citation := range req.Evidence {
		citationID := strings.TrimSpace(citation.CitationID)
		if citationID == "" {
			citationID = fmt.Sprintf("C%d", i+1)
		}
		sourceRef := strings.TrimSpace(citation.EvidenceID)
		if sourceRef == "" {
			sourceRef = fmt.Sprintf("task:%d:chunk:%d:index:%d", citation.TaskID, citation.ChunkID, citation.ChunkIndex)
		}
		byCitationID[strings.ToUpper(citationID)] = bySourceRef[sourceRef]
	}

	statements := splitAnswerClaims(req.RawAnswer)
	claims := make([]model.AgentClaim, 0, len(statements))
	links := make([]model.AgentClaimEvidence, 0, len(statements))
	for i, statement := range statements {
		claim, claimLinks := buildLedgerClaim(req, statement, i, byCitationID, now)
		claims = append(claims, claim)
		links = append(links, claimLinks...)
	}
	if len(claims) == 0 {
		return nil
	}
	return s.repos.EvidenceLedger.Append(ctx, repository.EvidenceLedgerBatch{Claims: claims, Evidence: artifacts, Links: links})
}

func validateLedgerEvidenceTask(taskID int64, citations []Citation, boundary string) error {
	for _, citation := range citations {
		if citation.TaskID != taskID {
			return fmt.Errorf("evidence ledger %s item does not belong to current task", boundary)
		}
	}
	return nil
}

func (s *EvidenceLedgerService) GetRun(ctx context.Context, userID int64, runID string) (*EvidenceLedgerView, error) {
	if s == nil || s.repos == nil || s.repos.EvidenceLedger == nil {
		return nil, gorm.ErrInvalidDB
	}
	records, err := s.repos.EvidenceLedger.ListRun(ctx, userID, runID)
	if err != nil {
		return nil, err
	}
	if len(records.Claims) == 0 && len(records.Evidence) == 0 {
		return nil, nil
	}
	return &EvidenceLedgerView{RunID: strings.TrimSpace(runID), Claims: records.Claims, Evidence: records.Evidence, ClaimEvidence: records.Links}, nil
}

func (s *EvidenceLedgerService) CorrectClaim(ctx context.Context, userID int64, claimID string, req EvidenceClaimCorrectionRequest) (*model.AgentClaim, error) {
	if s == nil || s.repos == nil || s.repos.EvidenceLedger == nil {
		return nil, gorm.ErrInvalidDB
	}
	if strings.TrimSpace(req.Text) == "" || strings.TrimSpace(req.Reason) == "" {
		return nil, gorm.ErrInvalidData
	}
	return s.repos.EvidenceLedger.AppendCorrection(ctx, userID, claimID, req.Text, req.Reason)
}

func (s *EvidenceLedgerService) CreateHypothesis(ctx context.Context, req EvidenceHypothesisRequest) (*model.AgentClaim, error) {
	req.RunID, req.Text = strings.TrimSpace(req.RunID), strings.TrimSpace(req.Text)
	if s == nil || s.repos == nil || s.repos.EvidenceLedger == nil || req.UserID <= 0 || req.SessionID <= 0 || req.MessageID <= 0 || req.RunID == "" || req.Text == "" || req.Confidence < 0 || req.Confidence > 1 {
		return nil, gorm.ErrInvalidData
	}
	id := deterministicUUID("hypothesis", strconv.FormatInt(req.UserID, 10), req.RunID, req.Text)
	claim := model.AgentClaim{
		ID: id, RootClaimID: id, Revision: 1, UserID: req.UserID, SessionID: req.SessionID, MessageID: req.MessageID,
		RunID: req.RunID, Kind: "hypothesis", Text: req.Text, Status: model.ClaimStatusHypothesized,
		Confidence: req.Confidence, ValidationNote: "awaiting evidence verification", CreatedAt: time.Now().UTC(),
	}
	if err := s.repos.EvidenceLedger.Append(ctx, repository.EvidenceLedgerBatch{Claims: []model.AgentClaim{claim}}); err != nil {
		return nil, err
	}
	return &claim, nil
}

func (s *EvidenceLedgerService) buildEvidence(req EvidenceLedgerRecordRequest, citation Citation, now time.Time) (model.AgentEvidence, error) {
	sourceRef := strings.TrimSpace(citation.EvidenceID)
	if sourceRef == "" {
		sourceRef = fmt.Sprintf("task:%d:chunk:%d:index:%d", citation.TaskID, citation.ChunkID, citation.ChunkIndex)
	}
	taskID := citation.TaskID
	if taskID <= 0 {
		taskID = req.TaskID
	}
	quote := strings.TrimSpace(citation.Content)
	artifact := model.AgentEvidence{
		ID:                   deterministicUUID("evidence", strconv.FormatInt(req.UserID, 10), req.RunID, sourceRef),
		UserID:               req.UserID,
		RunID:                req.RunID,
		SourceRef:            sourceRef,
		SourceType:           "rag_chunk",
		TaskID:               taskID,
		DocumentID:           fmt.Sprintf("video_chunk:%d", citation.ChunkID),
		TimeRangeStatus:      model.EvidenceTimeRangeUnknown,
		QuoteText:            quote,
		ContentHash:          sha256Hex(quote),
		SourceRevision:       "",
		SourceRevisionStatus: model.EvidenceSourceRevisionUnavailable,
		CreatedAt:            now,
		UpdatedAt:            now,
	}
	locator := map[string]any{
		"task_id": taskID, "chunk_id": citation.ChunkID, "chunk_index": citation.ChunkIndex,
		"evidence_id": sourceRef, "source_ref_kind": "rag_evidence_id", "retrieval_source": citation.Source,
		"source_revision_status": model.EvidenceSourceRevisionUnavailable,
	}
	visualSource := isVisualOCRCitation(citation)
	if visualSource {
		artifact.SourceType = "visual_ocr"
		artifact.DocumentID = sourceRef
		locator["source_artifact"] = sourceRef
	}
	if resolved, ok, err := s.resolveTimeRange(taskID, citation); err != nil {
		return model.AgentEvidence{}, err
	} else if ok {
		artifact.SourceType = resolved.sourceType
		artifact.DocumentID = resolved.documentID
		locator["source_artifact"] = resolved.documentID
		locator["range_basis"] = resolved.rangeBasis
		for key, value := range resolved.locator {
			locator[key] = value
		}
		if resolved.endSecond > resolved.startSecond && resolved.startSecond >= 0 {
			artifact.StartSecond = resolved.startSecond
			artifact.EndSecond = resolved.endSecond
			artifact.TimeRangeStatus = model.EvidenceTimeRangeKnown
		}
	}
	locator["time_range_status"] = artifact.TimeRangeStatus
	encodedLocator, err := json.Marshal(locator)
	if err != nil {
		return model.AgentEvidence{}, err
	}
	artifact.StableLocator = string(encodedLocator)
	return artifact, nil
}

type resolvedEvidenceRange struct {
	sourceType  string
	documentID  string
	startSecond int64
	endSecond   int64
	rangeBasis  string
	locator     map[string]any
}

func (s *EvidenceLedgerService) resolveTimeRange(taskID int64, citation Citation) (resolvedEvidenceRange, bool, error) {
	quote := strings.TrimSpace(citation.Content)
	if taskID <= 0 {
		return resolvedEvidenceRange{}, false, nil
	}
	if isVisualOCRCitation(citation) {
		if quote == "" {
			return resolvedEvidenceRange{}, false, errors.New("visual_ocr evidence requires OCR text")
		}
		if _, ok := visualFrameIDFromSourceRef(citation.EvidenceID); !ok {
			return resolvedEvidenceRange{}, false, errors.New("visual_ocr evidence requires visual-frame:<id> provenance")
		}
		return s.resolveVisualRange(taskID, citation.EvidenceID, quote)
	}
	if quote == "" {
		return resolvedEvidenceRange{}, false, nil
	}
	return s.resolveTranscriptRange(taskID, citation, quote)
}

func (s *EvidenceLedgerService) resolveTranscriptRange(taskID int64, citation Citation, quote string) (resolvedEvidenceRange, bool, error) {
	if s.repos.TranscriptionChunk == nil {
		return resolvedEvidenceRange{}, false, nil
	}
	chunks, err := s.repos.TranscriptionChunk.ListByTaskID(taskID)
	if err != nil {
		return resolvedEvidenceRange{}, false, err
	}
	targetIndex, identityPresent, identityResolved, identityBasis, err := s.resolveTranscriptIdentity(taskID, citation)
	if err != nil {
		return resolvedEvidenceRange{}, false, err
	}
	if identityPresent {
		if !identityResolved {
			return resolvedEvidenceRange{}, false, nil
		}
		for _, chunk := range chunks {
			if chunk.ChunkIndex != targetIndex || chunk.Status != model.TranscriptionChunkStatusCompleted || !evidenceTextMatches(quote, chunk.Content) {
				continue
			}
			return transcriptResolvedRange(chunk, identityBasis), true, nil
		}
		return resolvedEvidenceRange{}, false, nil
	}

	// Legacy evidence without any stable identity may use text only when it
	// points to exactly one completed ASR segment. Duplicate text is ambiguous.
	matches := make([]model.VideoTranscriptionChunk, 0, 1)
	for _, chunk := range chunks {
		if chunk.Status == model.TranscriptionChunkStatusCompleted && evidenceTextMatches(quote, chunk.Content) {
			matches = append(matches, chunk)
		}
	}
	if len(matches) != 1 {
		return resolvedEvidenceRange{}, false, nil
	}
	return transcriptResolvedRange(matches[0], "unique_text_fallback"), true, nil
}

func (s *EvidenceLedgerService) resolveTranscriptIdentity(taskID int64, citation Citation) (int, bool, bool, string, error) {
	evidenceID := strings.TrimSpace(citation.EvidenceID)
	hasEvidenceID := evidenceID != ""
	hasChunkID := citation.ChunkID > 0
	// ChunkIndex is an int in the compatibility citation schema, so zero cannot
	// distinguish an omitted value from the first chunk. Real retrieval records
	// carry EvidenceID/ChunkID; a non-zero standalone index remains explicit.
	hasChunkIndex := citation.ChunkIndex != 0
	identityPresent := hasEvidenceID || hasChunkID || hasChunkIndex
	if !identityPresent {
		return 0, false, false, "", nil
	}

	resolved := make(map[int][]string)
	if hasEvidenceID || hasChunkID {
		if s.repos.VideoChunk == nil {
			return 0, true, false, "", nil
		}
		chunks, err := s.repos.VideoChunk.ListAllByTaskID(taskID)
		if err != nil {
			return 0, true, false, "", err
		}
		for _, chunk := range chunks {
			if hasEvidenceID && chunk.VectorID == evidenceID {
				resolved[chunk.ChunkIndex] = append(resolved[chunk.ChunkIndex], "evidence_id")
			}
			if hasChunkID && chunk.ID == citation.ChunkID {
				resolved[chunk.ChunkIndex] = append(resolved[chunk.ChunkIndex], "chunk_id")
			}
		}
	}
	if hasChunkIndex {
		resolved[citation.ChunkIndex] = append(resolved[citation.ChunkIndex], "chunk_index")
	}
	if len(resolved) == 0 {
		return 0, true, false, "", nil
	}
	if len(resolved) != 1 {
		return 0, true, false, "", errors.New("transcript evidence identities resolve to different chunks")
	}
	for index, bases := range resolved {
		sort.Strings(bases)
		return index, true, true, strings.Join(bases, "+"), nil
	}
	return 0, true, false, "", nil
}

func transcriptResolvedRange(chunk model.VideoTranscriptionChunk, identityBasis string) resolvedEvidenceRange {
	resolved := resolvedEvidenceRange{
		sourceType: "transcript", documentID: fmt.Sprintf("transcription_chunk:%d", chunk.ID), rangeBasis: "unavailable",
		locator: map[string]any{"transcription_chunk_id": chunk.ID, "transcription_chunk_index": chunk.ChunkIndex, "identity_basis": identityBasis},
	}
	start, end := int64(chunk.StartSecond), int64(chunk.EndSecond)
	if end > start && start >= 0 {
		resolved.startSecond, resolved.endSecond, resolved.rangeBasis = start, end, "persisted_asr_segment"
	}
	return resolved
}

func (s *EvidenceLedgerService) resolveVisualRange(taskID int64, sourceRef, quote string) (resolvedEvidenceRange, bool, error) {
	expectedFrameID, hasFrameID := visualFrameIDFromSourceRef(sourceRef)
	if !hasFrameID {
		return resolvedEvidenceRange{}, false, errors.New("visual_ocr evidence requires visual-frame:<id> provenance")
	}
	if s.repos.VisualFrame == nil {
		return resolvedEvidenceRange{}, false, errors.New("visual_ocr frame repository unavailable")
	}
	frames, err := s.repos.VisualFrame.ListCompletedWithText(taskID)
	if err != nil {
		return resolvedEvidenceRange{}, false, err
	}
	for _, frame := range frames {
		if frame.ID != expectedFrameID {
			continue
		}
		if !evidenceTextMatches(quote, frame.OCRText) {
			continue
		}
		start := frame.TimeMs / 1000
		return resolvedEvidenceRange{
			sourceType: "visual_ocr", documentID: fmt.Sprintf("visual_frame:%d", frame.ID),
			startSecond: start, endSecond: start + 1, rangeBasis: "keyframe_timestamp",
			locator: map[string]any{
				"frame_id": frame.ID, "frame_index": frame.FrameIndex, "time_ms": frame.TimeMs,
				"object_key": frame.ObjectKey, "frame_source": frame.Source, "caption_method": frame.CaptionMethod,
			},
		}, true, nil
	}
	return resolvedEvidenceRange{}, false, errors.New("visual_ocr evidence does not match an existing completed frame")
}

func isVisualOCRCitation(citation Citation) bool {
	source := strings.ToLower(strings.TrimSpace(citation.Source))
	return source == "visual_ocr" || strings.HasPrefix(strings.ToLower(strings.TrimSpace(citation.EvidenceID)), "visual-frame:")
}

func visualFrameIDFromSourceRef(sourceRef string) (int64, bool) {
	const prefix = "visual-frame:"
	sourceRef = strings.ToLower(strings.TrimSpace(sourceRef))
	if !strings.HasPrefix(sourceRef, prefix) {
		return 0, false
	}
	id, err := strconv.ParseInt(strings.TrimSpace(strings.TrimPrefix(sourceRef, prefix)), 10, 64)
	return id, err == nil && id > 0
}

func evidenceTextMatches(quote, source string) bool {
	quote, source = strings.TrimSpace(quote), strings.TrimSpace(source)
	if quote == "" || source == "" {
		return false
	}
	return strings.Contains(source, quote) || strings.Contains(quote, source)
}

type answerClaimStatement struct {
	text       string
	references []string
}

func splitAnswerClaims(raw string) []answerClaimStatement {
	fragments := splitAnswerFragments(raw)
	statements := make([]answerClaimStatement, 0, len(fragments))
	for _, fragment := range fragments {
		visible := normalizeClaimText(stripCitationTokensVisible(fragment))
		if visible == "" {
			continue
		}
		referencedSet := parseReferencedCitationIDs(fragment)
		references := make([]string, 0, len(referencedSet))
		for ref := range referencedSet {
			references = append(references, strings.ToUpper(ref))
		}
		sort.Strings(references)
		statements = append(statements, answerClaimStatement{text: visible, references: references})
	}
	return statements
}

func splitAnswerFragments(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	fragments := make([]string, 0, 8)
	start := 0
	for offset := 0; offset < len(raw); {
		r, size := utf8.DecodeRuneInString(raw[offset:])
		offset += size
		if !isClaimBoundary(r) {
			continue
		}
		end := includeTrailingCitationTokens(raw, offset)
		if fragment := strings.TrimSpace(raw[start:end]); fragment != "" {
			fragments = append(fragments, fragment)
		}
		start, offset = end, end
	}
	if start < len(raw) {
		if fragment := strings.TrimSpace(raw[start:]); fragment != "" {
			fragments = append(fragments, fragment)
		}
	}
	return fragments
}

func isClaimBoundary(r rune) bool {
	switch r {
	case '。', '！', '？', '!', '?', '\n':
		return true
	default:
		return false
	}
}

func includeTrailingCitationTokens(text string, offset int) int {
	end := offset
	for {
		cursor := end
		for cursor < len(text) {
			r, size := utf8.DecodeRuneInString(text[cursor:])
			if r == '\n' || !unicode.IsSpace(r) {
				break
			}
			cursor += size
		}
		if cursor+3 > len(text) || text[cursor] != '[' || (text[cursor+1] != 'C' && text[cursor+1] != 'c') {
			return end
		}
		close := strings.IndexByte(text[cursor:], ']')
		if close < 0 {
			return end
		}
		close += cursor
		if _, ok := normalizeCitationID(text[cursor+1 : close]); !ok {
			return end
		}
		end = close + 1
	}
}

func normalizeClaimText(text string) string {
	text = strings.TrimSpace(text)
	text = strings.TrimLeft(text, "#*-+ \t")
	for len(text) > 0 && text[0] >= '0' && text[0] <= '9' {
		text = text[1:]
	}
	text = strings.TrimLeft(text, ".)、 \t")
	if text == "" || text == ":" || text == "：" {
		return ""
	}
	return text
}

func buildLedgerClaim(req EvidenceLedgerRecordRequest, statement answerClaimStatement, index int, evidence map[string]model.AgentEvidence, now time.Time) (model.AgentClaim, []model.AgentClaimEvidence) {
	rootID := deterministicUUID("claim", strconv.FormatInt(req.UserID, 10), req.RunID, strconv.Itoa(index), statement.text)
	claim := model.AgentClaim{
		ID: rootID, RootClaimID: rootID, Revision: 1, UserID: req.UserID, SessionID: req.SessionID,
		MessageID: req.MessageID, RunID: req.RunID, Kind: "answer_fact", Text: statement.text, CreatedAt: now,
	}
	links := make([]model.AgentClaimEvidence, 0, len(statement.references))
	missingReference, unknownRange := false, false
	for _, ref := range statement.references {
		artifact, ok := evidence[ref]
		if !ok {
			missingReference = true
			continue
		}
		bindingStatus, reason := model.ClaimStatusVerified, "explicit citation binding has a stable source and replayable time range; semantic entailment was not evaluated"
		if artifact.TimeRangeStatus != model.EvidenceTimeRangeKnown {
			bindingStatus, reason, unknownRange = model.ClaimStatusUncertain, "stable source resolved but time range is unavailable", true
		}
		links = append(links, model.AgentClaimEvidence{
			ClaimID: claim.ID, EvidenceID: artifact.ID, Relation: model.ClaimEvidenceSupports,
			VerificationStatus: bindingStatus, ValidationReason: reason, CreatedAt: now,
		})
	}
	switch {
	case len(statement.references) == 0 && containsUncertaintyMarker(statement.text):
		claim.Status, claim.Confidence, claim.ValidationNote = model.ClaimStatusUncertain, 0.35, "answer explicitly expresses uncertainty and has no evidence binding"
	case len(statement.references) == 0:
		claim.Status, claim.Confidence, claim.ValidationNote = model.ClaimStatusUnsupported, 0, "answer fact has no evidence binding"
	case missingReference:
		claim.Status, claim.Confidence, claim.ValidationNote = model.ClaimStatusUnsupported, 0, "answer references evidence outside the persisted retrieval set"
	case unknownRange:
		claim.Status, claim.Confidence, claim.ValidationNote = model.ClaimStatusUncertain, 0.5, "evidence is stable but its video time range could not be resolved"
	default:
		claim.Status, claim.Confidence, claim.ValidationNote = model.ClaimStatusVerified, 0.9, "all explicit citation bindings have stable references and replayable time ranges; natural-language semantic truth was not evaluated"
	}
	return claim, links
}

func containsUncertaintyMarker(text string) bool {
	lower := strings.ToLower(text)
	for _, marker := range []string{"可能", "也许", "或许", "推测", "不确定", "尚不清楚", "无法确认", "uncertain", "possibly", "maybe", "likely"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func deterministicUUID(parts ...string) string {
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte(strings.Join(parts, "\x00"))).String()
}

func sha256Hex(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
