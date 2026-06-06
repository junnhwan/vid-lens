export function shouldStopPolling(task, type) {
  if (!task) return false
  if (task.status === 4) return true
  if (task.status !== 3) return false
  if (type === 'transcription') return true
  if (type === 'summary') return true
  return false
}
