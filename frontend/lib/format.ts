import { TaskStatus, TaskStatusEnum, TaskStage } from './types'

// 任务状态 → 中文标签 + 颜色类（对应 mockup 徽标配色）
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

// 徽标 class：底色 + 文字色 + 是否脉冲点
export function statusBadge(s: TaskStatus): { cls: string; live: boolean } {
  switch (s) {
    case TaskStatusEnum.Pending:
      return { cls: 'bg-ink-0/5 text-ink-3', live: false }
    case TaskStatusEnum.Queued:
      return { cls: 'bg-ink-0/5 text-ink-3', live: false }
    case TaskStatusEnum.Running:
      return { cls: 'bg-sienna-500/10 text-sienna-700', live: true }
    case TaskStatusEnum.Completed:
      return { cls: 'bg-moss/10 text-moss', live: false }
    case TaskStatusEnum.Failed:
      return { cls: 'bg-rust/10 text-rust', live: false }
    case TaskStatusEnum.Dead:
      return { cls: 'bg-ink-0 text-paper-0', live: false }
  }
}

// 三阶段进度：根据 task 的 stage + has_transcription/has_summary + RAG 索引推断
// 返回每阶段 { label, state: 'done'|'running'|'queued', pct }
export type PhaseState = 'done' | 'running' | 'queued'
export interface Phase { label: string; state: PhaseState; pct: number; detail?: string }

export function computePhases(task: {
  status: TaskStatus
  stage: TaskStage
  has_transcription: boolean
  has_summary: boolean
}): Phase[] {
  const { status, stage, has_transcription, has_summary } = task
  // ASR 阶段
  let asr: PhaseState = 'queued'
  let asrPct = 5
  if (has_transcription) { asr = 'done'; asrPct = 100 }
  else if (stage === 'transcribing' || stage === 'visual_indexing') { asr = 'running'; asrPct = 55 }
  else if (stage === 'downloading' || stage === 'uploaded') { asr = 'running'; asrPct = 20 }

  // 摘要阶段
  let sum: PhaseState = 'queued'
  let sumPct = 5
  if (has_summary) { sum = 'done'; sumPct = 100 }
  else if (stage === 'summarizing') { sum = 'running'; sumPct = 60 }
  else if (asr === 'done') { sum = 'queued'; sumPct = 5 }

  // 索引阶段（RAG，无独立 stage 字段，简化：summarizing 之后或 indexing）
  let idx: PhaseState = 'queued'
  let idxPct = 5
  if (stage === 'indexing' || stage === 'visual_indexing') { idx = 'running'; idxPct = 50 }
  else if (sum === 'done') { idx = 'queued'; idxPct = 5 }

  // 失败态全部标灰
  if (status === TaskStatusEnum.Failed || status === TaskStatusEnum.Dead) {
    return [
      { label: '转写', state: 'queued', pct: 5 },
      { label: '摘要', state: 'queued', pct: 5 },
      { label: '索引', state: 'queued', pct: 5 },
    ]
  }
  return [
    { label: '转写', state: asr, pct: asrPct },
    { label: '摘要', state: sum, pct: sumPct },
    { label: '索引', state: idx, pct: idxPct },
  ]
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

export function stripMdPreview(s: string, maxLen = 120): string {
  const plain = s.replace(/[#*`_>\-]/g, ' ').replace(/\s+/g, ' ').trim()
  return plain.length > maxLen ? `${plain.slice(0, maxLen)}…` : plain
}
