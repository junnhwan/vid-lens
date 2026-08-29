/** 聊天页右侧「执行流水线」步骤（前端展示模型；Agent 流式就绪后可与 SSE 对齐） */

export type TraceStepStatus = 'pending' | 'running' | 'done' | 'error'

export type TraceStepKind = 'think' | 'retrieve' | 'tool' | 'answer'

export interface ChatTraceStep {
  id: string
  kind: TraceStepKind
  label: string
  status: TraceStepStatus
  detail?: string
}

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
  })
}

function upsertStep(steps: ChatTraceStep[], step: ChatTraceStep): ChatTraceStep[] {
  const i = steps.findIndex(s => s.id === step.id)
  if (i < 0) return [...steps, step]
  const next = [...steps]
  next[i] = { ...next[i], ...step }
  return next
}

/** 历史消息：从引用快照反推检索步骤（无 Agent trace 时的降级展示） */
export function traceFromCitationCount(count: number): ChatTraceStep[] {
  if (count <= 0) return []
  return [
    { id: 'retrieve', kind: 'retrieve', label: '检索转写片段', status: 'done', detail: `命中 ${count} 条引用` },
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
