import type {
  AIProfile, AIProfileRequest, ProfilePurpose, AskResult, AuthResult,
  ChatMessage, ChatMode, ChatScopeType, ChatSession, Citation, KnowledgeBase,
  PaginatedTasks, RAGIndexResult, SSEDone, SSEError, UploadResult, User, VideoTask,
} from './types'

// ============ 唯一后端出口 ============
// 所有后端调用经此模块；dev 时 Next rewrites 把 /api → :8080。

const API_BASE = '/api/v1'
const TOKEN_KEY = 'vidlens-token'

export function getToken(): string | null {
  if (typeof window === 'undefined') return null
  return localStorage.getItem(TOKEN_KEY)
}
export function setToken(t: string) { localStorage.setItem(TOKEN_KEY, t) }
export function clearToken() { localStorage.removeItem(TOKEN_KEY) }

function authHeaders(): Record<string, string> {
  const t = getToken()
  return t ? { Authorization: `Bearer ${t}` } : {}
}

// 业务层错误：401 跳登录，其它抛 message
export class ApiError extends Error {
  status: number
  constructor(status: number, message: string) {
    super(message)
    this.status = status
  }
}

// body 接受任意可序列化对象/字符串/FormData；内部统一处理
async function req<T>(path: string, method: string, body?: unknown, headersOverride?: Record<string, string>): Promise<T> {
  const headers: Record<string, string> = { ...authHeaders(), ...(headersOverride || {}) }
  let payload: BodyInit | null | undefined
  if (body instanceof FormData) {
    payload = body // 不设 Content-Type，浏览器自动加 boundary
  } else if (body !== undefined && body !== null) {
    headers['Content-Type'] = 'application/json'
    payload = typeof body === 'string' ? body : JSON.stringify(body)
  }
  const res = await fetch(`${API_BASE}${path}`, { method, headers, body: payload })
  // 401 跳登录（未授权）
  if (res.status === 401) {
    if (typeof window !== 'undefined') {
      clearToken()
      // 避免在已登录页循环：只跳到 /login
      if (!location.pathname.startsWith('/login')) location.href = '/login'
    }
    throw new ApiError(401, '未登录或登录已过期')
  }
  const env = await res.json().catch(() => ({ code: res.status, message: res.statusText }))
  // 后端成功 envelope: { code: 200, message: "success", data: ... }
  // 失败: code = HTTP 状态码（400/500 等）。所以判 code === 200 为成功。
  if (!res.ok || env.code !== 200) {
    throw new ApiError(res.status, env.message || `请求失败 (${res.status})`)
  }
  return env.data as T
}

// ============ 认证 ============
export const api = {
  register: (username: string, password: string, nickname?: string) =>
    req<AuthResult>('/user/register', 'POST', { username, password, nickname }),
  login: (username: string, password: string) =>
    req<AuthResult>('/user/login', 'POST', { username, password }),
  profile: () => req<User>('/user/profile', 'GET'),

  // ============ AI Profile ============
  listProfiles: () => req<AIProfile[]>('/ai/profiles', 'GET'),
  createProfile: (p: AIProfileRequest) => req<AIProfile>('/ai/profiles', 'POST', p),
  updateProfile: (id: number, p: Partial<AIProfileRequest>) => req<AIProfile>(`/ai/profiles/${id}`, 'PUT', p),
  deleteProfile: (id: number) => req<null>(`/ai/profiles/${id}`, 'DELETE'),
  testProfile: (payload: { id?: number } | AIProfileRequest) =>
    req<{ ok: boolean }>('/ai/profiles/test', 'POST', payload),
  listModels: (base_url: string, api_key: string, profile_id: number, purpose: ProfilePurpose = 'llm') =>
    req<{ models: string[] }>('/ai/profiles/models', 'POST', { base_url, api_key, profile_id, purpose }),
  probeEmbeddingDim: (endpoint: string, api_key: string, model: string, profile_id: number) =>
    req<{ dimension: number }>('/ai/profiles/embedding-dim', 'POST', { endpoint, api_key, model, profile_id }),

  // ============ 媒体 ============
  uploadFile: (file: File) => {
    const fd = new FormData()
    fd.append('file', file)
    return req<UploadResult>('/media/upload', 'POST', fd)
  },
  uploadUrl: (url: string) => req<UploadResult>('/media/upload-url', 'POST', { url }),
  listTasks: (page = 1, page_size = 20, keyword = '') =>
    req<PaginatedTasks>(`/media/list?page=${page}&page_size=${page_size}&keyword=${encodeURIComponent(keyword)}`, 'GET'),
  getTask: (id: number) => req<VideoTask>(`/media/task/${id}`, 'GET'),
  deleteTask: (id: number) => req<null>(`/media/task/${id}`, 'DELETE'),
  transcribe: (id: number, force = false) =>
    req<{ task_id: number }>(`/media/transcribe/${id}${force ? '?force=1' : ''}`, 'POST'),
  analyze: (id: number, force = false) =>
    req<{ task_id: number }>(`/media/analyze/${id}${force ? '?force=1' : ''}`, 'POST'),
  getRagIndex: (id: number) => req<RAGIndexResult>(`/media/task/${id}/rag-index`, 'GET'),
  triggerRagIndex: (id: number) => req<RAGIndexResult>(`/media/task/${id}/rag-index`, 'POST'),

  // ============ Chat ============
  createSession: (params: { task_id?: number; scope_type?: ChatScopeType; knowledge_base_id?: number; title?: string; mode?: ChatMode }) =>
    req<ChatSession>('/chat/sessions', 'POST', params),
  listSessions: (params: { task_id?: number; knowledge_base_id?: number; scope_type?: ChatScopeType } = {}) => {
    const q = new URLSearchParams()
    if (params.task_id) q.set('task_id', String(params.task_id))
    if (params.knowledge_base_id) q.set('knowledge_base_id', String(params.knowledge_base_id))
    if (params.scope_type) q.set('scope_type', params.scope_type)
    const qs = q.toString()
    return req<ChatSession[]>(`/chat/sessions${qs ? `?${qs}` : ''}`, 'GET')
  },
  getMessages: (sid: number) => req<ChatMessage[]>(`/chat/sessions/${sid}/messages`, 'GET'),
  ask: (sid: number, question: string, top_k: number, mode?: ChatMode) =>
    req<AskResult>(`/chat/sessions/${sid}/messages`, 'POST', { question, top_k, mode }),
  deleteSession: (sid: number) => req<{ deleted: boolean }>(`/chat/sessions/${sid}`, 'DELETE'),

  // ============ 知识库 ============
  listKBs: () => req<KnowledgeBase[]>('/knowledge-bases', 'GET'),
  getKB: (id: number) => req<KnowledgeBase>(`/knowledge-bases/${id}`, 'GET'),
  createKB: (name: string, description: string) =>
    req<KnowledgeBase>('/knowledge-bases', 'POST', { name, description }),
  updateKB: (id: number, name?: string, description?: string) =>
    req<KnowledgeBase>(`/knowledge-bases/${id}`, 'PATCH', { name, description }),
  deleteKB: (id: number) => req<null>(`/knowledge-bases/${id}`, 'DELETE'),
  addKBVideo: (id: number, task_id: number) =>
    req<null>(`/knowledge-bases/${id}/videos`, 'POST', { task_id }),
  removeKBVideo: (id: number, task_id: number) =>
    req<null>(`/knowledge-bases/${id}/videos/${task_id}`, 'DELETE'),
}

// ============ SSE 流式问答 ============
// EventSource 不能带 Authorization header，手动 fetch + ReadableStream 解析 text/event-stream。
// 事件：answer(增量 string) / citations([]Citation) / done(SSEDone) / error(SSEError)
export interface SSEHandlers {
  onAnswer: (delta: string) => void
  onCitations: (cs: Citation[]) => void
  onDone: (d: SSEDone) => void
  onError: (e: SSEError) => void
}

export async function streamAsk(
  sid: number,
  question: string,
  top_k: number,
  mode: ChatMode,
  h: SSEHandlers,
  signal?: AbortSignal,
): Promise<void> {
  const res = await fetch(`${API_BASE}/chat/sessions/${sid}/messages/stream`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', ...authHeaders() },
    body: JSON.stringify({ question, top_k, mode }),
    signal,
  })
  if (res.status === 401) {
    h.onError({ message: '未登录或登录已过期' })
    if (typeof window !== 'undefined') { clearToken(); location.href = '/login' }
    return
  }
  if (!res.ok || !res.body) {
    h.onError({ message: `流式请求失败 (${res.status})` })
    return
  }
  const reader = res.body.getReader()
  const decoder = new TextDecoder()
  let buf = ''
  // SSE 以 \n\n 分隔事件；data 行可能跨多行需拼接
  for (;;) {
    const { done, value } = await reader.read()
    if (done) break
    buf += decoder.decode(value, { stream: true })
    let sep: number
    while ((sep = buf.indexOf('\n\n')) >= 0) {
      const raw = buf.slice(0, sep)
      buf = buf.slice(sep + 2)
      const lines = raw.split('\n')
      let event = 'message'
      const dataParts: string[] = []
      for (const line of lines) {
        if (line.startsWith('event:')) event = line.slice(6).trim()
        else if (line.startsWith('data:')) dataParts.push(line.slice(5).trim())
      }
      const dataStr = dataParts.join('\n')
      if (!dataStr) continue
      let parsed: unknown
      try { parsed = JSON.parse(dataStr) } catch { parsed = dataStr }
      switch (event) {
        case 'answer': h.onAnswer(typeof parsed === 'string' ? parsed : '')
          break
        case 'citations': h.onCitations(parsed as Citation[])
          break
        case 'done': h.onDone(parsed as SSEDone)
          break
        case 'error': h.onError(parsed as SSEError)
          break
      }
    }
  }
}
