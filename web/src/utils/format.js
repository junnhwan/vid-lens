/**
 * 共享格式化工具函数，消除 TaskCard / TaskDrawer / Sidebar 间的重复代码。
 */

export const formatTime = (str) => {
  if (!str) return '--'
  const d = new Date(str)
  return `${d.getMonth() + 1}/${d.getDate()} ${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`
}

export const formatFileSize = (bytes) => {
  if (!bytes) return '--'
  const units = ['B', 'KB', 'MB', 'GB']
  let size = bytes
  let unitIndex = 0
  while (size >= 1024 && unitIndex < units.length - 1) {
    size /= 1024
    unitIndex++
  }
  return `${size.toFixed(2)} ${units[unitIndex]}`
}

export const STATUS_CLASSES = ['pending', 'queued', 'running', 'completed', 'failed']
export const STATUS_TEXTS  = ['待处理', '排队中', '处理中', '已完成', '失败']

export const statusClass = (s) => STATUS_CLASSES[s] || 'pending'
export const statusText  = (s) => STATUS_TEXTS[s] || '未知'
