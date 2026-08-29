/** 聊天页右侧「执行流水线」步骤（与后端 Agent SSE step_id 对齐） */

export type TraceStepStatus = 'pending' | 'running' | 'done' | 'error'

export type TraceStepKind = 'think' | 'retrieve' | 'tool' | 'answer' | 'plan' | 'observe'

export interface ChatTraceStep {
  /** 后端 step_id，如 s1、s2；RAG 推断步骤使用固定 id */
  id: string
  runId?: string
  kind: TraceStepKind
  label: string
  status: TraceStepStatus
  detail?: string
  query?: string
  hits?: number
  sources?: string[]
  tool?: string
  toolInput?: string
  toolOutput?: string
  durationMs?: number
  error?: string
}

export type TracePanelSource = 'agent' | 'inferred' | 'legacy'

export interface AgentTraceState {
  runId: string | null
  mode: string | null
  steps: ChatTraceStep[]
  finished: boolean
  fatalError?: string
}

export const emptyAgentTraceState = (): AgentTraceState => ({
  runId: null,
  mode: null,
  steps: [],
  finished: false,
})

// --- RAG 推断轨迹（普通 streamAsk，保持不变）---

export function initialRetrieveTrace(): ChatTraceStep[] {
  return [{ id: 'retrieve', kind: 'retrieve', label: '检索转写片段', status: 'running' }]
}

export function markRetrieveDone(steps: ChatTraceStep[], hits: number, sources?: string[]): ChatTraceStep[] {
  const detail = sources?.length
    ? `命中 ${hits} 条 · ${sources.slice(0, 3).join('、')}${sources.length > 3 ? '…' : ''}`
    : `命中 ${hits} 条引用`
  return upsertStep(steps, {
    id: 'retrieve',
    kind: 'retrieve',
    label: '检索转写片段',
    status: 'done',
    detail,
    hits,
    sources,
  })
}

export function markAnswerRunning(steps: ChatTraceStep[]): ChatTraceStep[] {
  return upsertStep(steps, {
    id: 'answer',
    kind: 'answer',
    label: '生成回答',
    status: 'running',
  })
}

export function markAnswerDone(steps: ChatTraceStep[]): ChatTraceStep[] {
  return upsertStep(steps, {
    id: 'answer',
    kind: 'answer',
    label: '生成回答',
    status: 'done',
    detail: '回答已输出至对话区',
  })
}

export function markRetrieveError(steps: ChatTraceStep[], message: string): ChatTraceStep[] {
  return upsertStep(steps, {
    id: 'retrieve',
    kind: 'retrieve',
    label: '检索转写片段',
    status: 'error',
    detail: message,
    error: message,
  })
}

function upsertStep(steps: ChatTraceStep[], step: ChatTraceStep): ChatTraceStep[] {
  const i = steps.findIndex(s => s.id === step.id)
  if (i < 0) return [...steps, step]
  const next = [...steps]
  next[i] = { ...next[i], ...step }
  return next
}

/** 历史消息：无 Agent trace 时从引用条数推断 */
export function traceFromCitationCount(count: number): ChatTraceStep[] {
  if (count <= 0) return []
  return [
    { id: 'retrieve', kind: 'retrieve', label: '检索转写片段', status: 'done', detail: `命中 ${count} 条引用`, hits: count },
    { id: 'answer', kind: 'answer', label: '生成回答', status: 'done' },
  ]
}

export function streamTraceReducer(
  steps: ChatTraceStep[],
  event: 'start' | 'citations' | 'answer' | 'done' | 'error',
  payload?: { hits?: number; sources?: string[]; error?: string },
): ChatTraceStep[] {
  switch (event) {
    case 'start':
      return initialRetrieveTrace()
    case 'citations':
      return markRetrieveDone(steps, payload?.hits ?? 0, payload?.sources)
    case 'answer':
      return markAnswerRunning(steps)
    case 'done': {
      let next = steps
      if (!next.some(s => s.kind === 'retrieve' && s.status === 'done')) {
        next = markRetrieveDone(next.length ? next : initialRetrieveTrace(), payload?.hits ?? 0, payload?.sources)
      }
      if (!next.some(s => s.id === 'answer')) next = markAnswerRunning(next)
      return markAnswerDone(next)
    }
    case 'error':
      return markRetrieveError(steps.length ? steps : initialRetrieveTrace(), payload?.error ?? '请求失败')
    default:
      return steps
  }
}

// --- Agent SSE reducer（按 run_id + step_id 幂等更新）---

export type AgentSSEPayload =
  | { type: 'run_start'; data: AgentRunStartPayload }
  | { type: 'step_start'; data: AgentStepPayload }
  | { type: 'step_done'; data: AgentStepPayload }
  | { type: 'step_error'; data: AgentStepPayload }
  | { type: 'tool_call'; data: AgentToolCallPayload }
  | { type: 'tool_result'; data: AgentToolResultPayload }
  | { type: 'retrieve_hits'; data: AgentRetrieveHitsPayload }
  | { type: 'done' }
  | { type: 'error'; data: { message: string; step_id?: string } }

export interface AgentRunStartPayload {
  run_id: string
  mode: string
  scope_type: string
  task_id?: number
  kb_id?: number
}

export interface AgentStepPayload {
  run_id: string
  step_id: string
  kind: string
  label: string
  status: string
  detail?: string
  query?: string
  hits?: number
  tool?: string
  input?: unknown
  output?: string
  error?: string
  ts?: string
}

export interface AgentToolCallPayload {
  run_id: string
  step_id: string
  tool: string
  input?: unknown
}

export interface AgentToolResultPayload {
  run_id: string
  step_id: string
  output?: string
  duration_ms?: number
  error?: string
}

export interface AgentRetrieveHitsPayload {
  run_id: string
  step_id: string
  query?: string
  hits: number
  sources?: string[]
}

function kindFromBackend(kind: string, tool?: string): TraceStepKind {
  if (kind === 'retrieve' || tool === 'search_transcript') return 'retrieve'
  if (kind === 'answer' || tool === 'build_cited_answer') return 'answer'
  if (kind === 'tool') return 'tool'
  if (kind === 'think') return 'think'
  if (kind === 'plan') return 'plan'
  if (kind === 'observe') return 'observe'
  return 'tool'
}

function formatToolInput(input: unknown): string | undefined {
  if (input == null) return undefined
  if (typeof input === 'string') return input
  try {
    const s = JSON.stringify(input)
    return s.length > 160 ? `${s.slice(0, 160)}…` : s
  } catch {
    return String(input)
  }
}

function mergeStep(steps: ChatTraceStep[], stepId: string, patch: Partial<ChatTraceStep>): ChatTraceStep[] {
  const i = steps.findIndex(s => s.id === stepId)
  if (i < 0) {
    return [...steps, {
      id: stepId,
      kind: patch.kind ?? 'tool',
      label: patch.label ?? '执行步骤',
      status: patch.status ?? 'running',
      ...patch,
    }]
  }
  const next = [...steps]
  next[i] = { ...next[i], ...patch }
  return next
}

function stepFromPayload(data: AgentStepPayload): Partial<ChatTraceStep> {
  return {
    runId: data.run_id,
    kind: kindFromBackend(data.kind, data.tool),
    label: data.label,
    status: data.status as TraceStepStatus,
    detail: data.detail || data.output || undefined,
    query: data.query,
    hits: data.hits || undefined,
    tool: data.tool,
    toolInput: formatToolInput(data.input),
    toolOutput: data.output,
    error: data.error,
  }
}

export function agentTraceReducer(state: AgentTraceState, event: AgentSSEPayload): AgentTraceState {
  switch (event.type) {
    case 'run_start': {
      return {
        runId: event.data.run_id,
        mode: event.data.mode,
        steps: [],
        finished: false,
      }
    }
    case 'step_start': {
      const p = stepFromPayload({ ...event.data, status: 'running' })
      return {
        ...state,
        runId: event.data.run_id,
        steps: mergeStep(state.steps, event.data.step_id, { ...p, status: 'running' }),
      }
    }
    case 'step_done': {
      const p = stepFromPayload({ ...event.data, status: 'done' })
      return {
        ...state,
        steps: mergeStep(state.steps, event.data.step_id, { ...p, status: 'done' }),
      }
    }
    case 'step_error': {
      const p = stepFromPayload({ ...event.data, status: 'error' })
      return {
        ...state,
        steps: mergeStep(state.steps, event.data.step_id, {
          ...p,
          status: 'error',
          error: event.data.error || p.error,
          detail: event.data.error || p.detail,
        }),
      }
    }
    case 'tool_call':
      return {
        ...state,
        steps: mergeStep(state.steps, event.data.step_id, {
          runId: event.data.run_id,
          tool: event.data.tool,
          toolInput: formatToolInput(event.data.input),
          kind: kindFromBackend('', event.data.tool),
        }),
      }
    case 'tool_result':
      return {
        ...state,
        steps: mergeStep(state.steps, event.data.step_id, {
          runId: event.data.run_id,
          toolOutput: event.data.output,
          durationMs: event.data.duration_ms,
          error: event.data.error,
          status: event.data.error ? 'error' : undefined,
        }),
      }
    case 'retrieve_hits':
      return {
        ...state,
        steps: mergeStep(state.steps, event.data.step_id, {
          runId: event.data.run_id,
          kind: 'retrieve',
          query: event.data.query,
          hits: event.data.hits,
          sources: event.data.sources,
          detail: event.data.hits > 0
            ? `命中 ${event.data.hits} 条${event.data.sources?.length ? ` · ${event.data.sources.slice(0, 3).join('、')}` : ''}`
            : '未命中引用',
        }),
      }
    case 'done':
      return { ...state, finished: true }
    case 'error':
      return { ...state, finished: true, fatalError: event.data.message }
    default:
      return state
  }
}

/** 解析 retrieval_snapshot：Agent envelope 或纯 citations 数组 */
export function traceFromSnapshot(snapshot?: string): ChatTraceStep[] | undefined {
  if (!snapshot) return undefined
  try {
    const parsed = JSON.parse(snapshot) as unknown
    if (Array.isArray(parsed)) {
      return traceFromCitationCount(parsed.length)
    }
    if (parsed && typeof parsed === 'object') {
      const obj = parsed as {
        trace?: Array<{ name?: string; tool?: string; output_ref?: string; error?: string }>
        citations?: unknown[]
      }
      if (Array.isArray(obj.trace) && obj.trace.length > 0) {
        return obj.trace.map((step, i) => legacyAgentStepToTrace(step, `hist-${i + 1}`))
      }
      if (Array.isArray(obj.citations)) {
        return traceFromCitationCount(obj.citations.length)
      }
    }
  } catch { /* 非 JSON 或旧格式 */ }
  return undefined
}

function legacyAgentStepToTrace(
  step: { name?: string; tool?: string; output_ref?: string; error?: string },
  id: string,
): ChatTraceStep {
  const tool = step.tool || ''
  const kind = kindFromBackend('', tool)
  const hasError = Boolean(step.error)
  return {
    id,
    kind,
    label: step.name || tool || '执行步骤',
    status: hasError ? 'error' : 'done',
    tool: tool || undefined,
    toolOutput: step.output_ref,
    detail: hasError ? step.error : step.output_ref,
    error: step.error,
  }
}

export function tracePanelSource(steps: ChatTraceStep[], agentMode: boolean): TracePanelSource {
  if (agentMode) return 'agent'
  if (steps.some(s => s.id.startsWith('s') || s.id.startsWith('hist-'))) return 'legacy'
  if (steps.some(s => s.id === 'retrieve' || s.id === 'answer')) return 'inferred'
  return 'agent'
}
