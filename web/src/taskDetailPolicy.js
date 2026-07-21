export function needsTaskDetail(task) {
  if (!task || task.status !== 3) return false
  return !task.transcription?.content || !task.summary?.content
}

/**
 * 列表项在「已完成」时通常不带大字段 content。
 * - transcription: 无正文时需拉详情展示（不要误当成「可再提交」）
 * - summary: 已有正文时仅用于展示/刷新；无正文时走提交总结，不强制先拉详情
 *   （与历史测试约定一致：有 content → true，无 content → false）
 */
export function needsResultDetail(task, type) {
  if (!task || task.status !== 3) return false
  if (type === 'transcription') return !task.transcription?.content
  if (type === 'summary') return !!task.summary?.content
  return needsTaskDetail(task)
}

export function taskFailureMessage(task) {
  if (!task || task.status !== 4) return ''
  return task.error_msg || ''
}
