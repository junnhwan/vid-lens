// 后端契约类型 —— 以 Go struct 实际 JSON tag 为准（agent 已核对 handler/service/model 三层）。
// 所有响应被统一外层 {code,message,data} 包裹；此处只列 data 内部结构。

// ============ 通用 ============
export interface ApiEnvelope<T> {
  code: number
  message: string
  data?: T
}

// ============ 用户 ============
export interface User {
  id: number
  username: string
  nickname: string
  avatar: string
  role: 'USER' | 'ADMIN' | 'DEMO'
  created_at: string
  updated_at: string
}
export interface AuthResult {
  token: string
  user: User
}

// ============ AI Profile（BYOK）============
// 注意：没有 type 字段。一个 profile 同时含 llm/asr/embedding/vision 四组配置。
// is_default 是单个 bool（snake_case），设新默认时后端把同类其它置 false——
// 但"同类"在后端按 profile 整体，不按 type 分。
export interface AIProfile {
  id: number
  name: string
  // LLM 组
  llm_provider: string
  llm_base_url: string
  llm_api_key_masked: string // 脱敏，回显用
  llm_model: string
  // ASR 组
  asr_provider: string
  asr_base_url: string
  asr_api_key_masked: string
  asr_model: string
  // Embedding 组
  embedding_provider: string
  embedding_endpoint: string
  embedding_api_key_masked: string
  embedding_model: string
  embedding_dim: number
  // Vision 组（可选多模态）
  vision_provider: string
  vision_base_url: string
  vision_api_key_masked: string
  vision_model: string
  is_default: boolean
  source?: string // "user" | "hosted"
  read_only?: boolean
}

// 创建/更新请求。api_key 明文（创建必填，更新可空保留旧值）；masked 字段不回传。
export interface AIProfileRequest {
  name: string
  llm_provider: string
  llm_base_url: string
  llm_api_key?: string
  llm_model: string
  asr_provider?: string
  asr_base_url?: string
  asr_api_key?: string
  asr_model?: string
  embedding_provider?: string
  embedding_endpoint?: string
  embedding_api_key?: string
  embedding_model?: string
  embedding_dim?: number
  vision_provider?: string
  vision_base_url?: string
  vision_api_key?: string
  vision_model?: string
  is_default?: boolean
}
export type ProfilePurpose = 'llm' | 'asr' | 'embedding' | 'vision'

// ============ 媒体任务 ============
export type TaskStatus = 0 | 1 | 2 | 3 | 4 | 5
// 0=Pending 1=Queued 2=Running 3=Completed 4=Failed 5=Dead
export const TaskStatusEnum = {
  Pending: 0, Queued: 1, Running: 2, Completed: 3, Failed: 4, Dead: 5,
} as const

// 三阶段子状态 stage 取值
export type TaskStage =
  | 'none' | 'downloading' | 'uploaded'
  | 'transcribing' | 'visual_indexing' | 'summarizing' | 'indexing'

export interface VideoAsset {
  id: number
  file_md5: string
  object_name: string
  file_size: number
  content_type: string
  lifecycle_state: 'active' | 'deleting'
  created_at: string
  updated_at: string
}
export interface VideoTranscription {
  id: number
  task_id: number
  file_md5: string
  content: string // 转录全文
  words: number
  created_at: string
}
export interface AISummary {
  id: number
  task_id: number
  file_md5: string
  content: string // Markdown
  model_name: string
  created_at: string
}
export interface TaskJob {
  id: number
  task_id: number
  type: string
  status: number
  // 其余字段按需补
  [k: string]: unknown
}

// VideoTask 详情/列表项（主键是 id，不是 task_id）
export interface VideoTask {
  id: number
  user_id: number
  asset_id?: number
  file_md5: string
  filename: string
  title?: string // 视频标题（omitempty）；注意不是 video_title
  file_url: string
  file_size: number
  status: TaskStatus
  stage: TaskStage
  trace_id: string
  source_type: 'upload' | 'chunked' | 'url'
  source_url?: string
  retry_count: number
  max_retries: number
  next_retry_at?: string
  last_error_code: string
  last_error_msg: string
  last_job_type: string
  stage_started_at?: string
  stage_finished_at?: string
  started_at?: string
  finished_at?: string
  error_msg: string
  created_at: string
  updated_at: string
  asset?: VideoAsset
  transcription?: VideoTranscription
  summary?: AISummary
  jobs?: TaskJob[]
  has_transcription: boolean
  has_summary: boolean
}

export interface UploadResult {
  task_id: number
  file_md5: string
  filename: string
  file_url: string
  file_size: number
  status: number
  stage: string
  trace_id: string
}

export interface PaginatedTasks {
  list: VideoTask[]
  total: number
  page: number
  page_size: number
}

export interface RAGIndexResult {
  task_id: number
  status: string
  indexed: boolean
  chunks: number
  embedding_model: string
  last_error: string
}

// ============ Chat ============
export type ChatScopeType = 'video' | 'knowledge_base'
export type ChatMode = 'video_assistant' | 'strict_rag'

export interface ChatSession {
  id: number
  user_id: number
  task_id: number // video scope 下关联 task；kb scope 下为 0
  scope_type: ChatScopeType
  knowledge_base_id: number
  title: string
  created_at: string
  updated_at: string
}

export interface ChatMessage {
  id: number
  session_id: number
  user_id: number
  role: 'user' | 'assistant'
  content: string
  retrieval_snapshot?: string // JSON 字符串指针，非对象
  model_name?: string
  created_at: string
}

export interface Citation {
  task_id: number
  video_title?: string
  citation_id: string
  evidence_id: string
  chunk_id: number
  chunk_index: number // 显示"片段 #N"
  score: number
  content: string
  source?: string // vector | hybrid | keyword
  vector_rank?: number
  keyword_rank?: number
  rrf_score?: number
  rerank_score?: number
  final_rank?: number
}

export interface AskResult {
  message_id: number
  answer: string
  citations: Citation[]
  model: string
  degraded?: boolean
}

// SSE 事件：answer=增量 string / citations=[]Citation / done={message_id,model,answer,degraded} / error={message}
export interface SSEDone {
  message_id: number
  model: string
  answer: string
  degraded?: boolean
}
export interface SSEError {
  message: string
}

// ============ 知识库 ============
export interface KBVideo {
  task_id: number
  title: string
  status: number
  index_status: string
  retrievable: boolean
}
export interface KnowledgeBase {
  id: number
  name: string
  description: string
  member_count: number
  embedding_model?: string
  videos?: KBVideo[] // 仅 GET :id 带详情
  created_at: string
  updated_at: string
}
