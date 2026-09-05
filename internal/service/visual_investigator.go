package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/google/uuid"
	"vid-lens/internal/ai"
	"vid-lens/internal/model"
	"vid-lens/internal/pkg/ffmpeg"
	"vid-lens/internal/repository"
	"vid-lens/internal/storage"
)

const (
	queryVisualCapturePolicyVersion = "query-frame-v1"
	queryVisualPromptVersion        = "query-observation-v1"
	queryVisualSource               = "query_frame"
)

// VisualInvestigator is the narrow query-time visual evidence port. Scope and
// the source video are resolved by the server; callers only provide a goal and
// windows that were already located in the canonical video evidence.
type VisualInvestigator interface {
	Inspect(context.Context, InspectRequest) (Investigation, error)
}

type VisualTimeRange struct {
	StartMS int64 `json:"start_ms"`
	EndMS   int64 `json:"end_ms"`
}

type RequiredFact struct {
	Name string `json:"name"`
}

type VisualBudget struct {
	MaxWindows  int   `json:"max_windows"`
	MaxFrames   int   `json:"max_frames"`
	MaxVLMCalls int   `json:"max_vlm_calls"`
	MaxWindowMS int64 `json:"max_window_ms"`
	MaxTotalMS  int64 `json:"max_total_ms"`
}

func DefaultVisualInvestigatorBudget() VisualBudget {
	return VisualBudget{
		MaxWindows: 3, MaxFrames: 8, MaxVLMCalls: 8,
		MaxWindowMS: 2 * 60 * 1000, MaxTotalMS: 6 * 60 * 1000,
	}
}

func (b VisualBudget) normalized() VisualBudget {
	d := DefaultVisualInvestigatorBudget()
	if b.MaxWindows <= 0 {
		b.MaxWindows = d.MaxWindows
	}
	if b.MaxFrames <= 0 {
		b.MaxFrames = d.MaxFrames
	}
	if b.MaxVLMCalls <= 0 {
		b.MaxVLMCalls = d.MaxVLMCalls
	}
	if b.MaxWindowMS <= 0 {
		b.MaxWindowMS = d.MaxWindowMS
	}
	if b.MaxTotalMS <= 0 {
		b.MaxTotalMS = d.MaxTotalMS
	}
	return b
}

type InspectRequest struct {
	// UserID and TaskID are injected by the server-facing tool adapter. They
	// are kept in the domain request so direct service callers get the same
	// scope checks.
	UserID        int64             `json:"-"`
	TaskID        int64             `json:"-"`
	Goal          string            `json:"goal"`
	RequiredFacts []RequiredFact    `json:"required_facts,omitempty"`
	SeedWindows   []VisualTimeRange `json:"seed_windows"`
	Budget        VisualBudget      `json:"budget"`
	TraceRef      string            `json:"-"`
}

type VisualBudgetUsage struct {
	WindowsSelected int    `json:"windows_selected"`
	FramesCaptured  int    `json:"frames_captured"`
	FramesReused    int    `json:"frames_reused"`
	VLMCalls        int    `json:"vlm_calls"`
	BytesUploaded   int64  `json:"bytes_uploaded"`
	CostMicros      int64  `json:"cost_micros"`
	CostSource      string `json:"cost_source"`
	BudgetExhausted bool   `json:"budget_exhausted"`
}

type VisualObservation struct {
	ID                   string   `json:"id"`
	TaskID               int64    `json:"task_id"`
	VideoRevision        string   `json:"video_revision"`
	FrameRef             string   `json:"frame_ref"`
	ArtifactKind         string   `json:"artifact_kind"`
	FFmpegArgs           []string `json:"ffmpeg_args,omitempty"`
	ObjectKey            string   `json:"object_key,omitempty"`
	StartMS              int64    `json:"start_ms"`
	EndMS                int64    `json:"end_ms"`
	Source               string   `json:"source"`
	CapturePolicyVersion string   `json:"capture_policy_version"`
	Model                string   `json:"model,omitempty"`
	PromptVersion        string   `json:"prompt_version"`
	Observation          string   `json:"observation,omitempty"`
	StructuredFacts      []string `json:"structured_facts,omitempty"`
	Gaps                 []string `json:"gaps,omitempty"`
	RawResponseHash      string   `json:"raw_response_hash,omitempty"`
	Status               string   `json:"status"`
	Error                string   `json:"error,omitempty"`
}

type ClaimEvidenceBinding struct {
	Fact           string `json:"fact"`
	ObservationID  string `json:"observation_id"`
	Relation       string `json:"relation"`
	Status         string `json:"status"`
	ValidationNote string `json:"validation_note,omitempty"`
}

type EvidenceGap struct {
	Kind   string `json:"kind"`
	Detail string `json:"detail,omitempty"`
}

type Investigation struct {
	Observations   []VisualObservation    `json:"observations"`
	ClaimBindings  []ClaimEvidenceBinding `json:"claim_bindings,omitempty"`
	UnresolvedGaps []EvidenceGap          `json:"unresolved_gaps,omitempty"`
	Status         string                 `json:"status"`
	TraceRef       string                 `json:"trace_ref"`
	Budget         VisualBudgetUsage      `json:"budget"`
}

// QueryVisualFrameMaterializer is injectable so policy and provenance can be
// tested without a real FFmpeg process. Production uses the FFmpeg adapter.
type QueryVisualFrameMaterializer func(context.Context, string, string, []int64) ([]ffmpeg.KeyFrame, string, error)
type QueryVisualVideoDownloader func(context.Context, string) (string, error)
type QueryVisualFrameUploader func(context.Context, string, string, string) (int64, error)

type QueryVisualInvestigator struct {
	repos         *repository.Repositories
	ffmpeg        string
	materialize   QueryVisualFrameMaterializer
	download      QueryVisualVideoDownloader
	upload        QueryVisualFrameUploader
	resolveVision func(context.Context, int64) (ai.VisionClient, error)
	resolveModel  func(context.Context, int64) (string, error)
}

func NewVisualInvestigator(repos *repository.Repositories, store *storage.MinIOStorage, ffmpegPath string) *QueryVisualInvestigator {
	if ffmpegPath == "" {
		ffmpegPath = "ffmpeg"
	}
	investigator := &QueryVisualInvestigator{
		repos:       repos,
		ffmpeg:      ffmpegPath,
		materialize: ffmpeg.ExtractFramesAtTimes,
	}
	if store != nil {
		investigator.download = store.DownloadToTemp
		investigator.upload = func(ctx context.Context, path, objectKey, contentType string) (int64, error) {
			return store.UploadFromPath(ctx, path, objectKey, contentType)
		}
	}
	return investigator
}

func (s *QueryVisualInvestigator) SetVisionResolver(fn func(context.Context, int64) (ai.VisionClient, error)) {
	if s != nil {
		s.resolveVision = fn
	}
}

func (s *QueryVisualInvestigator) SetVisionModelResolver(fn func(context.Context, int64) (string, error)) {
	if s != nil {
		s.resolveModel = fn
	}
}

func (s *QueryVisualInvestigator) SetFrameMaterializer(fn QueryVisualFrameMaterializer) {
	if s != nil && fn != nil {
		s.materialize = fn
	}
}

func (s *QueryVisualInvestigator) SetVideoDownloader(fn QueryVisualVideoDownloader) {
	if s != nil {
		s.download = fn
	}
}

func (s *QueryVisualInvestigator) SetFrameUploader(fn QueryVisualFrameUploader) {
	if s != nil {
		s.upload = fn
	}
}

func (s *QueryVisualInvestigator) Inspect(ctx context.Context, req InspectRequest) (Investigation, error) {
	if s == nil || s.repos == nil || s.repos.Task == nil {
		return Investigation{}, errors.New("visual investigator repository unavailable")
	}
	req.Goal = strings.TrimSpace(req.Goal)
	if req.UserID <= 0 || req.TaskID <= 0 {
		return Investigation{}, errors.New("visual investigator scope is invalid")
	}
	if req.Goal == "" {
		return Investigation{}, errors.New("visual investigator goal is empty")
	}
	if len(req.SeedWindows) == 0 {
		return Investigation{}, errors.New("visual investigator requires seed windows")
	}
	task, err := s.repos.Task.FindByID(req.TaskID)
	if err != nil {
		return Investigation{}, err
	}
	if task.UserID != req.UserID {
		return Investigation{}, errors.New("visual investigator task scope mismatch")
	}
	if strings.TrimSpace(task.FileURL) == "" {
		return Investigation{}, errors.New("visual investigator source video is unavailable")
	}

	budget := req.Budget.normalized()
	windows, budgetExhausted, err := s.selectSeedWindows(req.TaskID, req.SeedWindows, budget)
	if err != nil {
		return Investigation{}, err
	}
	if s.download == nil || s.upload == nil || s.materialize == nil {
		return Investigation{}, errors.New("visual investigator media materialization is unavailable")
	}
	traceRef := strings.TrimSpace(req.TraceRef)
	if traceRef == "" {
		traceRef = "visual-investigation:" + uuid.NewString()
	}
	usage := VisualBudgetUsage{WindowsSelected: len(windows), CostSource: "unknown", BudgetExhausted: budgetExhausted}
	result := Investigation{Observations: []VisualObservation{}, ClaimBindings: []ClaimEvidenceBinding{}, UnresolvedGaps: []EvidenceGap{}, TraceRef: traceRef, Budget: usage}
	if len(windows) == 0 {
		result.Status = "budget_exhausted"
		result.UnresolvedGaps = append(result.UnresolvedGaps, EvidenceGap{Kind: "location_missing", Detail: "no seed window fit the visual investigation budget"})
		return result, nil
	}

	times, timeBudgetExhausted, err := s.selectFrameTimes(req.TaskID, windows, budget.MaxFrames)
	if err != nil {
		return Investigation{}, err
	}
	if timeBudgetExhausted {
		usage.BudgetExhausted = true
	}
	if len(times) == 0 {
		result.Status = "uncertain"
		result.UnresolvedGaps = append(result.UnresolvedGaps, EvidenceGap{Kind: "location_missing", Detail: "seed windows contained no sampleable time"})
		result.Budget = usage
		return result, nil
	}

	videoPath, err := s.download(ctx, task.FileURL)
	if err != nil {
		return Investigation{}, fmt.Errorf("download source video for visual investigation: %w", err)
	}
	defer os.Remove(videoPath)

	vision, visionErr := s.resolveVisionClient(ctx, req.UserID)
	modelName := s.resolveVisionModel(ctx, req.UserID)
	if vision == nil {
		result.UnresolvedGaps = append(result.UnresolvedGaps, EvidenceGap{Kind: "vision_unavailable", Detail: errString(visionErr)})
	}
	frames, workDir, err := s.materialize(ctx, s.ffmpeg, videoPath, times)
	if err != nil {
		return Investigation{}, fmt.Errorf("materialize query-time visual frames: %w", err)
	}
	if workDir != "" {
		defer os.RemoveAll(workDir)
	}

	for _, frame := range frames {
		if err := ctx.Err(); err != nil {
			return Investigation{}, err
		}
		allowVLM := vision != nil && usage.VLMCalls < budget.MaxVLMCalls
		if vision != nil && !allowVLM {
			usage.BudgetExhausted = true
		}
		observation, reused, uploaded, obsErr := s.inspectFrame(ctx, req, task, frame, vision, modelName, allowVLM, traceRef)
		if obsErr != nil {
			return Investigation{}, obsErr
		}
		usage.BytesUploaded += uploaded
		if reused {
			usage.FramesReused++
		} else {
			usage.FramesCaptured++
		}
		if !reused && observation.Status == model.VisualObservationStatusObserved {
			usage.VLMCalls++
		} else if !reused && observation.Status == model.VisualObservationStatusFailed && observation.Error != "" && vision != nil {
			if allowVLM {
				usage.VLMCalls++
			}
		}
		result.Observations = append(result.Observations, observation)
		if observation.Status == model.VisualObservationStatusFailed {
			result.UnresolvedGaps = append(result.UnresolvedGaps, EvidenceGap{Kind: "observation_failed", Detail: observation.Error})
		}
		for _, gap := range observation.Gaps {
			result.UnresolvedGaps = append(result.UnresolvedGaps, EvidenceGap{Kind: "semantic_gap", Detail: gap})
		}
	}
	if vision != nil && usage.VLMCalls >= budget.MaxVLMCalls && len(result.Observations) < len(times) {
		usage.BudgetExhausted = true
		result.UnresolvedGaps = append(result.UnresolvedGaps, EvidenceGap{Kind: "visual_budget_exhausted", Detail: "maximum VLM calls reached"})
	}
	result.ClaimBindings = bindObservedFacts(req.RequiredFacts, result.Observations)
	if usage.BudgetExhausted {
		result.Status = "budget_exhausted"
	} else if len(result.Observations) == 0 || vision == nil || hasFailedObservation(result.Observations) {
		result.Status = "uncertain"
	} else {
		result.Status = "sufficient"
	}
	result.Budget = usage
	return result, nil
}

func (s *QueryVisualInvestigator) selectSeedWindows(taskID int64, requested []VisualTimeRange, budget VisualBudget) ([]VisualTimeRange, bool, error) {
	transcriptRows := []model.VideoTranscriptionChunk{}
	if s.repos.TranscriptionChunk != nil {
		rows, err := s.repos.TranscriptionChunk.ListByTaskID(taskID)
		if err != nil {
			return nil, false, err
		}
		transcriptRows = rows
	}
	frames := []model.VideoVisualFrame{}
	if s.repos.VisualFrame != nil {
		rows, err := s.repos.VisualFrame.ListByTaskID(taskID)
		if err != nil {
			return nil, false, err
		}
		frames = rows
	}

	valid := make([]VisualTimeRange, 0, len(requested))
	for _, window := range requested {
		if window.StartMS < 0 || window.EndMS <= window.StartMS {
			return nil, false, fmt.Errorf("invalid seed window [%d,%d)", window.StartMS, window.EndMS)
		}
		if window.EndMS-window.StartMS > budget.MaxWindowMS {
			return nil, false, fmt.Errorf("seed window exceeds %dms visual budget", budget.MaxWindowMS)
		}
		if !hasTimelineEvidenceInRange(window, transcriptRows, frames) {
			return nil, false, fmt.Errorf("seed window [%d,%d) is not covered by existing video evidence", window.StartMS, window.EndMS)
		}
		valid = append(valid, window)
	}
	sort.Slice(valid, func(i, j int) bool { return valid[i].StartMS < valid[j].StartMS })
	merged := make([]VisualTimeRange, 0, len(valid))
	for _, window := range valid {
		if len(merged) > 0 && window.StartMS <= merged[len(merged)-1].EndMS {
			if window.EndMS > merged[len(merged)-1].EndMS {
				merged[len(merged)-1].EndMS = window.EndMS
			}
			continue
		}
		merged = append(merged, window)
	}
	budgetExhausted := false
	if len(merged) > budget.MaxWindows {
		merged = merged[:budget.MaxWindows]
		budgetExhausted = true
	}
	total := int64(0)
	selected := merged[:0]
	for _, window := range merged {
		remaining := budget.MaxTotalMS - total
		if remaining <= 0 {
			budgetExhausted = true
			break
		}
		if window.EndMS-window.StartMS > remaining {
			window.EndMS = window.StartMS + remaining
			budgetExhausted = true
		}
		if window.EndMS <= window.StartMS {
			continue
		}
		selected = append(selected, window)
		total += window.EndMS - window.StartMS
	}
	return selected, budgetExhausted, nil
}

func hasTimelineEvidenceInRange(window VisualTimeRange, transcripts []model.VideoTranscriptionChunk, frames []model.VideoVisualFrame) bool {
	for _, row := range transcripts {
		if row.Status != model.TranscriptionChunkStatusCompleted {
			continue
		}
		start, end, status := transcriptTimelineRange(row)
		if status != model.ChunkTimeRangeUnknown && rangesOverlap(window.StartMS, window.EndMS, start, end) {
			return true
		}
	}
	for _, frame := range frames {
		if frame.Status != model.VisualFrameStatusCompleted {
			continue
		}
		start, end, status := visualFrameRange(frame)
		if status != model.ChunkTimeRangeUnknown && rangesOverlap(window.StartMS, window.EndMS, start, end) {
			return true
		}
	}
	return false
}

func (s *QueryVisualInvestigator) selectFrameTimes(taskID int64, windows []VisualTimeRange, maxFrames int) ([]int64, bool, error) {
	if maxFrames <= 0 {
		return nil, true, nil
	}
	candidatesByWindow := make([][]int64, 0, len(windows))
	if s.repos.VisualFrame != nil {
		for windowIndex, window := range windows {
			frames, err := s.repos.VisualFrame.ListCompletedInRange(taskID, window.StartMS, window.EndMS, maxFrames*2)
			if err != nil {
				return nil, false, err
			}
			if len(candidatesByWindow) <= windowIndex {
				candidatesByWindow = append(candidatesByWindow, []int64{})
			}
			for _, frame := range frames {
				candidatesByWindow[windowIndex] = append(candidatesByWindow[windowIndex], frame.TimeMs)
			}
		}
	}
	for windowIndex, window := range windows {
		for len(candidatesByWindow) <= windowIndex {
			candidatesByWindow = append(candidatesByWindow, []int64{})
		}
		candidatesByWindow[windowIndex] = append(candidatesByWindow[windowIndex], window.StartMS)
		if window.EndMS-window.StartMS > 2 {
			candidatesByWindow[windowIndex] = append(candidatesByWindow[windowIndex], window.StartMS+(window.EndMS-window.StartMS)/2, window.EndMS-1)
		}
	}
	for index := range candidatesByWindow {
		sort.Slice(candidatesByWindow[index], func(i, j int) bool { return candidatesByWindow[index][i] < candidatesByWindow[index][j] })
		unique := candidatesByWindow[index][:0]
		for _, value := range candidatesByWindow[index] {
			if len(unique) == 0 || unique[len(unique)-1] != value {
				unique = append(unique, value)
			}
		}
		candidatesByWindow[index] = unique
	}
	times := make([]int64, 0, maxFrames)
	seen := make(map[int64]struct{}, maxFrames)
	for offset := 0; ; offset++ {
		added := false
		for _, candidates := range candidatesByWindow {
			if offset >= len(candidates) {
				continue
			}
			value := candidates[offset]
			if _, ok := seen[value]; ok {
				continue
			}
			seen[value] = struct{}{}
			times = append(times, value)
			added = true
			if len(times) == maxFrames {
				break
			}
		}
		if len(times) == maxFrames || !added {
			break
		}
	}
	totalCandidates := 0
	for _, candidates := range candidatesByWindow {
		totalCandidates += len(candidates)
	}
	budgetExhausted := totalCandidates > maxFrames
	return times, budgetExhausted, nil
}

func (s *QueryVisualInvestigator) inspectFrame(ctx context.Context, req InspectRequest, task *model.VideoTask, frame ffmpeg.KeyFrame, vision ai.VisionClient, modelName string, allowVLM bool, traceRef string) (VisualObservation, bool, int64, error) {
	data, err := os.ReadFile(frame.Path)
	if err != nil {
		return VisualObservation{}, false, 0, fmt.Errorf("read query frame %dms: %w", frame.TimeMs, err)
	}
	frameHash := sha256.Sum256(data)
	frameHashHex := hex.EncodeToString(frameHash[:])
	videoRevision := firstNonEmpty(strings.TrimSpace(task.FileMD5), fmt.Sprintf("task:%d", task.ID))
	cacheKey := queryVisualCacheKey(req.UserID, req.TaskID, videoRevision, frame.TimeMs, frameHashHex, req.Goal, req.RequiredFacts, modelName)
	if s.repos.VisualObservation != nil {
		cached, cacheErr := s.repos.VisualObservation.FindByCacheKey(ctx, req.UserID, req.TaskID, cacheKey)
		if cacheErr != nil {
			return VisualObservation{}, false, 0, cacheErr
		}
		if cached != nil {
			return visualObservationFromModel(*cached), true, 0, nil
		}
	}

	objectKey := fmt.Sprintf("visual-investigations/task-%d/%s/%dms-%s.jpg", req.TaskID, videoRevision, frame.TimeMs, frameHashHex[:16])
	var uploadErr error
	var uploaded int64
	if s.upload != nil {
		uploaded, uploadErr = s.upload(ctx, frame.Path, objectKey, "image/jpeg")
	}
	observationText := ""
	facts := []string{}
	gaps := []string{}
	status := model.VisualObservationStatusCaptured
	observationErr := ""
	if uploadErr != nil {
		observationErr = "upload query frame: " + uploadErr.Error()
	}
	if uploadErr == nil && vision != nil && allowVLM {
		prompt := buildQueryVisualPrompt(req.Goal, req.RequiredFacts)
		response, visionErr := vision.CaptionImage(ctx, frame.Path, prompt)
		if visionErr != nil {
			status = model.VisualObservationStatusFailed
			observationErr = "query VLM: " + visionErr.Error()
		} else {
			observationText = strings.TrimSpace(response)
			facts, gaps = parseQueryVisualResponse(observationText)
			if observationText != "" {
				status = model.VisualObservationStatusObserved
			} else {
				status = model.VisualObservationStatusFailed
				observationErr = "query VLM returned an empty observation"
			}
		}
	} else if uploadErr == nil && vision != nil && !allowVLM {
		observationErr = "query VLM call budget exhausted; frame was captured only"
	} else if vision == nil && observationErr == "" {
		status = model.VisualObservationStatusFailed
		observationErr = "query VLM is unavailable; frame was captured only"
	}
	if uploadErr != nil {
		status = model.VisualObservationStatusFailed
	}
	structured, marshalErr := json.Marshal(facts)
	if marshalErr != nil {
		return VisualObservation{}, false, 0, marshalErr
	}
	structuredGaps, marshalErr := json.Marshal(gaps)
	if marshalErr != nil {
		return VisualObservation{}, false, 0, marshalErr
	}
	ffmpegArgs, marshalErr := json.Marshal(frame.Args)
	if marshalErr != nil {
		return VisualObservation{}, false, 0, marshalErr
	}
	rawResponseHash := ""
	if observationText != "" {
		rawResponseHash = sha256Text(observationText)
	}
	row := model.VideoVisualObservation{
		ID: uuid.NewString(), UserID: req.UserID, TaskID: req.TaskID, TraceRef: traceRef,
		CacheKey: cacheKey, VideoRevision: videoRevision,
		FrameRef: "query-frame:" + frameHashHex[:24], ArtifactKind: model.VisualArtifactKindFrame,
		FFmpegArgs: string(ffmpegArgs), ObjectKey: objectKey,
		StartMS: frame.TimeMs, EndMS: frame.TimeMs + 1, Source: queryVisualSource,
		CapturePolicyVersion: queryVisualCapturePolicyVersion, Model: modelName,
		PromptVersion: queryVisualPromptVersion, Observation: observationText,
		StructuredFacts: string(structured), StructuredGaps: string(structuredGaps), RawResponseHash: rawResponseHash,
		Status: status, ErrorMsg: truncateVisualInvestigatorError(observationErr),
	}
	if uploadErr != nil {
		row.ObjectKey = ""
	}
	if s.repos.VisualObservation == nil {
		return visualObservationFromModel(row), false, uploaded, nil
	}
	if err := s.repos.VisualObservation.Append(ctx, &row); err != nil {
		return VisualObservation{}, false, 0, err
	}
	return visualObservationFromModel(row), false, uploaded, nil
}

func (s *QueryVisualInvestigator) resolveVisionClient(ctx context.Context, userID int64) (ai.VisionClient, error) {
	if s.resolveVision == nil {
		return nil, errors.New("query vision resolver not configured")
	}
	return s.resolveVision(ctx, userID)
}

func (s *QueryVisualInvestigator) resolveVisionModel(ctx context.Context, userID int64) string {
	if s.resolveModel == nil {
		return "unknown"
	}
	modelName, err := s.resolveModel(ctx, userID)
	if err != nil || strings.TrimSpace(modelName) == "" {
		return "unknown"
	}
	return strings.TrimSpace(modelName)
}

func buildQueryVisualPrompt(goal string, required []RequiredFact) string {
	encoded, _ := json.Marshal(required)
	return fmt.Sprintf(`你是 VidLens 的查询时视觉取证器。只根据当前图片回答，不要补充图片外的信息。
用户问题：%s
必要事实（仅用于检查，不要编造）：%s
请只输出 JSON：{"facts":["可直接从画面观察到的事实"],"gaps":["仍缺少的信息"]}。
如果文字或细节看不清，请明确写入 gaps；不要输出置信度，不要声称已经看过其他帧。`, goal, string(encoded))
}

func parseQueryVisualResponse(raw string) ([]string, []string) {
	var parsed struct {
		Facts []string `json:"facts"`
		Gaps  []string `json:"gaps"`
	}
	if json.Unmarshal([]byte(raw), &parsed) == nil {
		return compactStrings(parsed.Facts), compactStrings(parsed.Gaps)
	}
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	return []string{strings.TrimSpace(raw)}, nil
}

func bindObservedFacts(required []RequiredFact, observations []VisualObservation) []ClaimEvidenceBinding {
	bindings := make([]ClaimEvidenceBinding, 0, len(required))
	for _, requiredFact := range required {
		fact := strings.TrimSpace(requiredFact.Name)
		if fact == "" {
			continue
		}
		for _, observation := range observations {
			if strings.Contains(strings.ToLower(observation.Observation), strings.ToLower(fact)) {
				bindings = append(bindings, ClaimEvidenceBinding{
					Fact: fact, ObservationID: observation.ID, Relation: "candidate_support", Status: "unverified",
					ValidationNote: "query-time observation is replayable; semantic support was not independently verified",
				})
				break
			}
		}
	}
	return bindings
}

func visualObservationFromModel(row model.VideoVisualObservation) VisualObservation {
	facts := []string{}
	_ = json.Unmarshal([]byte(row.StructuredFacts), &facts)
	gaps := []string{}
	_ = json.Unmarshal([]byte(row.StructuredGaps), &gaps)
	ffmpegArgs := []string{}
	_ = json.Unmarshal([]byte(row.FFmpegArgs), &ffmpegArgs)
	return VisualObservation{
		ID: row.ID, TaskID: row.TaskID, VideoRevision: row.VideoRevision, FrameRef: row.FrameRef,
		ArtifactKind: firstNonEmpty(strings.TrimSpace(row.ArtifactKind), model.VisualArtifactKindFrame),
		FFmpegArgs:   ffmpegArgs, ObjectKey: row.ObjectKey, StartMS: row.StartMS, EndMS: row.EndMS, Source: row.Source,
		CapturePolicyVersion: row.CapturePolicyVersion, Model: row.Model, PromptVersion: row.PromptVersion,
		Observation: row.Observation, StructuredFacts: facts, Gaps: gaps, RawResponseHash: row.RawResponseHash,
		Status: row.Status, Error: row.ErrorMsg,
	}
}

func queryVisualCacheKey(userID, taskID int64, videoRevision string, timeMS int64, frameHash string, goal string, facts []RequiredFact, modelName string) string {
	encoded, _ := json.Marshal(facts)
	return sha256Text(strings.Join([]string{fmt.Sprint(userID), fmt.Sprint(taskID), videoRevision, fmt.Sprint(timeMS), frameHash, goal, string(encoded), modelName, queryVisualPromptVersion}, "\x00"))
}

func sha256Text(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func rangesOverlap(startA, endA, startB, endB int64) bool {
	return startA < endB && startB < endA
}

func hasFailedObservation(observations []VisualObservation) bool {
	for _, observation := range observations {
		if observation.Status == model.VisualObservationStatusFailed {
			return true
		}
	}
	return false
}

func compactStrings(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func truncateVisualInvestigatorError(value string) string {
	value = strings.TrimSpace(value)
	if len(value) > 500 {
		return value[:500]
	}
	return value
}
