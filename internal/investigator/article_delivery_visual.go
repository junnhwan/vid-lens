package investigator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"vidlens/internal/domain"
	"vidlens/internal/media"
	"vidlens/internal/store"
)

const (
	deliveryVisualWindows    = 6
	deliveryVisualFrames     = 18
	deliveryVisualStandalone = 3
	deliveryVisualImages     = 24
	deliveryVisualTokens     = 120000
	deliveryVisualBytes      = 16 << 20
	deliveryVisualCaptureMS  = 120000
	deliveryVisualAttemptMS  = 20000
)

type deliveryVisualRequest struct {
	ActivityTitle string       `json:"activity_title,omitempty" jsonschema:"maxLength=48,description=Write a short public Chinese action title for this screenshot, such as 查看邮件分类结果 or 向后微调至结果出现; no reasoning or success claim"`
	Key           string       `json:"key" jsonschema:"description=Short stable name for this visual goal; reuse it for its one refinement"`
	SectionID     string       `json:"section_id" jsonschema:"description=Existing chapter ID or creation key from this save; screenshots must help this chapter"`
	Question      string       `json:"question" jsonschema:"description=What the screenshot should show and why it helps the chapter; at most 240 characters"`
	StartMS       int64        `json:"start_ms"`
	Count         int          `json:"count,omitempty" jsonschema:"minimum=1,maximum=3,description=1 to 3 candidates; default 3, or 1 for detail"`
	EndMS         int64        `json:"end_ms" jsonschema:"description=Target range within video, up to 3 minutes; program narrows to a centered 60-second window. Refinement at most 12 seconds near parent"`
	RefineFrom    string       `json:"refine_from,omitempty" jsonschema:"description=Actually delivered fN alias to adjust once; never repeatedly search the same goal"`
	Detail        bool         `json:"detail,omitempty" jsonschema:"description=Request fresh higher-resolution pixels from the original video for small text"`
	Crop          *domain.Crop `json:"crop,omitempty" jsonschema:"description=Optional crop of a delivered parent frame; shares that goal's single refinement"`
}

type deliveryFigurePlacement struct {
	SectionID    string     `json:"section_id" jsonschema:"description=Existing chapter ID or creation key from this save"`
	AfterBlockID string     `json:"after_block_id,omitempty" jsonschema:"description=Existing block in this chapter; omitted appends the figure"`
	Figure       loopFigure `json:"figure"`
}

type deliveryVisualRecord struct {
	ActivityTitle  string        `json:"activity_title,omitempty"`
	Operation      string        `json:"operation"`
	Goal           string        `json:"goal"`
	SectionID      string        `json:"section_id,omitempty"`
	Question       string        `json:"question"`
	Window         media.Window  `json:"window"`
	RequestedRange *domain.Range `json:"requested_range,omitempty"`
	Frames         []string      `json:"frames,omitempty"`
	Refinement     bool          `json:"refinement,omitempty"`
	Overview       bool          `json:"overview,omitempty"`
	Attempted      bool          `json:"attempted,omitempty"`
	ChargedFrames  int           `json:"charged_frames"`
	ElapsedMS      int64         `json:"elapsed_ms"`
	Status         string        `json:"status"`
}

type deliveryVisualState struct {
	OverviewDone    bool                   `json:"overview_done,omitempty"`
	PixelsClosed    bool                   `json:"pixels_closed,omitempty"`
	StandaloneSlots []int                  `json:"standalone_slots,omitempty"`
	Records         []deliveryVisualRecord `json:"records,omitempty"`
}

type deliveryVisualAllowance struct {
	Allowed             bool                     `json:"allowed"`
	Reason              string                   `json:"reason,omitempty"`
	WindowsRemaining    int                      `json:"new_windows_remaining"`
	FramesRemaining     int                      `json:"capture_frames_remaining"`
	StandaloneRemaining int                      `json:"standalone_calls_remaining"`
	ProseCallsReserved  int                      `json:"prose_calls_reserved"`
	ProseMSReserved     int64                    `json:"prose_ms_reserved"`
	CaptureMSRemaining  int64                    `json:"capture_ms_remaining"`
	Usage               store.ArticleVisualUsage `json:"physical_image_input"`
}

func (s *session) deliveryVisuals() bool {
	return s.cp.ArticleLoop != nil && (s.cp.ArticleLoop.ReviewRevision == domain.ArticleDeliveryV6 || s.cp.ArticleLoop.ReviewRevision == domain.ArticleDeliveryV7)
}

// A syntactically valid prose save survives a malformed optional sidecar.
// The complete original tool response remains in the model operation ledger;
// strict duplicate-key/whole-JSON validation still happens before this decoder.
func decodeDeliveryVisualWrite(raw []byte, in *loopSaveInput) error {
	fields := map[string]json.RawMessage{}
	if err := json.Unmarshal(raw, &fields); err != nil {
		return err
	}
	requests, placements := fields["visual_requests"], fields["figure_placements"]
	delete(fields, "visual_requests")
	delete(fields, "figure_placements")
	if err := loopDecode(domain.JSON(fields), in); err != nil {
		return err
	}
	decode := func(raw json.RawMessage, out any) bool {
		if len(raw) == 0 {
			return true
		}
		if string(raw) == "null" {
			return true
		}
		decoder := json.NewDecoder(strings.NewReader(string(raw)))
		decoder.DisallowUnknownFields()
		return decoder.Decode(out) == nil
	}
	if !decode(requests, &in.VisualRequests) {
		in.VisualRequests = nil
		in.visualParseIssues = append(in.visualParseIssues, "malformed optional visual_requests omitted; prose preserved")
	}
	if !decode(placements, &in.FigurePlacements) {
		in.FigurePlacements = nil
		in.visualParseIssues = append(in.visualParseIssues, "malformed optional figure_placements omitted; prose preserved")
	}
	return nil
}

// Source position is navigation, not a semantic completeness verdict. Reserve
// conservatively from all text beyond the saved position, with room to finish.
func (s *session) deliveryProseReserve(a domain.Article) int {
	last := -1
	ordinals := map[string]int{}
	for i, t := range s.transcript {
		ordinals[t.ID] = i
	}
	for _, sec := range a.Sections {
		for _, b := range sec.Blocks {
			for _, ref := range blockCitationIDs(b) {
				for _, id := range a.Citations[ref].SegmentIDs {
					if n, ok := ordinals[id]; ok {
						last = max(last, n)
					}
				}
			}
		}
	}
	chars := 0
	for _, t := range s.transcript[last+1:] {
		chars += utf8.RuneCountInString(t.Text)
	}
	return max(2, (chars+3999)/4000+1)
}

func (s *session) deliveryVisualAllowance(ctx context.Context, a domain.Article) (deliveryVisualAllowance, error) {
	l := s.cp.ArticleLoop
	out := deliveryVisualAllowance{WindowsRemaining: deliveryVisualWindows, FramesRemaining: deliveryVisualFrames, StandaloneRemaining: max(0, deliveryVisualStandalone-len(l.Visual.StandaloneSlots)), CaptureMSRemaining: deliveryVisualCaptureMS, ProseCallsReserved: s.deliveryProseReserve(a)}
	for _, r := range l.Visual.Records {
		out.CaptureMSRemaining -= r.ElapsedMS
		if !r.Overview && r.Attempted {
			out.WindowsRemaining--
			out.FramesRemaining -= r.ChargedFrames
		}
	}
	out.WindowsRemaining, out.FramesRemaining, out.CaptureMSRemaining = max(0, out.WindowsRemaining), max(0, out.FramesRemaining), max(int64(0), out.CaptureMSRemaining)
	stats, err := s.engine.Store.ModelExecution(ctx, s.claim.Run.ID)
	if err != nil {
		return out, err
	}
	out.Usage, err = s.engine.Store.ArticleVisualUsage(ctx, s.claim.Run.ID)
	if err != nil {
		return out, err
	}
	run, err := s.engine.Store.Run(ctx, s.claim.Run.ID)
	if err != nil {
		return out, err
	}
	perCallMS := max(int64(45000), stats.ResponseMS/int64(max(1, stats.Calls)))
	out.ProseMSReserved = int64(out.ProseCallsReserved) * perCallMS
	remainingMS := min(l.Budget.ActiveSeconds*1000-run.ActiveMS, time.Until(l.Budget.Deadline).Milliseconds())
	switch {
	case l.Visual.PixelsClosed || out.Usage.Images >= deliveryVisualImages || out.Usage.EstimatedTokens >= deliveryVisualTokens || out.Usage.Bytes >= deliveryVisualBytes:
		out.Reason = "image_input_limit"
	case min(l.Budget.Calls-s.cp.Calls, l.Budget.Sends-stats.Submissions) <= out.ProseCallsReserved:
		out.Reason = "prose_calls_reserved"
	case remainingMS < out.ProseMSReserved+deliveryVisualAttemptMS:
		out.Reason = "prose_time_reserved"
	case out.CaptureMSRemaining < 1000:
		out.Reason = "capture_time_limit"
	default:
		out.Allowed = true
	}
	return out, nil
}

func (s *session) deliveryFrameID(alias string, seen bool) string {
	l := s.cp.ArticleLoop
	for i, id := range l.Frames {
		if (alias == id || alias == fmt.Sprint("f", i+1)) && (!seen || slices.Contains(l.SeenFrames, id)) {
			return id
		}
	}
	return ""
}

func (s *session) rememberDeliveryVisual(r deliveryVisualRecord) {
	l := s.cp.ArticleLoop
	if !slices.ContainsFunc(l.Visual.Records, func(old deliveryVisualRecord) bool { return old.Operation == r.Operation }) {
		l.Visual.Records = append(l.Visual.Records, r)
	}
	for _, id := range r.Frames {
		l.Frames = appendUnique(l.Frames, id)
	}
}

func deliveryVisualTitle(req deliveryVisualRequest) string {
	question := strings.TrimSpace(req.Question)
	if runes := []rune(question); len(runes) > 48 {
		question = string(runes[:47]) + "…"
	}
	title := publicTitle(req.ActivityTitle, publicTitle(question, "查看所需视频画面"))
	title = strings.TrimSpace(strings.TrimPrefix(title, "正在"))
	if title == "" {
		title = "查看所需视频画面"
	}
	if runes := []rune(title); len(runes) > 48 {
		title = string(runes[:47]) + "…"
	}
	prefix := "按需取帧："
	if req.RefineFrom != "" || req.Crop != nil {
		prefix = "微调取帧："
	}
	return prefix + title
}

func (s *session) deliveryVisualActivity(ctx context.Context, r deliveryVisualRecord, started time.Time, finished bool) error {
	if r.Overview {
		return nil
	}
	step := domain.Step{ID: "visual_" + s.claim.Run.ID + "_" + store.Hash(r.Operation)[:16], Kind: "tool_execution", ModelCall: s.cp.Calls, Title: r.ActivityTitle, Purpose: domain.Ptr(r.Question), Phase: "compose", Status: "running", StartedAt: &started, FrameIDs: []string{}}
	if r.Window.StartMS >= 0 && r.Window.EndMS > r.Window.StartMS && r.Window.EndMS <= s.snapshot.DurationMS {
		step.SourceRange = &domain.Range{StartMS: r.Window.StartMS, EndMS: r.Window.EndMS}
	}
	if finished {
		step.Status, step.FinishedAt = "completed", domain.Ptr(time.Now().UTC())
		message := fmt.Sprintf("已取得%d张候选画面，继续写作并按内容需要选图。", len(r.Frames))
		if r.Status == "reused_window" {
			message = fmt.Sprintf("复用%d张已有候选画面，继续写作与选图。", len(r.Frames))
		}
		if len(r.Frames) == 0 {
			step.Title = "略过取帧：" + strings.TrimPrefix(strings.TrimPrefix(r.ActivityTitle, "按需取帧："), "微调取帧：")
			message = "本次未取得可用画面，保留正文并继续写作。"
			switch r.Status {
			case "prose_calls_reserved", "prose_time_reserved":
				message = "剩余调用与时间优先用于完成正文，本次略过可选截图。"
			case "capture_limit", "capture_time_limit", "image_input_limit":
				message = "已达到本轮截图额度，本次略过可选截图并继续正文。"
			case "goal_refinement_limit":
				message = "同一目标已使用一次微调，本次略过继续找图，正文保留。"
			case "interrupted_capture_omitted":
				message = "上次取帧中断，本次不重复取帧，保留正文并继续写作。"
			case "invalid_time_window", "refinement_outside_neighborhood":
				message = "截图时间范围不适用，本次略过该图并继续正文。"
			}
		} else {
			step.FrameIDs = append([]string{}, r.Frames...)
		}
		step.Outcome = &domain.Outcome{Message: message, EvidenceStatus: "not_assessed"}
	}
	_, err := s.engine.Store.Step(ctx, s.claim, step)
	return err
}

// Each optional attempt has its own durable receipt. A worker interrupted in
// ffmpeg charges the attempted work and skips it on takeover instead of opening
// another search/retry loop. Operational fencing/cancellation still propagates.
func (s *session) captureDeliveryVisual(ctx context.Context, key string, req deliveryVisualRequest, overview bool) (deliveryVisualRecord, error) {
	r := deliveryVisualRecord{Operation: "article-visual:" + key, Goal: req.Key, SectionID: req.SectionID, Question: req.Question, Overview: overview, Status: "skipped", RequestedRange: &domain.Range{StartMS: req.StartMS, EndMS: req.EndMS}, ActivityTitle: deliveryVisualTitle(req)}
	arguments := req
	arguments.ActivityTitle = "" // Public wording cannot create another capture.
	old, err := s.engine.Store.Operation(ctx, s.claim.Run.ID, r.Operation)
	if err == nil && (old.Kind != "article_visual" || old.ArgsHash != store.Hash(arguments)) {
		return r, domain.ErrFenced
	}
	if err == nil && old.Status == "completed" {
		if err = json.Unmarshal(old.Result, &r); err != nil {
			return r, err
		}
		s.rememberDeliveryVisual(r)
		return r, nil
	}
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return r, err
	}
	interrupted := err == nil
	if _, err = s.engine.Store.BeginOperation(ctx, s.claim, r.Operation, "article_visual", arguments, false); err != nil {
		return r, err
	}
	activityStarted := time.Now().UTC()
	if err = s.deliveryVisualActivity(ctx, r, activityStarted, false); err != nil {
		return r, err
	}
	complete := func(status string) (deliveryVisualRecord, error) {
		r.Status = status
		if err := s.deliveryVisualActivity(ctx, r, activityStarted, true); err != nil {
			return r, err
		}
		if err := s.engine.Store.FinishOperation(ctx, s.claim, r.Operation, r, "", nil); err != nil {
			return r, err
		}
		s.rememberDeliveryVisual(r)
		return r, nil
	}
	if interrupted {
		r.Attempted, r.ChargedFrames, r.ElapsedMS = true, 3, deliveryVisualAttemptMS
		return complete("interrupted_capture_omitted")
	}
	if strings.TrimSpace(req.Question) == "" || utf8.RuneCountInString(req.Question) > 240 || req.Key == "" || len(req.Key) > 80 {
		return complete("invalid_visual_goal")
	}
	count := req.Count
	if count == 0 {
		count = 3
		if req.Detail {
			count = 1
		}
	}
	if count < 1 || count > 3 {
		return complete("at_most_three_candidates")
	}
	w := media.Window{StartMS: req.StartMS, EndMS: req.EndMS, Count: count, Mode: "targeted"}
	if overview {
		w.Count, w.Mode = 1, "overview"
	}
	if req.Detail {
		w.Mode = "detail"
	}
	// Real writers often identify a two-minute explanation rather than the
	// exact slide time. Narrow locally without asking the model to repair an
	// optional tool parameter. Never turn an out-of-video request into evidence.
	if !overview && req.RefineFrom == "" && req.Crop == nil && w.StartMS >= 0 && w.EndMS <= s.snapshot.DurationMS && w.EndMS-w.StartMS > 60000 && w.EndMS-w.StartMS <= 180000 {
		mid := w.StartMS + (w.EndMS-w.StartMS)/2
		w.StartMS, w.EndMS = mid-30000, mid+30000
	}
	if req.Crop != nil {
		if req.Detail {
			return complete("use_detail_window_or_existing_pixel_crop")
		}
		parentID := s.deliveryFrameID(req.Crop.ParentFrameID, true)
		if parentID == "" {
			return complete("crop_requires_delivered_parent")
		}
		parent, e := s.engine.Store.InternalFrame(ctx, s.claim, parentID)
		if e != nil {
			return r, e
		}
		if parent.TimeMS == nil {
			return complete("crop_parent_time_missing")
		}
		crop := *req.Crop
		crop.ParentFrameID = parentID
		w.Crop, w.Count, w.StartMS, w.EndMS = &crop, 1, max(int64(0), *parent.TimeMS-1), min(s.snapshot.DurationMS, *parent.TimeMS+1)
		req.RefineFrom = parentID
	}
	r.Window = w
	if w.StartMS < 0 || w.EndMS <= w.StartMS || w.EndMS > s.snapshot.DurationMS || w.EndMS-w.StartMS > 60000 {
		return complete("invalid_time_window")
	}
	if req.RefineFrom != "" {
		id := s.deliveryFrameID(req.RefineFrom, true)
		if id == "" {
			return complete("refinement_requires_delivered_parent")
		}
		parent, e := s.engine.Store.InternalFrame(ctx, s.claim, id)
		if e != nil {
			return r, e
		}
		if parent.TimeMS == nil || w.EndMS-w.StartMS > 12000 || w.StartMS < *parent.TimeMS-15000 || w.EndMS > *parent.TimeMS+15000 {
			return complete("refinement_outside_neighborhood")
		}
		r.Refinement, r.Goal = true, "frame:"+id
		for _, previous := range s.cp.ArticleLoop.Visual.Records {
			if slices.Contains(previous.Frames, id) {
				r.Goal = previous.Goal
				break
			}
		}
		if slices.ContainsFunc(s.cp.ArticleLoop.Visual.Records, func(p deliveryVisualRecord) bool { return p.Goal == r.Goal && p.Refinement && p.Attempted }) {
			return complete("goal_refinement_limit")
		}
	} else if !overview && slices.ContainsFunc(s.cp.ArticleLoop.Visual.Records, func(p deliveryVisualRecord) bool { return p.Goal == req.Key && p.Attempted }) {
		return complete("goal_already_sampled_use_one_refinement")
	}
	r.Window = w
	_, draft, err := s.loadLoopDraft(ctx)
	if err != nil {
		return r, err
	}
	a, err := s.deliveryVisualAllowance(ctx, draft.Article)
	if err != nil {
		return r, err
	}
	if !a.Allowed {
		return complete(a.Reason)
	}
	if !overview && (a.WindowsRemaining == 0 || a.FramesRemaining < w.Count) {
		return complete("capture_limit")
	}
	// Identical windows are reloads, regardless of how the author names them.
	for _, previous := range s.cp.ArticleLoop.Visual.Records {
		if previous.Attempted && store.Hash(previous.Window) == store.Hash(w) && len(previous.Frames) > 0 {
			r.Frames = previous.Frames
			return complete("reused_window")
		}
	}
	r.Attempted, r.ChargedFrames = true, w.Count
	limit := min(int64(deliveryVisualAttemptMS), a.CaptureMSRemaining)
	if overview {
		limit = min(limit, 5000)
	}
	child, cancel := context.WithTimeout(ctx, time.Duration(limit)*time.Millisecond)
	started := time.Now()
	out, captureErr := s.engine.Media.Inspect(child, s.claim, "article-visual:media:"+store.Hash(w), req.Question, w, nil)
	cancel()
	r.ElapsedMS = max(int64(1), time.Since(started).Milliseconds())
	if ctx.Err() != nil {
		return r, ctx.Err()
	}
	if err = s.engine.Store.Check(ctx, s.claim); err != nil {
		return r, err
	}
	if captureErr != nil {
		return complete("capture_unavailable_prose_preserved")
	}
	r.Frames = out.FrameIDs
	return complete("captured")
}

func (s *session) requestDeliveryVisuals(ctx context.Context, key string, in loopSaveInput, receipt *loopReceipt) error {
	if len(in.VisualRequests) == 0 || receipt == nil {
		return nil
	}
	_, draft, err := s.loadLoopDraft(ctx)
	if err != nil {
		return err
	}
	for i, req := range in.VisualRequests[:min(2, len(in.VisualRequests))] {
		if in.Finish {
			break
		} // Completion always wins over another optional search.
		if id := receipt.Created[req.SectionID]; id != "" {
			req.SectionID = id
		}
		if !slices.ContainsFunc(draft.Article.Sections, func(sec domain.Section) bool { return sec.ID == req.SectionID }) {
			continue
		}
		r, err := s.captureDeliveryVisual(ctx, fmt.Sprintf("%s:%d", key, i), req, false)
		if err != nil {
			return err
		}
		for _, id := range r.Frames {
			s.cp.ArticleLoop.NextFrames = appendUnique(s.cp.ArticleLoop.NextFrames, id)
		}
	}
	return nil
}

func (s *session) viewDeliveryFrames(ctx context.Context, p articleLoopCall, in loopViewInput) (bool, error) {
	l := s.cp.ArticleLoop
	if p.OptionalVisualAllowed != nil && !*p.OptionalVisualAllowed {
		l.Feedback = domain.JSON(map[string]any{"visual_status": "prose_budget_reserved", "action": "Save the remaining complete prose. Optional viewing is disabled for this call."})
		return false, nil
	}
	if !slices.Contains(l.Visual.StandaloneSlots, p.Slot) {
		if len(l.Visual.StandaloneSlots) >= deliveryVisualStandalone {
			l.Feedback = domain.JSON(map[string]any{"visual_status": "standalone_limit", "action": "Continue saving complete prose; optional screenshots are finished."})
			return false, nil
		}
		l.Visual.StandaloneSlots = append(l.Visual.StandaloneSlots, p.Slot)
	}
	if len(in.FrameIDs) > 0 {
		if in.EndMS != 0 || in.StartMS != 0 || in.Crop != nil || in.RefineFrom != "" || len(in.FrameIDs) > 3 {
			l.Feedback = domain.JSON(map[string]any{"visual_status": "reload_requires_only_1_to_3_frame_ids", "action": "Continue prose or use a valid optional request in the next save."})
			return false, nil
		}
		for _, alias := range in.FrameIDs {
			if id := s.deliveryFrameID(alias, false); id != "" {
				l.NextFrames = appendUnique(l.NextFrames, id)
			}
		}
	} else {
		r, err := s.captureDeliveryVisual(ctx, fmt.Sprint("view:", p.Slot), deliveryVisualRequest{Key: fmt.Sprint("view:", p.Slot), Question: in.Question, StartMS: in.StartMS, EndMS: in.EndMS, RefineFrom: in.RefineFrom, Detail: in.Detail, Crop: in.Crop, Count: in.Count, ActivityTitle: in.activityTitle}, false)
		if err != nil {
			return false, err
		}
		l.NextFrames = append([]string{}, r.Frames...)
	}
	l.Feedback = domain.JSON(map[string]any{"next_pixels": l.NextFrames, "action": "Select only useful actual pixels while continuing prose; omit optional figures when unavailable."})
	return false, nil
}

func (s *session) deliveryVisualFramesForCall(ctx context.Context, p *articleLoopCall) error {
	l := s.cp.ArticleLoop
	if l.Visual.PixelsClosed {
		p.Frames = nil
		return nil
	}
	usage, err := s.engine.Store.ArticleVisualUsage(ctx, s.claim.Run.ID)
	if err != nil {
		return err
	}
	frames := []string{}
	for _, id := range p.Frames {
		if slices.Contains(frames, id) {
			continue
		}
		a, _, err := s.engine.Media.ReadImage(ctx, s.claim, id)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if e := s.engine.Store.Check(ctx, s.claim); e != nil {
				return e
			}
			continue
		}
		tokens := int64(estimateImageTokens(a))
		if usage.Images+1 > deliveryVisualImages || usage.EstimatedTokens+tokens > deliveryVisualTokens || usage.Bytes+a.Bytes > deliveryVisualBytes {
			continue
		}
		usage.Images++
		usage.EstimatedTokens += tokens
		usage.Bytes += a.Bytes
		frames = append(frames, id)
		if len(frames) == 4 {
			break
		}
	}
	p.Frames = frames
	return nil
}

func (s *session) placeDeliveryFigures(ctx context.Context, d *articleLoopDraft, placements []deliveryFigurePlacement, receipt *loopReceipt, catalog map[string]domain.Citation) error {
	for _, placement := range placements[:min(6, len(placements))] {
		id := placement.SectionID
		if created := receipt.Created[id]; created != "" {
			id = created
		}
		index := slices.IndexFunc(d.Article.Sections, func(sec domain.Section) bool { return sec.ID == id })
		if index < 0 {
			receipt.FigureOmissions = append(receipt.FigureOmissions, "unknown target chapter")
			continue
		}
		sec := &d.Article.Sections[index]
		position := len(sec.Blocks)
		if placement.AfterBlockID != "" {
			position = slices.IndexFunc(sec.Blocks, func(b domain.Block) bool { return b.ID == placement.AfterBlockID }) + 1
			if position == 0 {
				receipt.FigureOmissions = append(receipt.FigureOmissions, "unknown target block")
				continue
			}
		}
		built, cits, notes, _, err := loopCanonicalSection(loopSection{Title: sec.Title, Content: []loopContent{{Figure: &placement.Figure}}}, id, catalog, s.transcript)
		if err == nil {
			err = s.validateBlocks(ctx, built.Blocks, cits, true)
		}
		if err != nil {
			receipt.FigureOmissions = append(receipt.FigureOmissions, "unavailable or unsupported optional figure")
			continue
		}
		b := built.Blocks[0]
		duplicate := false
		for _, existing := range d.Article.Sections {
			for _, old := range existing.Blocks {
				if old.Type == "figure" && slices.Equal(old.FrameIDs, b.FrameIDs) {
					duplicate = true
				}
			}
		}
		if duplicate {
			receipt.FigureOmissions = append(receipt.FigureOmissions, "figure already used")
			continue
		}
		b.ID = "b_" + store.Hash(map[string]any{"section": id, "frames": b.FrameIDs})[:20]
		sec.Blocks = slices.Insert(sec.Blocks, position, b)
		for ref, cit := range cits {
			d.Article.Citations[ref] = cit
		}
		for frame, note := range notes {
			d.Notes[frame] = note
		}
		receipt.Accepted = appendUnique(receipt.Accepted, id)
	}
	return nil
}

const deliveryVisualInstructions = `配图按每段内容的实际帮助选择，不按视频类型、时长或章节数配额。画面能说明概念、数据、代码或操作时可以多图；信息少时少图或无图，也可选择少量有代表性且贴合正文的场景，不为了数量重复放图。只看到少量概览不能断言全片没有有用画面。
优先边写边取：在save_sections保存完整正文时，附带最多2个visual_requests，写明稳定key、对应section_id（新章可用本次creation key）、想看清什么与时间范围。每个截图需求自己写简短中文activity_title，用于向用户展示具体取帧/微调小标题，只说明行动、不输出推理或预先宣称成功。程序先保存正文再取附近1–3帧，在下一次正常写作中交付像素，不单独安排配图规划或审图调用。已看到的像素不自动重复发送；frame_index_navigation_only只是导航，不是新像素。需要时可有限重看。
看到目标还没出现或已经过去时，对同一个目标用refine_from填写实际已交付的fN，在父帧前后15秒内指定不超过12秒的新窗口；每个目标最多微调一次。小字需要detail=true从原视频重取更高分辨率画面。一次静态图不证明连续动作。
选图后可用figure_placements把figure插到现有章节的after_block_id后（省略则章末），与下一批正文一起保存，不重发整章。图注、visible_facts和uncertainty只根据实际收到的像素，拒绝推测。finish=true时不再执行新的截图需求，直接交付已有完整稿。
visual_budget由程序执行；正文调用和时间优先保留。额外独立view_frames最多3次，同目标微调1次，找不到、失败或预算不足就省去这张图并继续正文，不停止整篇、不删减后半段，也不为找图循环调用。`
