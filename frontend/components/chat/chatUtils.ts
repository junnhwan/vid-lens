import type { ChatMessage } from '@/lib/types'
import { citesFromSnapshot } from '@/components/Citation'
import type { CiteRef } from '@/components/Citation'
import type { ChatTraceStep } from '@/components/chat/traceTypes'
import { traceFromCitationCount } from '@/components/chat/traceTypes'

export interface ChatMsg {
  role: 'user' | 'assistant'
  content: string
  cites?: CiteRef[]
  openCiteIds?: string[]
  streaming?: boolean
  degraded?: boolean
  error?: string
  trace?: ChatTraceStep[]
}

export function parseMessages(
  msgs: ChatMessage[],
  memberColor?: (taskId: number) => string,
): ChatMsg[] {
  return msgs.map(m => {
    const cites = m.role === 'assistant' ? citesFromSnapshot(m.retrieval_snapshot, memberColor) : undefined
    return {
      role: m.role as 'user' | 'assistant',
      content: m.content,
      openCiteIds: [],
      ...(cites ? {
        cites,
        trace: traceFromCitationCount(cites.length),
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
