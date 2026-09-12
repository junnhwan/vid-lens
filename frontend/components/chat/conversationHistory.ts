import type { ChatMsg } from './chatUtils.ts'
import { progressTrace } from './traceTypes.ts'
import type { ProgressEvent } from '../../lib/conversationStream.ts'

export interface RunHistory {
  run_id: string
  question: string
  status: string
  stop_reason?: string
  error?: string
  created_at: string
  steps: ProgressEvent[]
}

export function mergeRunHistory(messages: ChatMsg[], runs: RunHistory[]): ChatMsg[] {
  const saved = new Set(messages.flatMap(m => m.agentRunId ? [m.agentRunId] : []))
  const result = [...messages]
  for (const run of runs) {
    if (saved.has(run.run_id)) continue
    saved.add(run.run_id)
    const at = Date.parse(run.created_at)
    result.push({ role: 'user', content: run.question, createdAt: at }, {
      role: 'assistant', content: '', createdAt: at + 1, agentRun: true, agentRunId: run.run_id, agentMode: 'agent',
      runStatus: run.status,
      trace: run.steps.reduce((steps, step) => progressTrace(steps, step), [] as NonNullable<ChatMsg['trace']>),
      traceSource: 'agent', cancelled: run.status === 'cancelled',
      error: run.status === 'cancelled' ? undefined : run.error || (['pending', 'running'].includes(run.status) ? '服务端运行仍在进行，刷新可重新核对；未自动重新运行。' : run.status === 'completed' ? '运行已结束，已保存回答尚待确认。' : run.status === 'budget_exhausted' ? `运行预算已用尽，本轮未保存回答。${run.stop_reason || ''}` : `本轮执行失败，未保存回答。${run.stop_reason || ''}`),
    })
  }
  return result.sort((a, b) => (a.createdAt ?? 0) - (b.createdAt ?? 0))
}
