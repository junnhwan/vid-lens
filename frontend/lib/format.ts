import { TaskStatus, TaskStatusEnum } from './types'

// 任务状态 → 中文标签
// status: 0 Pending / 1 Queued / 2 Running / 3 Completed / 4 Failed / 5 Dead
export function statusLabel(s: TaskStatus): string {
  switch (s) {
    case TaskStatusEnum.Pending: return '待处理'
    case TaskStatusEnum.Queued: return '排队中'
    case TaskStatusEnum.Running: return '处理中'
    case TaskStatusEnum.Completed: return '已完成'
    case TaskStatusEnum.Failed: return '失败'
    case TaskStatusEnum.Dead: return '已废弃'
  }
}

// 播放/转写时钟统一出口: 不足 1h 为 MM:SS(分秒补零), 超过 1h 为 H:MM:SS。
export function formatClock(ms?: number): string {
  if (!Number.isFinite(ms)) return '--:--'
  const total = Math.max(0, Math.floor((ms || 0) / 1000))
  const hours = Math.floor(total / 3600)
  const minutes = Math.floor((total % 3600) / 60)
  const seconds = total % 60
  return hours > 0
    ? `${hours}:${pad(minutes)}:${pad(seconds)}`
    : `${pad(minutes)}:${pad(seconds)}`
}

// 日期时间统一出口: YYYY-MM-DD HH:MM(不再裸用 toLocaleString)
export function fmtDate(iso: string): string {
  const d = new Date(iso)
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`
}
export function fmtDateTime(iso: string): string {
  const d = new Date(iso)
  return `${fmtDate(iso)} ${pad(d.getHours())}:${pad(d.getMinutes())}`
}
// 一天中的时刻 HH:MM(本地),不随运行环境 locale 变化
export function fmtTimeOfDay(iso: string | number): string {
  const d = new Date(iso)
  return `${pad(d.getHours())}:${pad(d.getMinutes())}`
}

// 检索相关度展示精度统一: 两位小数
export function fmtScore(score?: number): string {
  return typeof score === 'number' && Number.isFinite(score) ? score.toFixed(2) : '—'
}

// 文件大小格式化
export function fmtSize(bytes: number): string {
  if (!bytes) return '0B'
  const u = ['B', 'K', 'M', 'G', 'T']
  let i = 0
  let n = bytes
  while (n >= 1024 && i < u.length - 1) { n /= 1024; i++ }
  return `${n.toFixed(n >= 10 || i === 0 ? 0 : 1)}${u[i]}`
}

// 相对时间：刚刚 / N 分钟前 / 昨日 HH:MM / MM-DD
export function fmtRelTime(iso: string): string {
  const d = new Date(iso)
  const now = new Date()
  const diff = (now.getTime() - d.getTime()) / 1000
  if (diff < 60) return '刚刚'
  if (diff < 3600) return `${Math.floor(diff / 60)} 分钟前`
  const sameDay = d.toDateString() === now.toDateString()
  if (sameDay) return `${pad(d.getHours())}:${pad(d.getMinutes())}`
  const yest = new Date(now.getTime() - 86400000)
  if (d.toDateString() === yest.toDateString()) return `昨日 ${pad(d.getHours())}:${pad(d.getMinutes())}`
  if (diff < 86400 * 7) return `${Math.floor(diff / 86400)} 日前`
  return `${pad(d.getMonth() + 1)}-${pad(d.getDate())}`
}
function pad(n: number) { return n < 10 ? `0${n}` : `${n}` }

// 来源标签
export function sourceLabel(t: { source_type: string }): string {
  switch (t.source_type) {
    case 'url': return 'URL 下载'
    case 'chunked': return '分片上传'
    default: return '本地上传'
  }
}

export function taskTitle(t: { title?: string; filename: string; id?: number }): string {
  return t.title || t.filename || (t.id != null ? `任务 #${t.id}` : '未命名')
}

export function indexStatusText(indexStatus: string, retrievable: boolean): string {
  if (retrievable) return '已可检索'
  if (indexStatus === 'pending') return '索引排队中'
  if (indexStatus === 'building') return '索引构建中'
  if (indexStatus === 'failed') return '索引失败'
  return indexStatus || '未索引'
}

export function stripMdPreview(s: string, maxLen = 120): string {
  const plain = s.replace(/[#*`_>\-]/g, ' ').replace(/\s+/g, ' ').trim()
  return plain.length > maxLen ? `${plain.slice(0, maxLen)}…` : plain
}
