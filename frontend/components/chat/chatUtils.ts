import type { ChatMessage } from '@/lib/types'
import { BLOCKED_ANSWER_PREFIX } from '@/lib/types'
import { citesFromSnapshot } from '@/components/Citation'
import type { CiteRef } from '@/components/Citation'
import type { ChatTraceStep } from '@/components/chat/traceTypes'
import { traceFromCitationCount } from '@/components/chat/traceTypes'
import { parseSnapshotTrace } from '@/components/chat/snapshotTraceAdapter'

export interface ChatMsg {
  messageId?: number
  runStatus?: string
  role: 'user' | 'assistant'
  content: string
  cites?: CiteRef[]
  openCiteIds?: string[]
  streaming?: boolean
  degraded?: boolean
  error?: string
  trace?: ChatTraceStep[]
  agentRun?: boolean
  /** Agent 运行 ID（SSE done 事件、非流式结果 run_id 或历史快照），用于关联历史执行 */
  agentRunId?: string
  /** agent | research | evidence_funnel：决定消息署名、模式 chip 与右栏轨迹形态 */
  agentMode?: string
  traceSource?: 'agent' | 'inferred' | 'legacy' | 'server'
  reasoning?: Record<string, string>
  processStartedAt?: number
  processFinishedAt?: number
  cancelled?: boolean
  createdAt?: number
}

/** EvidenceInspector 阻断发布时的替换文案（与后端 inspectorBlockedAnswer 对齐），
    仅兼容旧阻断回答 */
export function isBlockedAnswer(answer: string): boolean {
  return answer.startsWith(BLOCKED_ANSWER_PREFIX)
}

export function parseMessages(
  msgs: ChatMessage[],
  memberColor?: (taskId: number) => string,
): ChatMsg[] {
  return msgs.map(m => {
    const cites = m.role === 'assistant' ? citesFromSnapshot(m.retrieval_snapshot, memberColor) : undefined
    const snapshotTrace = m.role === 'assistant' ? parseSnapshotTrace(m.retrieval_snapshot) : undefined
    const trace = m.role === 'assistant'
      ? (snapshotTrace?.steps ?? (cites?.length ? traceFromCitationCount(cites.length) : undefined))
      : undefined
    const agentRun = Boolean(snapshotTrace?.isAgentEnvelope)
    return {
      messageId: m.id,
      role: m.role as 'user' | 'assistant',
      content: m.content,
      createdAt: Date.parse(m.created_at),
      ...(snapshotTrace?.degraded ? { degraded: true } : {}),
      openCiteIds: [],
      ...(cites ? { cites } : {}),
      ...(snapshotTrace?.runId ? { agentRunId: snapshotTrace.runId } : {}),
      ...(agentRun && snapshotTrace?.mode ? { agentMode: snapshotTrace.mode } : {}),
      ...(trace ? {
        trace,
        ...(agentRun ? { agentRun: true, traceSource: snapshotTrace?.source } : {}),
      } : {}),
    }
  })
}

export function fmtSession(iso: string) {
  const d = new Date(iso)
  return `${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`
}

export function fmtShortDate(iso: string) {
  const d = new Date(iso)
  return `${pad(d.getMonth() + 1)}-${pad(d.getDate())}`
}

function pad(n: number) { return n < 10 ? `0${n}` : `${n}` }
