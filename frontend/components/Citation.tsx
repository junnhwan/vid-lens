// 引用数据契约与工具:后端 Citation → 前端 CiteRef。
// 表现层(行内 C# chip、证据抽屉、回放)按 docs/prototype 在聊天阶段实现;
// 本文件只保留已验证的快照解析,供 useConversationSession / chatUtils 使用。

import type { Citation } from '@/lib/types'

export interface CiteRef {
  id: string          // "C1"
  taskId?: number
  chunkIndex: number
  score: number
  content: string
  anchorQuote?: string
  displayContext?: string
  modality?: string
  startMS?: number
  endMS?: number
  timeRangeStatus?: string
  contextStartMS?: number
  contextEndMS?: number
  displayContextTruncated?: boolean
  sourceRefs?: Citation['source_refs']
  source?: string
  videoTitle?: string // kb 跨视频用
  finalRank?: number
  color?: string      // kb 跨视频色点
}

// 从后端消息的 retrieval_snapshot(JSON 字符串,引用快照)重建 CiteRef 列表。
// 历史消息加载时用:后端每条 assistant 消息都持久化了引用快照。
export function citesFromSnapshot(snapshot?: string, memberColor?: (taskId: number) => string): CiteRef[] {
  if (!snapshot) return []
  try {
    const parsed = JSON.parse(snapshot) as Citation[] | { citations?: Citation[] }
    const cs: Citation[] = Array.isArray(parsed)
      ? parsed
      : Array.isArray(parsed.citations) ? parsed.citations : []
    return cs.map((c) => ({
      id: c.citation_id || `C${c.chunk_index}`,
      taskId: c.task_id,
      chunkIndex: c.chunk_index,
      score: c.score,
      content: c.content,
      anchorQuote: c.anchor_quote || c.content,
      displayContext: c.display_context || c.content,
      modality: c.modality,
      startMS: c.start_ms,
      endMS: c.end_ms,
      timeRangeStatus: c.time_range_status,
      contextStartMS: c.context_start_ms,
      contextEndMS: c.context_end_ms,
      displayContextTruncated: c.display_context_truncated,
      sourceRefs: c.source_refs,
      source: c.source,
      videoTitle: c.video_title,
      finalRank: c.final_rank,
      color: c.task_id && memberColor ? memberColor(c.task_id) : undefined,
    }))
  } catch {
    return []
  }
}

export function hasReplayRange(cite: Pick<CiteRef, 'startMS' | 'endMS'>): boolean {
  return Number.isFinite(cite.startMS) && Number.isFinite(cite.endMS) && (cite.endMS || 0) > (cite.startMS || 0)
}

export function formatTime(ms?: number): string {
  if (!Number.isFinite(ms)) return '--:--'
  const total = Math.max(0, Math.floor((ms || 0) / 1000))
  const hours = Math.floor(total / 3600)
  const minutes = Math.floor((total % 3600) / 60)
  const seconds = total % 60
  return hours > 0
    ? `${hours}:${String(minutes).padStart(2, '0')}:${String(seconds).padStart(2, '0')}`
    : `${String(minutes).padStart(2, '0')}:${String(seconds).padStart(2, '0')}`
}

export function formatTimeRange(startMS?: number, endMS?: number): string {
  return `${formatTime(startMS)} – ${formatTime(endMS)}`
}

export function modalityLabel(modality?: string): string {
  if (modality === 'transcript') return '转写'
  if (modality === 'visual_ocr') return '画面 OCR'
  if (modality === 'visual_caption') return '画面描述'
  return modality || ''
}
