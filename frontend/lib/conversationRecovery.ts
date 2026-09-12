/** Recovery reads authoritative state only; it never starts another execution. */
export async function recoverConversationRun<T>(runId: string, reads: {
  detail: () => Promise<{ status: string; stop_reason?: string }>
  messages: () => Promise<T[]>
  runId: (message: T) => string | undefined
}, signal?: AbortSignal): Promise<{ message?: T; status: string; notice?: string }> {
  let timer: ReturnType<typeof setTimeout> | undefined
  const deadline = new Promise<never>((_, reject) => { timer = setTimeout(() => reject(new Error('timeout')), 6000) })
  try {
    return await Promise.race([deadline, (async () => {
      const detail = await reads.detail()
      if (signal?.aborted) return { status: 'unconfirmed' }
      const messages = await reads.messages()
      if (signal?.aborted) return { status: 'unconfirmed' }
      const message = messages.find(message => reads.runId(message) === runId)
      if (message) return { message, status: detail.status }
      if (detail.status === 'pending' || detail.status === 'running') return { status: detail.status, notice: '连接已中断，服务端运行仍在进行。刷新可重新核对；未自动重新运行。' }
      if (detail.status === 'cancelled') return { status: detail.status, notice: '本轮运行已取消，未保存回答。' }
      if (detail.status === 'failed' || detail.status === 'budget_exhausted') return { status: detail.status, notice: `本轮未保存回答：${detail.stop_reason || detail.status}` }
      return { status: 'unconfirmed', notice: '运行已结束，但尚未读取到已保存回答。刷新可重新核对。' }
    })()])
  } catch {
    return { status: 'unconfirmed', notice: '暂时无法确认运行状态。已保留运行标识，刷新后可重新核对；未自动重新运行。' }
  } finally { clearTimeout(timer) }
}
