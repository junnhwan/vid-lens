export function shouldStopPolling(task, type) {
  if (!task) return false
  if (task.status === 4 || task.status === 5) return true
  if (type === 'download') {
    return task.status === 0 && task.stage === 'uploaded'
  }
  if (task.status !== 3) return false
  if (type === 'transcription') return true
  if (type === 'summary') return true
  return false
}

export function isPollingSuccessful(task, type) {
  if (!task) return false
  if (type === 'download') {
    return task.status === 0 && task.stage === 'uploaded'
  }
  return task.status === 3
}
