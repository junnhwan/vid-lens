export type RequestStatus = "waiting_source" | "generating" | "completed" | "failed" | "canceled"
export type TurnStatus =
  | "queued"
  | "running"
  | "waiting_dependency"
  | "retry_wait"
  | "cancel_requested"
  | "completed"
  | "failed"
  | "canceled"
export type RunStatus = TurnStatus | "succeeded"
export type Phase = "prepare_source" | "overview" | "investigate" | "compose" | "validate" | "publish" | "answer"
export type WaitReason = "dispatch" | "capacity" | "media_restore"
export type HistoryStatus = "none" | "available" | "partial" | "expired"
export type StepStatus = "queued" | "running" | "completed" | "failed" | "canceled"
export type EvidenceStatus = "not_assessed" | "supported" | "unresolved" | "not_applicable"

export interface Problem {
  type?: string
  title?: string
  status: number
  detail: string
  instance?: string
  code: string
  trace_id?: string
  meta?: {
    field_errors?: { field: string; message: string }[]
    active_turn_id?: string
    restart_url?: string
    [key: string]: unknown
  } | null
}

export interface Capabilities {
  detail_levels?: DetailLevel[]
  default_detail_level?: DetailLevel
  max_purpose_chars?: number
  purpose_input?: boolean
  model_selection?: boolean
  article_export?: { formats: string[] }
  external_sources?: { enabled: boolean; search_enabled: boolean }
  supported_platforms: string[]
  accepting_requests: boolean
  question_chars_max: number
  selection_chars_max: number
  idempotency_window_seconds: number
}

export interface SessionResponse {
  schema_version: number
  session: { id: string; expires_at: string }
  csrf_token: string
  capabilities: Capabilities
}

export interface Range {
  start_ms: number
  end_ms: number
}

export interface Part {
  key: string
  label: string
}

export interface Provenance {
  origin: string
  coverage: string
  language: string | null
}

export interface SourceSummary {
  source_id: string
  snapshot_id: string | null
  platform: string
  canonical_url: string
  title: string | null
  duration_ms: number | null
  part: Part | null
  text_provenance: Provenance | null
}

export interface Failure {
  diagnostic_id?: string
  code: string
  message: string
  can_retry: boolean
  retry_at: string | null
}

export interface ExecutionView {
  id: string
  kind: "source.prepare" | "article.generate" | "answer.generate" | string
  status: RunStatus
  phase: Phase | null
  retry_at: string | null
  wait_reason: WaitReason | null
}

export interface FrameUrls {
  thumbnail: string | null
  full: string | null
}

export interface Frame {
  id: string
  snapshot_id: string
  time_ms: number
  requested_time_ms: number
  width: number
  height: number
  mime_type: string
  caption: string
  crop: {
    parent_frame_id: string
    x: number
    y: number
    width: number
    height: number
  } | null
  replaces_frame_id: string | null
  availability: "ready" | "expired" | "unavailable" | string
  unavailable_reason: string | null
  expires_at: string | null
  urls: FrameUrls
}

export interface Outcome {
  message: string
  evidence_status: EvidenceStatus | string
}

export interface ActivityStep {
  kind?: string
  model_call?: number
  submission?: number
  plan?: ExecutionPlan | null
  id: string
  run_id: string
  segment_no: number
  first_seq: number
  last_seq: number
  phase: string
  title: string
  purpose: string | null
  status: StepStatus | string
  source_range: Range | null
  frame_ids: string[]
  frames: Frame[]
  outcome: Outcome | null
  retry_of_step_id: string | null
  started_at: string | null
  finished_at: string | null
}

export interface ExecutionPlan {
  items: { id: string; title: string; status: string; detail?: string }[]
  sections?: { id: string; title: string; status: string }[]
  wrap_up: boolean
  note?: string
}

export interface ActivitySummary {
  model_calls?: number
  model_submissions?: number | null
  plan?: ExecutionPlan | null
  current_step_id: string | null
  recent_steps: ActivityStep[]
  has_more: boolean
  history_url: string
  history_status: HistoryStatus | string
  history_expires_at: string | null
}

export interface StreamPosition {
  cursor: string
  revision: number
  run_id: string | null
  seq: number
  url?: string
}

export interface RequestActions {
  can_cancel: boolean
  can_retry: boolean
  can_read_article: boolean
}

export interface RequestBody {
  detail_level?: DetailLevel | ""
  detail_policy_revision?: string
  purpose?: string
  generation_model?: GenerationModel
  id: string
  revision: number
  input_url: string
  status: RequestStatus | string
  reuse: "pending" | "none" | "active_work" | "published_article" | string
  retry_of_id: string | null
  article_id: string | null
  created_at: string
  updated_at: string
}

export interface GenerationModel {
  id: string
  revision_id: string
  display_name: string
  brand_id: string
  description?: string
  selection_locked?: boolean
  legacy?: boolean
}
export interface ModelCatalog {
  revision: number
  default_model_id: string | null
  selection_enabled: boolean
  models: GenerationModel[]
}
export interface GenerationInput {
  detail_level?: DetailLevel
  purpose?: string
  model_id?: string
  model_revision_id?: string
}

export interface RequestView {
  server_now?: string
  schema_version: number
  request: RequestBody
  source: SourceSummary | null
  execution: ExecutionView | null
  activity: ActivitySummary
  failure: Failure | null
  actions: RequestActions
  stream: StreamPosition
}

export interface TurnActions {
  can_cancel: boolean
  can_retry: boolean
  can_submit_next: boolean
}

export interface Passage {
  section_id: string
  block_id: string
  quote: string
}

export interface QuestionContext {
  frame_id: string | null
  passage: Passage | null
}

export interface Inline {
  type: string
  text?: string
  marks?: string[]
  citation_id?: string
  url?: string
}

export interface Block {
  id: string
  type: string
  origin: string
  content?: Inline[]
  ordered?: boolean
  items?: { id: string; content: Inline[] }[]
  language?: string | null
  text?: string
  columns?: string[]
  rows?: { cells: string[]; citation_ids: string[] }[]
  frame_ids?: string[]
  layout?: string
  caption?: string
  citation_ids?: string[]
}

export interface Citation {
  reference_id?: string
  content_sha256?: string
  url?: string
  title?: string
  read_at?: string
  id: string
  kind: string
  label: string
  snapshot_id?: string
  range?: Range | null
  segment_ids?: string[]
  quote?: string | null
  text_origin?: string
  frame_id?: string
  time_ms?: number | null
  article_id?: string
  section_id?: string
  block_id?: string
}

export interface Limitation {
  code: string
  message: string
  section_ids: string[]
  citation_ids: string[]
}

export interface MindMap {
  root_id: string
  nodes: {
    id: string
    parent_id: string | null
    label: string
    section_id: string | null
  }[]
}

export interface Article {
  schema_version: number
  id: string
  snapshot_id: string
  source: SourceSummary
  title: string
  published_at: string
  sections: { id: string; title: string; blocks: Block[] }[]
  mind_map: MindMap | null
  citations: Record<string, Citation>
  limitations: Limitation[]
  quality: string
}

export interface MediaCapability {
  status: string
  can_inspect_now: boolean
  can_restore_for_question: boolean
}

export interface ArticleResponse {
  detail_level?: DetailLevel | ""
  detail_policy_revision?: string
  generation_model?: GenerationModel
  purpose?: string
  qa_available?: boolean
  schema_version: number
  article: Article
  frames: Frame[]
  media: MediaCapability
}

export type DetailLevel = "concise" | "standard" | "detailed"

export interface Answer {
  kind: string
  summary: string
  blocks: Block[]
  citations: Record<string, Citation>
  frames: Frame[]
  limitations: Limitation[]
}

export interface TurnBody {
  id: string
  conversation_id: string
  request_id: string
  article_id: string
  snapshot_id: string
  turn_no: number
  revision: number
  status: TurnStatus | string
  question: string
  context: QuestionContext
  retry_of_id: string | null
  answer: Answer | null
  created_at: string
  updated_at: string
}

export interface TurnView {
  server_now?: string
  schema_version: number
  turn: TurnBody
  execution: ExecutionView | null
  activity: ActivitySummary
  failure: Failure | null
  actions: TurnActions
  stream: StreamPosition
}

export interface Conversation {
  id: string
  request_id: string
  article_id: string
  snapshot_id: string
  last_turn_id: string | null
  active_turn_id: string | null
  created_at: string
}

export interface ConversationResponse {
  schema_version: number
  conversation: Conversation
}

export interface ActivityPage {
  schema_version: number
  items: ActivityStep[]
  next_before: string | null
  has_more: boolean
  through_cursor: string
  history_status: HistoryStatus | string
  history_expires_at: string | null
}

export interface TurnPage {
  schema_version: number
  items: TurnView[]
  next_before_turn_no: number | null
  has_more: boolean
}

export interface Accepted<T> {
  status: number
  replayed: boolean
  expiresAt: string | null
  location: string | null
  body: T
}

export interface StreamScope {
  type: "request" | "turn" | string
  id: string
}

export interface StreamEnvelope {
  schema_version: number
  scope: StreamScope
  position?: {
    cursor: string
    revision: number
    run_id: string | null
    seq: number
  }
  payload: Record<string, unknown>
}
