import { SSEStreamDecoder } from './streamDecoder.ts'

export interface ProgressEvent {
  id: string
  run_id?: string
  plan_id?: string
  kind: string
  label: string
  status: 'running' | 'done' | 'error' | 'cancelled'
  detail?: string
  tool?: string
  evidence_refs?: string[]
  replan?: boolean
  duration_ms?: number
  ts?: string
}
export interface ReasoningEvent { call_id: string; run_id?: string; delta: string }
export interface ProcessHandlers {
  onProgress?: (event: ProgressEvent) => void
  onReasoning?: (event: ReasoningEvent) => void
  onAnswerReset?: () => void
}

/** EOF is a transport event, not confirmation that a message was saved. */
export async function readConversationStream(
  body: ReadableStream<Uint8Array>,
  dispatch: (event: string, data: unknown) => void,
  signal?: AbortSignal,
): Promise<void> {
  const reader = body.getReader()
  const decoder = new SSEStreamDecoder()
  let terminal = false
  const emit = (event: string, data: unknown) => {
    if (terminal || signal?.aborted) return
    if (event === 'done' || event === 'error') terminal = true
    dispatch(event, data)
  }
  const cancel = () => { void reader.cancel().catch(() => {}) }
  signal?.addEventListener('abort', cancel, { once: true })
  try {
    while (!signal?.aborted && !terminal) {
      const { done, value } = await reader.read()
      if (done) {
        for (const item of decoder.finish()) emit(item.event, item.data)
        if (!terminal && !signal?.aborted) emit('error', { code: 'stream_interrupted', message: '连接已中断，正在核对运行状态。' })
        break
      }
      for (const item of decoder.push(value)) emit(item.event, item.data)
    }
  } finally {
    signal?.removeEventListener('abort', cancel)
    await reader.cancel().catch(() => {})
    reader.releaseLock()
  }
}
