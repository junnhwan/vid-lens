package domain

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"
)

func ID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b[:])
}
func JSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}
func Terminal(s string) bool {
	return s == "succeeded" || s == "completed" || s == "failed" || s == "canceled"
}

type Problem struct {
	Status int            `json:"status"`
	Code   string         `json:"code"`
	Detail string         `json:"detail"`
	Meta   map[string]any `json:"meta,omitempty"`
}

func (p *Problem) Error() string { return p.Code + ": " + p.Detail }
func Err(status int, code, msg string) error {
	return &Problem{Status: status, Code: code, Detail: msg}
}

var ErrFenced = errors.New("execution ownership lost")

type WorkError struct {
	Code       string
	Message    string
	Transient  bool
	RetryAfter time.Duration
}

func (e *WorkError) Error() string { return e.Code + ": " + e.Message }

type Failure struct {
	DiagnosticID string     `json:"diagnostic_id,omitempty"`
	Code         string     `json:"code"`
	Message      string     `json:"message"`
	CanRetry     bool       `json:"can_retry"`
	RetryAt      *time.Time `json:"retry_at"`
}
type Range struct {
	StartMS int64 `json:"start_ms"`
	EndMS   int64 `json:"end_ms"`
}
type Part struct {
	Key   string `json:"key"`
	Label string `json:"label"`
}
type Provenance struct {
	Origin   string  `json:"origin"`
	Coverage string  `json:"coverage"`
	Language *string `json:"language"`
}
type Source struct {
	SourceID       string      `json:"source_id"`
	SnapshotID     *string     `json:"snapshot_id"`
	Platform       string      `json:"platform"`
	CanonicalURL   string      `json:"canonical_url"`
	Title          *string     `json:"title"`
	DurationMS     *int64      `json:"duration_ms"`
	Part           *Part       `json:"part"`
	TextProvenance *Provenance `json:"text_provenance"`
}
type Segment struct {
	ID      string `json:"id"`
	Ordinal int    `json:"ordinal"`
	StartMS int64  `json:"start_ms"`
	EndMS   int64  `json:"end_ms"`
	Text    string `json:"text"`
	Origin  string `json:"origin"`
}
type Snapshot struct {
	ID         string          `json:"id"`
	SourceID   string          `json:"source_id"`
	VideoHash  string          `json:"video_hash"`
	TextHash   string          `json:"text_hash"`
	DurationMS int64           `json:"duration_ms"`
	Source     Source          `json:"source"`
	Metadata   json.RawMessage `json:"metadata"`
	CreatedAt  time.Time       `json:"created_at"`
}
type ExecutionSegment struct {
	RunID      string `json:"run_id"`
	SegmentNo  int    `json:"segment_no"`
	ThroughSeq *int64 `json:"visible_through_seq"`
}
type Request struct {
	GenerationSpec *GenerationSpec    `json:"generation_spec"`
	ID             string             `json:"id"`
	SessionID      string             `json:"session_id"`
	InputURL       string             `json:"input_url"`
	SourceID       string             `json:"source_id"`
	SnapshotID     *string            `json:"snapshot_id"`
	ActiveRunID    *string            `json:"active_run_id"`
	ArticleID      *string            `json:"article_id"`
	Revision       int64              `json:"revision"`
	Status         string             `json:"status"`
	Reuse          string             `json:"reuse"`
	RetryOfID      *string            `json:"retry_of_id"`
	ConfigID       string             `json:"config_id"`
	Segments       []ExecutionSegment `json:"segments"`
	Failure        *Failure           `json:"failure"`
	DeadlineAt     time.Time          `json:"deadline_at"`
	CreatedAt      time.Time          `json:"created_at"`
	UpdatedAt      time.Time          `json:"updated_at"`
}
type Run struct {
	ID                 string          `json:"id"`
	Kind               string          `json:"kind"`
	WorkKey            string          `json:"work_key"`
	SourceID           string          `json:"source_id"`
	SnapshotID         *string         `json:"snapshot_id"`
	ConfigID           string          `json:"config_id"`
	Status             string          `json:"status"`
	Phase              *string         `json:"phase"`
	DispatchGeneration int64           `json:"dispatch_generation"`
	ExecutionEpoch     int64           `json:"execution_epoch"`
	AttemptNo          int             `json:"attempt_no"`
	FailureCount       int             `json:"failure_count"`
	ActiveMS           int64           `json:"active_ms"`
	DeadlineAt         time.Time       `json:"deadline_at"`
	QueuedAt           time.Time       `json:"queued_at"`
	LeaseOwner         *string         `json:"lease_owner"`
	LeaseExpiresAt     *time.Time      `json:"lease_expires_at"`
	LastEventSeq       int64           `json:"last_event_seq"`
	Checkpoint         json.RawMessage `json:"checkpoint"`
	Failure            *Failure        `json:"failure"`
	ResultID           *string         `json:"result_id"`
	DependencyRunID    *string         `json:"dependency_run_id"`
	RestoreUsed        bool            `json:"restore_used"`
	RetryNotBefore     *time.Time      `json:"retry_not_before"`
	RetryAt            *time.Time      `json:"retry_at"`
	CreatedAt          time.Time       `json:"created_at"`
	UpdatedAt          time.Time       `json:"updated_at"`
}
type Claim struct {
	Run       Run
	Owner     string
	Epoch     int64
	AttemptID string
}
type Crop struct {
	ParentFrameID string `json:"parent_frame_id"`
	X             int    `json:"x"`
	Y             int    `json:"y"`
	Width         int    `json:"width"`
	Height        int    `json:"height"`
}
type Asset struct {
	ID              string     `json:"id"`
	SnapshotID      *string    `json:"snapshot_id"`
	SourceID        string     `json:"source_id"`
	RunID           string     `json:"run_id"`
	Kind            string     `json:"kind"`
	StorageKey      string     `json:"storage_key"`
	SHA256          string     `json:"sha256"`
	MIME            string     `json:"mime"`
	Bytes           int64      `json:"bytes"`
	State           string     `json:"state"`
	TimeMS          *int64     `json:"time_ms"`
	RequestedTimeMS *int64     `json:"requested_time_ms"`
	Width           int        `json:"width"`
	Height          int        `json:"height"`
	Caption         string     `json:"caption"`
	Crop            *Crop      `json:"crop"`
	ReplacesFrameID *string    `json:"replaces_frame_id"`
	ExpiresAt       *time.Time `json:"expires_at"`
	CreatedAt       time.Time  `json:"created_at"`
}
type Frame struct {
	ID                string             `json:"id"`
	SnapshotID        string             `json:"snapshot_id"`
	TimeMS            int64              `json:"time_ms"`
	RequestedTimeMS   int64              `json:"requested_time_ms"`
	Width             int                `json:"width"`
	Height            int                `json:"height"`
	MIMEType          string             `json:"mime_type"`
	Caption           string             `json:"caption"`
	Crop              *Crop              `json:"crop"`
	ReplacesFrameID   *string            `json:"replaces_frame_id"`
	Availability      string             `json:"availability"`
	UnavailableReason *string            `json:"unavailable_reason"`
	ExpiresAt         *time.Time         `json:"expires_at"`
	URLs              map[string]*string `json:"urls"`
}
type Outcome struct {
	Message        string `json:"message"`
	EvidenceStatus string `json:"evidence_status"`
}
type Usage struct {
	Input  int64 `json:"input"`
	Output int64 `json:"output"`
}
type Step struct {
	Kind          string         `json:"kind,omitempty"`
	ModelCall     int            `json:"model_call,omitempty"`
	Submission    int            `json:"submission,omitempty"`
	Plan          *ExecutionPlan `json:"plan,omitempty"`
	Usage         *Usage         `json:"usage,omitempty"`
	ID            string         `json:"id"`
	RunID         string         `json:"run_id"`
	SegmentNo     int            `json:"segment_no"`
	FirstSeq      int64          `json:"first_seq"`
	LastSeq       int64          `json:"last_seq"`
	Phase         string         `json:"phase"`
	Title         string         `json:"title"`
	Purpose       *string        `json:"purpose"`
	Status        string         `json:"status"`
	SourceRange   *Range         `json:"source_range"`
	FrameIDs      []string       `json:"frame_ids"`
	Frames        []Frame        `json:"frames"`
	Outcome       *Outcome       `json:"outcome"`
	RetryOfStepID *string        `json:"retry_of_step_id"`
	StartedAt     *time.Time     `json:"started_at"`
	FinishedAt    *time.Time     `json:"finished_at"`
}

type PlanItem struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
}
type ExecutionPlan struct {
	OpenEnded bool       `json:"open_ended,omitempty"`
	Items     []PlanItem `json:"items"`
	Sections  []PlanItem `json:"sections,omitempty"`
	WrapUp    bool       `json:"wrap_up"`
	Note      string     `json:"note,omitempty"`
}
type Inline struct {
	Type       string   `json:"type"`
	Text       string   `json:"text,omitempty"`
	Marks      []string `json:"marks,omitempty"`
	CitationID string   `json:"citation_id,omitempty"`
	URL        string   `json:"url,omitempty"`
}
type ListItem struct {
	ID      string   `json:"id"`
	Content []Inline `json:"content"`
}
type TableRow struct {
	Cells       []string `json:"cells"`
	CitationIDs []string `json:"citation_ids"`
}
type Block struct {
	ID          string     `json:"id"`
	Type        string     `json:"type"`
	Origin      string     `json:"origin"`
	Content     []Inline   `json:"content,omitempty"`
	Ordered     *bool      `json:"ordered,omitempty"`
	Items       []ListItem `json:"items,omitempty"`
	Language    *string    `json:"language,omitempty"`
	Text        string     `json:"text,omitempty"`
	Columns     []string   `json:"columns,omitempty"`
	Rows        []TableRow `json:"rows,omitempty"`
	FrameIDs    []string   `json:"frame_ids,omitempty"`
	Layout      string     `json:"layout,omitempty"`
	Caption     string     `json:"caption,omitempty"`
	CitationIDs []string   `json:"citation_ids,omitempty"`
}
type Section struct {
	ID     string  `json:"id"`
	Title  string  `json:"title"`
	Blocks []Block `json:"blocks"`
}
type Citation struct {
	ReferenceID   string     `json:"reference_id,omitempty"`
	ContentSHA256 string     `json:"content_sha256,omitempty"`
	URL           string     `json:"url,omitempty"`
	Title         string     `json:"title,omitempty"`
	ReadAt        *time.Time `json:"read_at,omitempty"`
	ID            string     `json:"id"`
	Kind          string     `json:"kind"`
	Label         string     `json:"label"`
	SnapshotID    string     `json:"snapshot_id,omitempty"`
	Range         *Range     `json:"range,omitempty"`
	SegmentIDs    []string   `json:"segment_ids,omitempty"`
	Quote         *string    `json:"quote" jsonschema:"description=For video_text use null for paraphrases; a string must be a verbatim contiguous source quote. For article_passage use the exact article quote."`
	TextOrigin    string     `json:"text_origin,omitempty"`
	FrameID       string     `json:"frame_id,omitempty"`
	TimeMS        *int64     `json:"time_ms,omitempty"`
	ArticleID     string     `json:"article_id,omitempty"`
	SectionID     string     `json:"section_id,omitempty"`
	BlockID       string     `json:"block_id,omitempty"`
}
type Limitation struct {
	Code        string   `json:"code"`
	Message     string   `json:"message"`
	SectionIDs  []string `json:"section_ids"`
	CitationIDs []string `json:"citation_ids"`
}
type MapNode struct {
	ID        string  `json:"id"`
	ParentID  *string `json:"parent_id"`
	Label     string  `json:"label"`
	SectionID *string `json:"section_id"`
}
type MindMap struct {
	RootID string    `json:"root_id"`
	Nodes  []MapNode `json:"nodes"`
}

// ExportRendererVersion identifies reusable offline payloads, independently of
// the immutable article and the schema version of its content.
const ExportRendererVersion = 3

type Article struct {
	DetailLevel          string              `json:"detail_level,omitempty"`
	DetailPolicyRevision string              `json:"detail_policy_revision,omitempty"`
	SchemaVersion        int                 `json:"schema_version"`
	ID                   string              `json:"id"`
	SnapshotID           string              `json:"snapshot_id"`
	Source               Source              `json:"source"`
	Title                string              `json:"title"`
	PublishedAt          time.Time           `json:"published_at"`
	Sections             []Section           `json:"sections"`
	MindMap              *MindMap            `json:"mind_map"`
	Citations            map[string]Citation `json:"citations"`
	Limitations          []Limitation        `json:"limitations"`
	Quality              string              `json:"quality"`
}
type Passage struct {
	SectionID string `json:"section_id"`
	BlockID   string `json:"block_id"`
	Quote     string `json:"quote"`
}
type QuestionContext struct {
	FrameID  *string   `json:"frame_id"`
	Passage  *Passage  `json:"passage"`
	Passages []Passage `json:"passages,omitempty"`
}
type QuestionInput struct {
	AllowExternalSources bool            `json:"allow_external_sources,omitempty"`
	ReferenceURLs        []string        `json:"reference_urls,omitempty"`
	Question             string          `json:"question"`
	Context              QuestionContext `json:"context"`
	ExpectedLastTurnID   *string         `json:"expected_last_turn_id"`
}
type AnswerPart struct {
	Title    string   `json:"title"`
	BlockIDs []string `json:"block_ids"`
}
type Answer struct {
	Kind        string              `json:"kind"`
	Summary     string              `json:"summary"`
	Blocks      []Block             `json:"blocks"`
	Parts       []AnswerPart        `json:"parts,omitempty"`
	Citations   map[string]Citation `json:"citations"`
	Frames      []Frame             `json:"frames"`
	Limitations []Limitation        `json:"limitations"`
}
type Conversation struct {
	ID           string    `json:"id"`
	SessionID    string    `json:"session_id,omitempty"`
	RequestID    string    `json:"request_id"`
	ArticleID    string    `json:"article_id"`
	SnapshotID   string    `json:"snapshot_id"`
	LastTurnID   *string   `json:"last_turn_id"`
	ActiveTurnID *string   `json:"active_turn_id"`
	NextTurnNo   int64     `json:"next_turn_no,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}
type Turn struct {
	AllowExternalSources bool            `json:"allow_external_sources"`
	ReferenceURLs        []string        `json:"reference_urls"`
	ID                   string          `json:"id"`
	ConversationID       string          `json:"conversation_id"`
	RequestID            string          `json:"request_id"`
	ArticleID            string          `json:"article_id"`
	SnapshotID           string          `json:"snapshot_id"`
	RunID                string          `json:"run_id"`
	TurnNo               int64           `json:"turn_no"`
	Revision             int64           `json:"revision"`
	Status               string          `json:"status"`
	Question             string          `json:"question"`
	Context              QuestionContext `json:"context"`
	RetryOfID            *string         `json:"retry_of_id"`
	Answer               *Answer         `json:"answer"`
	Failure              *Failure        `json:"failure"`
	CreatedAt            time.Time       `json:"created_at"`
	UpdatedAt            time.Time       `json:"updated_at"`
	ExpiresAt            *time.Time      `json:"expires_at"`
}

func Ptr[T any](v T) *T { return &v }

// Old runs keep their original failure evidence. This one legacy catch-all
// can be explicitly retried under the corrected submission protocol.
func RetryableFailure(f *Failure) bool {
	return f != nil && (f.CanRetry || f.Code == "EVIDENCE_INSUFFICIENT" && f.Message == "纠正一次后仍没有合格的正式提交。")
}

const ArticleDraftProtocol = "vidlens-article-draft-v1"
