// 后端 Citation → 前端 CiteRef，流式回答与历史快照共用同一映射和时间规则。

import type { Citation } from '@/lib/types'
import { formatClock } from '@/lib/format'

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
  evidenceId?: string      // 后端 evidence_id,证据抽屉展示用
  sourceMappingStatus?: string // source_mapping_status: mapped/partial/unmapped
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
    return cs.map((c, index) => ({
      ...citeFromAPI(c, index),
      color: c.task_id && memberColor ? memberColor(c.task_id) : undefined,
    }))
  } catch {
    return []
  }
}

export function citeFromAPI(c: Citation, index: number): CiteRef {
  return {
      id: c.citation_id || `C${index + 1}`,
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
      evidenceId: c.evidence_id,
      sourceMappingStatus: c.source_mapping_status,
  }
}

export function hasReplayRange(cite: Pick<CiteRef, 'startMS' | 'endMS' | 'timeRangeStatus' | 'modality'>): boolean {
  if (cite.timeRangeStatus === 'unknown' || !Number.isFinite(cite.startMS) || !Number.isFinite(cite.endMS) || cite.startMS! < 0) return false
  if (cite.endMS! > cite.startMS!) return true
  return cite.endMS === cite.startMS && cite.timeRangeStatus === 'exact' && (cite.modality === 'visual_ocr' || cite.modality === 'visual_caption')
}

export function citationTimeLabel(cite: Pick<CiteRef, 'startMS' | 'endMS' | 'timeRangeStatus' | 'modality'>): string {
  if (!hasReplayRange(cite)) return '时间未知'
  const time = cite.startMS === cite.endMS ? formatTime(cite.startMS) : formatTimeRange(cite.startMS, cite.endMS)
  return cite.timeRangeStatus === 'coarse' ? `原片段 ${time}` : time
}

export function needsCitationUpgrade(cite: Pick<CiteRef, 'startMS' | 'endMS' | 'timeRangeStatus' | 'modality'>): boolean {
  return (!cite.modality || cite.modality === 'transcript') && cite.timeRangeStatus === 'coarse' && hasReplayRange(cite) && cite.endMS! - cite.startMS! > 30_000
}

export function formatTime(ms?: number): string {
  return formatClock(ms)
}

export function formatTimeRange(startMS?: number, endMS?: number): string {
  return `${formatTime(startMS)} – ${formatTime(endMS)}`
}
