export function needsTaskDetail(task) {
  if (!task || task.status !== 3) return false
  return !task.transcription?.content || !task.summary?.content
}

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
