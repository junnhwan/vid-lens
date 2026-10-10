package service

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"strings"
	"testing"

	"vid-lens/internal/ai"
	"vid-lens/internal/artifact"
	"vid-lens/internal/model"
	"vid-lens/internal/processing"
	"vid-lens/internal/summarydoc"
	"vid-lens/internal/textsource"
)

type summaryVisualFixture struct {
	f                       *generationFixture
	chatCalls, inspectCalls int
	requests                []InspectRequest
	foreignSelection        bool
	afterInspect            func()
	selectedID              string
	visualError             error
	planResponses           []string
}

func (v *summaryVisualFixture) Enrich(ctx context.Context, task *model.VideoTask, job *model.TaskJob, snapshot processing.GenerationSnapshot, profile ai.Profile, source *textsource.Snapshot, base *model.AISummary, token string) error {
	err := NewSummaryVisualService(v.f.repos, v, v).Enrich(ctx, task, job, snapshot, profile, source, base, token)
	v.visualError = err
	return err
}

func (v *summaryVisualFixture) NewChatClient(ai.Profile) (ai.ChatClient, error) { return v, nil }
func (v *summaryVisualFixture) NewVisionClient(ai.Profile) (ai.VisionClient, error) {
	return &investigatorVisionClient{response: "真实配置画面"}, nil
}
func (v *summaryVisualFixture) Chat(ctx context.Context, messages []ai.ChatMessage) (string, error) {
	system := messages[0].Content
	if strings.Contains(system, "最多三个目标") {
		v.chatCalls++
		if len(v.planResponses) > 0 {
			raw := v.planResponses[0]
			v.planResponses = v.planResponses[1:]
			return raw, nil
		}
		return `{"public_title":"定位配置画面","reason":"参数配置需要截图","targets":[{"block_id":"block-cue-a","cue_id":"cue-a","goal":"连接池配置","required_facts":[{"name":"最大连接数"}]}]}`, nil
	}
	if strings.Contains(system, "已实际看图") {
		v.chatCalls++
		id := v.selectedID
		if v.foreignSelection {
			id = "foreign-observation"
		}
		return artifact.JSON(map[string]any{"public_title": "选择参数截图", "presentation_mode": "image_text", "reason": "配置截图解释对应段落", "figures": []map[string]any{{"block_id": "block-cue-a", "observation_id": id, "caption": "画面显示最大连接数配置。", "alt": "连接池参数配置画面", "supports": "说明该章节的参数设置"}}}), nil
	}
	return v.f.chat.Chat(ctx, messages)
}
func (v *summaryVisualFixture) Inspect(ctx context.Context, req InspectRequest) (Investigation, error) {
	v.inspectCalls++
	v.requests = append(v.requests, req)
	if req.VisionClient == nil || req.SourceID != v.f.source.ID || req.SourceDigest != v.f.source.SourceDigest || !req.RequireImageQuality {
		return Investigation{}, errors.New("missing frozen source/profile")
	}
	row := model.VideoVisualObservation{ID: "summary-real-observation", UserID: req.UserID, TaskID: req.TaskID, VideoRevision: v.f.task.FileMD5, ObjectKey: "visual-investigations/owner-private.jpg", StartMS: 1000, EndMS: 1001, Status: model.VisualObservationStatusObserved, RawResponseHash: "actual-observation-hash", FrameRef: "query-frame:actual-content", CacheKey: "summary-visual-fixture-cache", Observation: "连接池配置显示最大连接数。"}
	if err := v.f.db.Create(&row).Error; err != nil {
		return Investigation{}, err
	}
	v.selectedID = row.ID
	if v.afterInspect != nil {
		v.afterInspect()
		v.afterInspect = nil
	}
	return Investigation{Status: "sufficient", Observations: []VisualObservation{visualObservationFromModel(row)}, Budget: VisualBudgetUsage{FramesCaptured: 1, VLMCalls: 1, CostSource: "unknown"}}, nil
}
func enableGenerationVisualFixture(t *testing.T, f *generationFixture) *summaryVisualFixture {
	t.Helper()
	f.profiles.profile.VisionProvider = "openai_compatible"
	f.profiles.profile.VisionBaseURL = "https://example.com/v1"
	f.profiles.profile.VisionAPIKey = "fixture-only"
	f.profiles.profile.VisionModel = "frozen-vision"
	var frozen processing.GenerationSnapshot
	if err := artifact.Decode([]byte(f.job.InputSnapshotJSON), &frozen); err != nil {
		t.Fatal(err)
	}
	frozen.Intent.Options.SummaryVisualEnabled = true
	frozen.Intent.Options.OutputMode = "image_text"
	frozen.Intent.ProfileFingerprint = processing.FingerprintProfile(f.profiles.profile)
	frozen.Intent.PolicyJSON = artifact.JSON(struct {
		Recipe  string             `json:"recipe"`
		Options processing.Options `json:"options"`
	}{processing.Recipe, frozen.Intent.Options})
	f.job.InputSnapshotJSON = artifact.JSON(frozen)
	f.task.VisualDisabled = false
	f.task.VisualMode = model.VisualModeBoth
	f.task.ProcessingIntentJSON = artifact.JSON(frozen.Intent)
	if err := f.db.Save(f.task).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.db.Save(f.job).Error; err != nil {
		t.Fatal(err)
	}
	v := &summaryVisualFixture{f: f}
	f.svc = NewSummaryGenerationService(f.repos, f.profiles, v).WithVisualEnricher(v)
	return v
}
func TestSummaryVisualPublishesInspectedFramesAndRedeliverySpendsNothing(t *testing.T) {
	f := newGenerationFixture(t, false)
	v := enableGenerationVisualFixture(t, f)
	if err := f.svc.Generate(context.Background(), f.task, f.job, f.job.ProcessingToken); err != nil {
		t.Fatal(err)
	}
	if v.visualError != nil {
		t.Fatal(v.visualError)
	}
	summary, err := f.repos.Summary.FindByTaskID(f.task.ID)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := summarydoc.Parse([]byte(summary.DocumentJSON))
	if err != nil {
		t.Fatal(err)
	}
	if doc.PresentationMode != "image_text" || summary.GeneratedVersion != 2 || len(doc.Blocks[0].Figures) != 1 || v.inspectCalls != 1 || v.chatCalls != 2 || len(f.chat.calls) != 1 {
		t.Fatalf("summary=%+v visualCalls=%d/%d text=%d", summary, v.chatCalls, v.inspectCalls, len(f.chat.calls))
	}
	imageRef := doc.Blocks[0].Figures[0]
	registered, err := f.repos.ReadSummaryScreenshot(context.Background(), 7, f.task.ID, imageRef.ScreenshotRef)
	if err != nil || registered.ObservationID != v.selectedID || *imageRef.CaptureMS != 1000 {
		t.Fatalf("image=%+v err=%v", registered, err)
	}
	if strings.Contains(summary.DocumentJSON, "object_key") || strings.Contains(summary.DocumentJSON, "owner-private") {
		t.Fatal("private object leaked into document")
	}
	records, err := f.repos.AgentExecution.GetExecution(context.Background(), 7, f.job.GenerationID)
	if err != nil || records.Run.VisionCallsUsed != 1 || records.Run.FramesUsed != 1 || records.Run.VisualCallsUsed != 1 || records.Run.Status != model.AgentRunStatusCompleted {
		t.Fatalf("run=%+v err=%v", records, err)
	}
	if err := f.svc.Generate(context.Background(), f.task, f.job, f.job.ProcessingToken); err != nil {
		t.Fatal(err)
	}
	if v.inspectCalls != 1 || v.chatCalls != 2 || len(f.chat.calls) != 1 {
		t.Fatal("redelivery repeated completed model/vision calls")
	}
}
func TestSummaryVisualRejectsForeignObservationAndKeepsTextReady(t *testing.T) {
	f := newGenerationFixture(t, false)
	v := enableGenerationVisualFixture(t, f)
	v.foreignSelection = true
	if err := f.svc.Generate(context.Background(), f.task, f.job, f.job.ProcessingToken); err != nil {
		t.Fatal(err)
	}
	summary, _ := f.repos.Summary.FindByTaskID(f.task.ID)
	doc, err := summarydoc.Parse([]byte(summary.DocumentJSON))
	if err != nil || doc.PresentationMode != "text" || summary.GeneratedVersion != 1 || len(doc.Blocks[0].Figures) != 0 {
		t.Fatalf("foreign image changed text: %+v err=%v", doc, err)
	}
	var refs int64
	f.db.Model(&model.SummaryScreenshotRef{}).Count(&refs)
	if refs != 0 {
		t.Fatal("foreign reference registered")
	}
}
func TestSummaryVisualSourceReplacementCancelsOldGeneration(t *testing.T) {
	f := newGenerationFixture(t, false)
	v := enableGenerationVisualFixture(t, f)
	v.afterInspect = func() { f.db.Model(f.task).Update("active_text_source_id", "new-source") }
	if err := f.svc.Generate(context.Background(), f.task, f.job, f.job.ProcessingToken); err == nil {
		t.Fatal("source replacement published visual result")
	}
	summary, _ := f.repos.Summary.FindByTaskID(f.task.ID)
	doc, _ := summarydoc.Parse([]byte(summary.DocumentJSON))
	if doc.PresentationMode != "text" || len(doc.Blocks[0].Figures) != 0 {
		t.Fatal("stale visual wrote into latest result")
	}
}
func TestSummaryFrameQualityRejectsTinyBlankAndUnreadableFrames(t *testing.T) {
	encode := func(width, height int, contrast bool) []byte {
		img := image.NewRGBA(image.Rect(0, 0, width, height))
		if contrast {
			for y := 0; y < height; y++ {
				for x := 0; x < width; x++ {
					if x/20%2 == 0 {
						img.Set(x, y, color.White)
					}
				}
			}
		}
		var b bytes.Buffer
		if err := jpeg.Encode(&b, img, nil); err != nil {
			t.Fatal(err)
		}
		return b.Bytes()
	}
	for _, data := range [][]byte{[]byte("invalid"), encode(100, 100, true), encode(640, 360, false)} {
		if validateSummaryFrameQuality(data) == nil {
			t.Fatal("low quality capture accepted")
		}
	}
	if err := validateSummaryFrameQuality(encode(640, 360, true)); err != nil {
		t.Fatal(err)
	}
}
