export const TASK_STATUS = {
  PENDING: 0,
  QUEUED: 1,
  RUNNING: 2,
  COMPLETED: 3,
  FAILED: 4,
  DEAD: 5,
}

/** 任务忙（排队/运行/本地 loading）时禁止再点主操作 */
export function isTaskActionDisabled(task, isLoading = false) {
  return isLoading || task?.status === TASK_STATUS.QUEUED || task?.status === TASK_STATUS.RUNNING
}

function truthyFlag(value) {
  return value === true || value === 1 || value === '1' || value === 'true'
}

/**
 * 列表 API 不带正文 content，靠 has_transcription / has_summary；
 * 详情接口则有 transcription.content / summary.content。
 * 兼容 snake_case / camelCase，以及 1/"true" 等序列化形态。
 */
export function hasTranscriptionResult(task) {
  if (!task) return false
  if (truthyFlag(task.has_transcription) || truthyFlag(task.hasTranscription)) return true
  const content = task.transcription?.content
  return typeof content === 'string' && content.length > 0
}

export function hasSummaryResult(task) {
  if (!task) return false
  if (truthyFlag(task.has_summary) || truthyFlag(task.hasSummary)) return true
  const content = task.summary?.content
  return typeof content === 'string' && content.length > 0
}

/** 主按钮「提取文字」：忙或已有结果时禁用（看结果 / 用次要重新提取） */
export function isPrimaryTranscribeDisabled(task, isLoading = false) {
  return isTaskActionDisabled(task, isLoading) || hasTranscriptionResult(task)
}

/** 主按钮「AI 总结」：忙或已有结果时禁用 */
export function isPrimaryAnalyzeDisabled(task, isLoading = false) {
  return isTaskActionDisabled(task, isLoading) || hasSummaryResult(task)
}

export function primaryTranscribeLabel(task) {
  return hasTranscriptionResult(task) ? '已提取' : '提取文字'
}

export function primaryAnalyzeLabel(task) {
  return hasSummaryResult(task) ? '已总结' : 'AI 总结'
}
