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

/** 后端 version 1 Agent envelope 中单步快照（与 AgentSnapshotStep 对齐） */
export interface AgentSnapshotStepJSON {
  step_id?: string
  kind?: string
  label?: string
  status?: string
  tool?: string
  input?: unknown
  output?: string
  error?: string
  ts?: string
}

interface AgentSnapshotEnvelopeJSON {
  version?: number
  run_id?: string
  mode?: string
  template?: string
  steps?: AgentSnapshotStepJSON[]
  trace?: LegacyVideoAgentStepJSON[]
  citations?: unknown[]
}

/** 旧版 Agent trace 条目（VideoAgentStep） */
interface LegacyVideoAgentStepJSON {
  name?: string
  tool?: string
  output_ref?: string
  error?: string
}

export interface ParsedSnapshotTrace {
  steps: ChatTraceStep[]
  runId?: string
  mode?: string
  /** 是否为 Agent 终态 envelope（含 steps[] 或旧 trace[]），区别于纯 RAG citations */
  isAgentEnvelope: boolean
  source: TracePanelSource
}

/** 解析 retrieval_snapshot：优先回放 steps[]，兼容旧 trace[] 与纯 citations */
export function parseSnapshotTrace(snapshot?: string): ParsedSnapshotTrace | undefined {
  if (!snapshot) return undefined
  try {
    const parsed = JSON.parse(snapshot) as unknown
    if (Array.isArray(parsed)) {
      const steps = traceFromCitationCount(parsed.length)
      return { steps, isAgentEnvelope: false, source: steps.length ? 'inferred' : 'inferred' }
    }
    if (!parsed || typeof parsed !== 'object') return undefined

    const obj = parsed as AgentSnapshotEnvelopeJSON
    const runId = obj.run_id?.trim() || undefined
    const mode = obj.mode?.trim() || undefined

    if (Array.isArray(obj.steps)) {
      const steps = obj.steps.map((step, i) => snapshotStepToTrace(step, i, runId))
      return {
        steps,
        runId,
        mode,
        isAgentEnvelope: true,
        source: 'agent',
      }
    }

    if (Array.isArray(obj.trace) && obj.trace.length > 0) {
      const steps = obj.trace.map((step, i) => legacyAgentStepToTrace(step, `hist-${i + 1}`, runId))
      return {
        steps,
        runId,
        mode,
        isAgentEnvelope: true,
        source: 'legacy',
      }
    }

    if (Array.isArray(obj.citations)) {
      const steps = traceFromCitationCount(obj.citations.length)
      const isAgentEnvelope = Boolean(
        obj.version === 1 || mode === 'agent' || mode === 'research' || obj.template,
      )
      return {
        steps,
        runId,
        mode,
        isAgentEnvelope,
        source: isAgentEnvelope ? 'agent' : 'inferred',
      }
    }

    if (obj.version === 1 || mode === 'agent' || mode === 'research' || obj.template) {
      return { steps: [], runId, mode, isAgentEnvelope: true, source: 'agent' }
    }
  } catch { /* 非 JSON 或旧格式 */ }
  return undefined
}

/** @deprecated 使用 parseSnapshotTrace；保留供仅需 steps 的调用方 */
export function traceFromSnapshot(snapshot?: string): ChatTraceStep[] | undefined {
  return parseSnapshotTrace(snapshot)?.steps
}

function snapshotStepToTrace(step: AgentSnapshotStepJSON, index: number, runId?: string): ChatTraceStep {
  const id = step.step_id?.trim() || `s${index + 1}`
  const error = step.error?.trim() || undefined
  const output = step.output?.trim() || undefined
  return {
    id,
    runId,
    kind: kindFromBackend(step.kind || '', step.tool),
    label: step.label?.trim() || step.tool?.trim() || '执行步骤',
    status: stepStatusFromSnapshot(step.status),
    tool: step.tool?.trim() || undefined,
    toolInput: formatToolInput(step.input),
    toolOutput: output,
    detail: error || output,
    error,
  }
}

function stepStatusFromSnapshot(status?: string): TraceStepStatus {
  const normalized = status?.trim()
  if (normalized === 'running' || normalized === 'done' || normalized === 'error') return normalized
  if (normalized === 'cancelled') return 'error'
  return 'done'
}

function legacyAgentStepToTrace(
  step: LegacyVideoAgentStepJSON,
  id: string,
  runId?: string,
): ChatTraceStep {
  const tool = step.tool?.trim() || ''
  const kind = kindFromBackend('', tool)
  const error = step.error?.trim() || undefined
  const output = step.output_ref?.trim() || undefined
  return {
    id,
    runId,
    kind,
    label: step.name?.trim() || tool || '执行步骤',
    status: error ? 'error' : 'done',
    tool: tool || undefined,
    toolOutput: output,
    detail: error || output,
    error,
  }
}

export function tracePanelSource(steps: ChatTraceStep[], agentMode: boolean): TracePanelSource {
  if (agentMode) return 'agent'
  if (steps.some(s => /^s\d+$/.test(s.id))) return 'agent'
  if (steps.some(s => s.id.startsWith('hist-'))) return 'legacy'
  if (steps.some(s => s.id === 'retrieve' || s.id === 'answer')) return 'inferred'
  return 'agent'
}
