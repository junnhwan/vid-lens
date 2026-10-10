package model

import "vid-lens/internal/summarydoc"

// SummaryVersionRef deliberately distinguishes immutable revisions from the
// current generated version, whose historical body may no longer exist.
type SummaryVersionRef struct {
	RevisionID       string `json:"revision_id,omitempty"`
	GeneratedVersion *int64 `json:"generated_version,omitempty"`
}

type SummaryContextRef struct {
	Kind           string            `json:"kind"`
	TaskID         int64             `json:"task_id"`
	VersionRef     SummaryVersionRef `json:"version_ref"`
	DocumentDigest string            `json:"document_digest"`
	BlockID        string            `json:"block_id"`
	BlockDigest    string            `json:"block_digest"`
	TextStart      int               `json:"text_start"`
	TextEnd        int               `json:"text_end"`
	Quote          string            `json:"quote"`
	ScreenshotRef  string            `json:"screenshot_ref,omitempty"`
}

type AnnotationImage struct {
	ScreenshotRef string `json:"screenshot_ref"`
	ObservationID string `json:"observation_id"`
	CaptureMS     int64  `json:"capture_ms"`
	Caption       string `json:"caption"`
	Alt           string `json:"alt"`
	Provenance    string `json:"provenance"`
	InputMode     string `json:"input_mode"`
}

type ContextAnnotation struct {
	SummaryContextRef
	SourceTitle  string                 `json:"source_title"`
	BlockTitle   string                 `json:"block_title"`
	SourceRefs   []summarydoc.SourceRef `json:"source_refs"`
	SourceID     string                 `json:"source_id,omitempty"`
	SourceDigest string                 `json:"source_digest,omitempty"`
	GenerationID string                 `json:"generation_id,omitempty"`
	Images       []AnnotationImage      `json:"images,omitempty"`
	Provenance   string                 `json:"provenance"`
}
