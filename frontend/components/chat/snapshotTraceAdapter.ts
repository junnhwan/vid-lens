import {
  traceFromCitationCount,
  type ChatTraceStep,
  type TracePanelSource,
  type TraceStepKind,
  type TraceStepStatus,
} from './traceTypes.ts'

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
  degraded?: boolean
  version?: number
  run_id?: string
  mode?: string
  template?: string
  steps?: AgentSnapshotStepJSON[]
  trace?: LegacyVideoAgentStepJSON[]
  citations?: unknown[]
}

interface LegacyVideoAgentStepJSON {
  name?: string
  tool?: string
  output_ref?: string
  error?: string
}

export interface ParsedSnapshotTrace {
  degraded?: boolean
  steps: ChatTraceStep[]
  runId?: string
  mode?: string
  isAgentEnvelope: boolean
  source: TracePanelSource
}

/** Compatibility adapter for persisted retrieval_snapshot formats only. */
export function parseSnapshotTrace(snapshot?: string): ParsedSnapshotTrace | undefined {  if (!snapshot) return undefined
  try {
    const parsed = JSON.parse(snapshot) as unknown
    if (Array.isArray(parsed)) {
      return { steps: traceFromCitationCount(parsed.length), isAgentEnvelope: false, source: 'inferred' }
    }
    if (!parsed || typeof parsed !== 'object') return undefined

    const obj = parsed as AgentSnapshotEnvelopeJSON
    const runId = obj.run_id?.trim() || undefined
    const mode = obj.mode?.trim() || undefined
    if (Array.isArray(obj.steps)) {
      return { ...(obj.degraded ? { degraded: true } : {}), steps: obj.steps.map((step, index) => snapshotStepToTrace(step, index, runId)), runId, mode, isAgentEnvelope: true, source: 'agent' }
    }
    if (Array.isArray(obj.trace) && obj.trace.length > 0) {
      return { steps: obj.trace.map((step, index) => legacyAgentStepToTrace(step, `hist-${index + 1}`, runId)), runId, mode, isAgentEnvelope: true, source: 'legacy' }
    }
    if (Array.isArray(obj.citations)) {
      const isAgentEnvelope = Boolean(obj.version === 1 || mode === 'agent' || mode === 'research' || obj.template)
      return {
        steps: traceFromCitationCount(obj.citations.length), runId, mode, isAgentEnvelope,
        source: isAgentEnvelope ? 'agent' : 'inferred',
      }
    }
    if (obj.version === 1 || mode === 'agent' || mode === 'research' || obj.template) {
      return { steps: [], runId, mode, isAgentEnvelope: true, source: 'agent' }
    }
  } catch { /* non-JSON legacy values have no trace */ }
  return undefined
}

function snapshotStepToTrace(step: AgentSnapshotStepJSON, index: number, runId?: string): ChatTraceStep {
  const error = step.error?.trim() || undefined
  const output = step.output?.trim() || undefined
  return {
    id: step.step_id?.trim() || `s${index + 1}`,
    runId,
    kind: snapshotKind(step.kind || '', step.tool),
    label: step.label?.trim() || step.tool?.trim() || '执行步骤',
    status: snapshotStatus(step.status),
    tool: step.tool?.trim() || undefined,
    toolInput: formatSnapshotInput(step.input),
    toolOutput: output,
    detail: error || output,
    error,
  }
}

function legacyAgentStepToTrace(step: LegacyVideoAgentStepJSON, id: string, runId?: string): ChatTraceStep {
  const tool = step.tool?.trim() || ''
  const error = step.error?.trim() || undefined
  const output = step.output_ref?.trim() || undefined
  return {
    id, runId, kind: snapshotKind('', tool), label: step.name?.trim() || tool || '执行步骤',
    status: error ? 'error' : 'done', tool: tool || undefined, toolOutput: output,
    detail: error || output, error,
  }
}

function snapshotKind(kind: string, tool?: string): TraceStepKind {
  if (kind === 'retrieve' || tool === 'search_transcript') return 'retrieve'
  if (kind === 'answer' || tool === 'build_cited_answer') return 'answer'
  if (kind === 'think' || kind === 'plan' || kind === 'observe' || kind === 'tool' || kind === 'save') return kind
  return 'tool'
}

function snapshotStatus(status?: string): TraceStepStatus {
  const normalized = status?.trim()
  if (normalized === 'running' || normalized === 'done' || normalized === 'error') return normalized
  if (normalized === 'cancelled') return 'cancelled'
  return 'done'
}

function formatSnapshotInput(input: unknown): string | undefined {
  if (input == null) return undefined
  if (typeof input === 'string') return input
  try {
    const encoded = JSON.stringify(input)
    return encoded.length > 160 ? `${encoded.slice(0, 160)}…` : encoded
  } catch {
    return String(input)
  }
}

/** 非流式 Agent 接口（/messages/agent，mode=research|evidence_funnel）返回的
    VideoAgentResult.trace（name/tool/input/output_ref/error）→ 可回放的步骤轨迹。
    步骤形状与历史快照的 legacy trace 一致，复用同一套字段判定；无时长信息，不伪造。 */
export function traceFromAgentResult(result: {
  run_id?: string
  mode?: string
  template?: string
  trace?: AgentResultStepJSON[]
}): ParsedSnapshotTrace {
  const runId = result.run_id?.trim() || undefined
  const mode = result.mode?.trim() || result.template?.trim() || undefined
  const steps = Array.isArray(result.trace)
    ? result.trace.map((step, index) => agentResultStepToTrace(step, `s${index + 1}`, runId))
    : []
  return { steps, runId, mode, isAgentEnvelope: true, source: 'agent' }
}

interface AgentResultStepJSON {
  name?: string
  tool?: string
  input?: unknown
  output_ref?: string
  error?: string
}

function agentResultStepToTrace(step: AgentResultStepJSON, id: string, runId?: string): ChatTraceStep {
  const tool = step.tool?.trim() || ''
  const error = step.error?.trim() || undefined
  const output = step.output_ref?.trim() || undefined
  return {
    id,
    runId,
    kind: snapshotKind('', tool),
    label: step.name?.trim() || tool || '执行步骤',
    status: error ? 'error' : 'done',
    tool: tool || undefined,
    toolInput: formatSnapshotInput(step.input),
    toolOutput: output,
    detail: error || output,
    error,
  }
}

