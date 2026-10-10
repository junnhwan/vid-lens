package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"gorm.io/gorm"
	"vid-lens/internal/artifact"
	"vid-lens/internal/model"
	"vid-lens/internal/summarydoc"
	"vid-lens/internal/summaryselection"
)

const MaxSummarySelections = 3
const MaxSummarySelectionRunes = 3000

type SummaryContextFigure struct {
	summarydoc.Figure
	CanonicalCaption string `json:"canonical_caption"`
}

type SummaryBlockContext struct {
	TaskID                               int64                   `json:"task_id"`
	VersionRef                           model.SummaryVersionRef `json:"version_ref"`
	DocumentDigest                       string                  `json:"document_digest"`
	BlockID                              string                  `json:"block_id"`
	BlockDigest                          string                  `json:"block_digest"`
	Title                                string                  `json:"title"`
	CanonicalText                        string                  `json:"canonical_text"`
	SourceRefs                           []summarydoc.SourceRef  `json:"source_refs"`
	Figures                              []SummaryContextFigure  `json:"figures"`
	SourceTitle                          string                  `json:"source_title"`
	sourceID, sourceDigest, generationID string
}

func ParseSummaryVersionRef(value string) (model.SummaryVersionRef, error) {
	var ref model.SummaryVersionRef
	if strings.HasPrefix(value, "revision:") {
		ref.RevisionID = strings.TrimPrefix(value, "revision:")
	} else if strings.HasPrefix(value, "generated:") {
		n, err := strconv.ParseInt(strings.TrimPrefix(value, "generated:"), 10, 64)
		if err != nil || n < 0 {
			return ref, artifact.Err("invalid_version_ref", 400)
		}
		ref.GeneratedVersion = &n
	} else {
		return ref, artifact.Err("invalid_version_ref", 400)
	}
	if !validSummaryVersionRef(ref) {
		return ref, artifact.Err("invalid_version_ref", 400)
	}
	return ref, nil
}
func validSummaryVersionRef(ref model.SummaryVersionRef) bool {
	return (ref.RevisionID != "" && ref.GeneratedVersion == nil) || (ref.RevisionID == "" && ref.GeneratedVersion != nil && *ref.GeneratedVersion >= 0)
}

// SummaryBlockContext resolves one stored version, never silently substituting
// the newest generated body for a discarded historical version.
func (r *Repositories) SummaryBlockContext(ctx context.Context, owner, taskID int64, blockID string, version model.SummaryVersionRef) (SummaryBlockContext, error) {
	var out SummaryBlockContext
	if !validSummaryVersionRef(version) {
		return out, artifact.Err("invalid_version_ref", 400)
	}
	task, err := summaryTask(r.db.WithContext(ctx), owner, taskID, false)
	if err != nil {
		return out, err
	}
	var effective EffectiveSummary
	if version.RevisionID != "" {
		var row model.SummaryRevision
		if err = r.db.WithContext(ctx).Where("id=? AND user_id=? AND task_id=?", version.RevisionID, owner, taskID).First(&row).Error; err != nil {
			return out, hideMissing(err)
		}
		if err = loadEffectiveContent(&effective, row.Content, row.DocumentJSON, row.ContentHashKind, row.ContentDigest); err != nil {
			return out, err
		}
		effective.GenerationID = row.GenerationID
	} else {
		row, err := generatedSummary(r.db.WithContext(ctx), task)
		if err != nil {
			return out, err
		}
		if row == nil {
			return out, artifact.Err("source_not_ready", 422)
		}
		if row.GeneratedVersion != *version.GeneratedVersion {
			return out, artifact.Err("summary_selection_stale", 409)
		}
		if err = loadEffectiveContent(&effective, row.Content, row.DocumentJSON, row.ContentHashKind, row.ContentDigest); err != nil {
			return out, err
		}
		effective.GenerationID = row.GenerationID
	}
	out = SummaryBlockContext{TaskID: taskID, VersionRef: version, DocumentDigest: effective.ContentDigest, BlockID: blockID, SourceTitle: task.Title, SourceRefs: []summarydoc.SourceRef{}, Figures: []SummaryContextFigure{}, generationID: effective.GenerationID}
	if effective.Document != nil {
		doc := effective.Document
		auth, err := r.SummaryValidationContext(ctx, owner, taskID, doc.SourceID, doc.SourceDigest, effective.GenerationID)
		if err != nil {
			return out, err
		}
		if err = summarydoc.Validate(*doc, auth); err != nil {
			return out, artifact.Err("source_changed", 409)
		}
		out.sourceID, out.sourceDigest = doc.SourceID, doc.SourceDigest
		switch blockID {
		case "summary-title":
			out.Title = "标题"
			out.CanonicalText = doc.Title
		case "summary-overview":
			out.Title = "概览"
			out.CanonicalText = summaryselection.Text(doc.Overview)
		default:
			found := false
			for _, block := range doc.Blocks {
				if block.ID == blockID {
					found = true
					out.Title = block.Title
					out.SourceRefs = block.SourceRefs
					for _, figure := range block.Figures {
						out.Figures = append(out.Figures, SummaryContextFigure{Figure: figure, CanonicalCaption: summaryselection.Text(figure.Caption)})
					}
					out.CanonicalText = summaryselection.Text(block.BodyMarkdown)
					for _, f := range block.Figures {
						out.CanonicalText = strings.TrimSpace(out.CanonicalText + "\n" + summaryselection.Text(f.Caption))
					}
					break
				}
			}
			if !found {
				return out, artifact.Err("not_found", 404)
			}
		}
	} else {
		parts := strings.Split(strings.ReplaceAll(effective.Content, "\r\n", "\n"), "\n\n")
		found := false
		if blockID == "summary-title" {
			out.Title = "标题"
			out.CanonicalText = summaryselection.Text(parts[0])
			found = true
		}
		if blockID == "summary-overview" {
			out.Title = "概览"
			out.CanonicalText = summaryselection.Text(effective.Content)
			found = true
		}
		for i, part := range parts {
			if blockID == fmt.Sprintf("legacy-%d", i) {
				out.Title = fmt.Sprintf("段落 %d", i+1)
				out.CanonicalText = summaryselection.Text(part)
				found = true
				break
			}
		}
		if !found {
			return out, artifact.Err("not_found", 404)
		}
	}
	out.BlockDigest = artifact.Hash(artifact.JSON(struct {
		Title, Text string
		Refs        []summarydoc.SourceRef
		Figures     []SummaryContextFigure
	}{out.Title, out.CanonicalText, out.SourceRefs, out.Figures}))
	return out, nil
}

func (r *Repositories) FreezeSummarySelections(ctx context.Context, owner, sessionID int64, refs []model.SummaryContextRef) ([]model.ContextAnnotation, error) {
	if len(refs) > MaxSummarySelections {
		return nil, artifact.Err("summary_selection_limit", 400)
	}
	session, err := r.Chat.FindSessionForUser(owner, sessionID)
	if err != nil {
		return nil, err
	}
	if session == nil {
		return nil, artifact.Err("not_found", 404)
	}
	out := make([]model.ContextAnnotation, 0, len(refs))
	total := 0
	for _, ref := range refs {
		if ref.Kind != "summary_selection" && ref.Kind != "summary_screenshot" {
			return nil, artifact.Err("invalid_context_ref", 400)
		}
		if err = authorizeAnnotationTask(r.db.WithContext(ctx), owner, session, ref.TaskID); err != nil {
			return nil, err
		}
		block, err := r.SummaryBlockContext(ctx, owner, ref.TaskID, ref.BlockID, ref.VersionRef)
		if err != nil {
			return nil, err
		}
		if ref.DocumentDigest != block.DocumentDigest || ref.BlockDigest != block.BlockDigest {
			return nil, artifact.Err("summary_selection_stale", 409)
		}
		runes := []rune(block.CanonicalText)
		if !utf8.ValidString(ref.Quote) || ref.TextStart < 0 || ref.TextEnd <= ref.TextStart || ref.TextEnd > len(runes) {
			return nil, artifact.Err("invalid_selection_range", 400)
		}
		actual := string(runes[ref.TextStart:ref.TextEnd])
		if ref.Quote != actual {
			return nil, artifact.Err("summary_quote_mismatch", 409)
		}
		total += ref.TextEnd - ref.TextStart
		if total > MaxSummarySelectionRunes {
			return nil, artifact.Err("summary_selection_limit", 400)
		}
		ref.Quote = actual
		annotation := model.ContextAnnotation{SummaryContextRef: ref, SourceTitle: block.SourceTitle, BlockTitle: block.Title, SourceRefs: block.SourceRefs, SourceID: block.sourceID, SourceDigest: block.sourceDigest, GenerationID: block.generationID, Provenance: "derived_summary"}
		if ref.Kind == "summary_screenshot" {
			if ref.ScreenshotRef == "" {
				return nil, artifact.Err("invalid_context_ref", 400)
			}
			found := false
			for _, figure := range block.Figures {
				if figure.ScreenshotRef != ref.ScreenshotRef {
					continue
				}
				image, err := r.ReadSummaryScreenshot(ctx, owner, ref.TaskID, ref.ScreenshotRef)
				if err != nil {
					return nil, err
				}
				annotation.Images = append(annotation.Images, model.AnnotationImage{ScreenshotRef: image.ID, ObservationID: image.ObservationID, CaptureMS: image.CaptureMS, Caption: figure.Caption, Alt: figure.Alt, Provenance: "registered_video_frame_with_derived_caption", InputMode: "caption_only"})
				found = true
				break
			}
			if !found {
				return nil, artifact.Err("invalid_context_ref", 400)
			}
		} else if ref.ScreenshotRef != "" {
			return nil, artifact.Err("invalid_context_ref", 400)
		}
		for _, image := range annotation.Images {
			total += utf8.RuneCountInString(image.Caption) + utf8.RuneCountInString(image.Alt)
		}
		if total > MaxSummarySelectionRunes {
			return nil, artifact.Err("summary_selection_limit", 400)
		}
		out = append(out, annotation)
	}
	return out, nil
}
func authorizeAnnotationTask(tx *gorm.DB, owner int64, session *model.ChatSession, taskID int64) error {
	if taskID <= 0 {
		return artifact.Err("invalid_context_ref", 400)
	}
	if _, err := summaryTask(tx, owner, taskID, false); err != nil {
		return err
	}
	switch session.ScopeType {
	case model.ChatScopeVideo:
		if session.TaskID != taskID {
			return artifact.Err("context_outside_scope", 403)
		}
	case model.ChatScopeKnowledgeBase:
		var count int64
		if err := tx.Model(&model.KnowledgeBaseVideo{}).Where("knowledge_base_id=? AND task_id=?", session.KnowledgeBaseID, taskID).Count(&count).Error; err != nil {
			return err
		}
		if count != 1 {
			return artifact.Err("context_outside_scope", 403)
		}
		var kb model.KnowledgeBase
		if err := tx.Where("id=? AND user_id=?", session.KnowledgeBaseID, owner).First(&kb).Error; err != nil {
			return hideMissing(err)
		}
	case model.ChatScopeVideoLibrary:
	default:
		return artifact.Err("context_outside_scope", 403)
	}
	return nil
}

// AuthorizeFrozenAnnotations rechecks current access while retaining accepted
// content and versions. It never replaces an old quote with the latest summary.
func (r *Repositories) AuthorizeFrozenAnnotations(ctx context.Context, owner, sessionID int64, annotations []model.ContextAnnotation) error {
	if len(annotations) == 0 {
		return nil
	}
	if r.Chat == nil {
		return artifact.Err("context_unavailable", 503)
	}
	session, err := r.Chat.FindSessionForUser(owner, sessionID)
	if err != nil {
		return err
	}
	if session == nil {
		return artifact.Err("not_found", 404)
	}
	return authorizeFrozenAnnotations(r.db.WithContext(ctx), owner, session, annotations)
}
func authorizeFrozenAnnotations(tx *gorm.DB, owner int64, session *model.ChatSession, annotations []model.ContextAnnotation) error {
	if len(annotations) > MaxSummarySelections {
		return artifact.Err("summary_selection_limit", 400)
	}
	repos := NewRepositories(tx)
	total := 0
	for _, a := range annotations {
		if err := authorizeAnnotationTask(tx, owner, session, a.TaskID); err != nil {
			return err
		}
		if a.Provenance != "derived_summary" || !validSummaryVersionRef(a.VersionRef) || (a.Kind != "summary_selection" && a.Kind != "summary_screenshot") {
			return artifact.Err("invalid_context_ref", 400)
		}
		total += utf8.RuneCountInString(a.Quote)
		for _, image := range a.Images {
			total += utf8.RuneCountInString(image.Caption) + utf8.RuneCountInString(image.Alt)
		}
		if total > MaxSummarySelectionRunes {
			return artifact.Err("summary_selection_limit", 400)
		}
		if a.SourceID != "" {
			source, err := repos.TextSource.Read(tx.Statement.Context, owner, a.TaskID, a.SourceID)
			if err != nil {
				return err
			}
			if source.SourceDigest != a.SourceDigest {
				return artifact.Err("source_changed", 409)
			}
		}
		for _, image := range a.Images {
			ref, err := repos.ReadSummaryScreenshot(tx.Statement.Context, owner, a.TaskID, image.ScreenshotRef)
			if err != nil {
				return err
			}
			if ref.SourceID != a.SourceID || ref.SourceDigest != a.SourceDigest || ref.GenerationID != a.GenerationID || ref.ObservationID != image.ObservationID || ref.CaptureMS != image.CaptureMS {
				return artifact.Err("source_changed", 409)
			}
		}
	}
	return nil
}
func validateMessageAnnotations(tx *gorm.DB, owner int64, session *model.ChatSession, message *model.ChatMessage) error {
	if message.ContextAnnotationsJSON == nil {
		return nil
	}
	var annotations []model.ContextAnnotation
	if err := json.Unmarshal([]byte(*message.ContextAnnotationsJSON), &annotations); err != nil {
		return artifact.Err("invalid_context_ref", 400)
	}
	return authorizeFrozenAnnotations(tx, owner, session, annotations)
}
