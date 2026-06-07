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

// 基础 status 映射（保持兼容性）
export const STATUS_CLASSES = ['pending', 'queued', 'running', 'completed', 'failed', 'dead']
export const STATUS_TEXTS  = ['待处理', '排队中', '处理中', '已完成', '失败', '已终止']

export const statusClass = (s) => STATUS_CLASSES[s] || 'pending'
export const statusText  = (s) => STATUS_TEXTS[s] || '未知'

/**
 * 根据 task 的 status + stage + retry 状态，返回更精确的展示文本和样式类
 */
export const getDetailedStatus = (task) => {
  if (!task) return { text: '未知', class: 'pending', icon: '❓' }

  const { status, stage, next_retry_at } = task

  // status=5 或 'dead' 字符串
  if (status === 5 || status === 'dead') {
    return { text: '已终止', class: 'dead', icon: '⛔' }
  }

  // status=4 (failed) 且有 next_retry_at，说明等待重试
  if (status === 4 && next_retry_at) {
    return { text: '等待重试', class: 'retrying', icon: '🔄' }
  }

  // status=4 (failed) 且无 next_retry_at，说明已彻底失败
  if (status === 4) {
    return { text: '失败', class: 'failed', icon: '❌' }
  }

  // status=3 (completed)
  if (status === 3) {
    return { text: '已完成', class: 'completed', icon: '✅' }
  }

  // status < 3 且有 stage，展示具体阶段
  if (status < 3 && stage) {
    const stageMap = {
      downloading: { text: '下载中', class: 'running', icon: '⬇️' },
      uploaded: { text: '已上传', class: 'queued', icon: '📤' },
      transcribing: { text: '文字提取中', class: 'running', icon: '📝' },
      summarizing: { text: 'AI 总结中', class: 'running', icon: '🤖' },
      indexing: { text: '构建索引中', class: 'running', icon: '🔍' }
    }
    if (stageMap[stage]) {
      return stageMap[stage]
    }
  }

  // 无 stage 时回退到基础 status 映射
  const baseStatus = { text: statusText(status), class: statusClass(status) }
  const iconMap = {
    pending: '⏸️',
    queued: '⏳',
    running: '⏳',
    completed: '✅',
    failed: '❌',
    dead: '⛔'
  }
  baseStatus.icon = iconMap[baseStatus.class] || '❓'
  return baseStatus
}

/**
 * 格式化相对时间（next_retry_at）
 */
export const formatRelativeTime = (isoStr) => {
  if (!isoStr) return ''
  const target = new Date(isoStr)
  const now = new Date()
  const diffMs = target - now
  if (diffMs < 0) return '即将重试'
  const diffSec = Math.floor(diffMs / 1000)
  if (diffSec < 60) return `${diffSec}秒后`
  const diffMin = Math.floor(diffSec / 60)
  if (diffMin < 60) return `${diffMin}分钟后`
  const diffHr = Math.floor(diffMin / 60)
  return `${diffHr}小时后`
}

/**
 * 获取失败信息，优先使用 last_error_msg，然后 error_msg
 */
export const getErrorMessage = (task) => {
  if (!task) return ''
  return task.last_error_msg || task.error_msg || ''
}
