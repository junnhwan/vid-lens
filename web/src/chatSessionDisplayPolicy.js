/**
 * Session chip label helpers — pure, unit-tested.
 */

export function formatSessionLabel(session, { index = 0 } = {}) {
  const title = (session?.title || '').trim()
  if (title) return title
  if (session?.id != null) return `会话 #${session.id}`
  return `对话 ${index + 1}`
}

/**
 * Compact relative time for session chips.
 * @param {string|number|Date|null|undefined} value
 * @param {Date} [now]
 */
export function formatSessionRelativeTime(value, now = new Date()) {
  if (value == null || value === '') return ''
  const date = value instanceof Date ? value : new Date(value)
  if (Number.isNaN(date.getTime())) return ''
  const diffMs = now.getTime() - date.getTime()
  if (diffMs < 0) return '刚刚'
  const sec = Math.floor(diffMs / 1000)
  if (sec < 60) return '刚刚'
  const min = Math.floor(sec / 60)
  if (min < 60) return `${min} 分钟前`
  const hr = Math.floor(min / 60)
  if (hr < 24) return `${hr} 小时前`
  const day = Math.floor(hr / 24)
  if (day < 7) return `${day} 天前`
  const y = date.getFullYear()
  const m = String(date.getMonth() + 1).padStart(2, '0')
  const d = String(date.getDate()).padStart(2, '0')
  return `${y}-${m}-${d}`
}
