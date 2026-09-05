'use client'

import { useState } from 'react'
import { Play } from 'lucide-react'
import Markdown from '@/components/Markdown'
import type { Citation } from '@/lib/types'
// [Cx] 是上标小徽标，点击在回答正下方内联展开引用卡片（max-height 过渡 350ms），不要浮层。
// 一条回答可能有多个 citation，共享一组卡片。
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

// 从后端消息的 retrieval_snapshot（JSON 字符串，引用快照）重建 CiteRef 列表。
// 历史消息加载时用：后端每条 assistant 消息都持久化了引用快照，前端据此恢复引用片段。
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

// 上标徽标。点击 toggle 对应卡片。
export function CiteBadge({ id, onToggle, active }: { id: string; onToggle: (id: string) => void; active: boolean }) {
  return (
    <span className={`cite ${active ? 'open' : ''}`} onClick={(e) => { e.stopPropagation(); onToggle(id) }}>{id}</span>
  )
}

// 引用卡片群：跟在回答下方。openIds 控制哪些展开。
export function CitationCards({ refs, openIds, onPlayCitation }: {
  refs: CiteRef[]
  openIds: string[]
  onPlayCitation?: (citation: CiteRef) => void
}) {
  return (
    <>
      {refs.map((r) => (
        <div key={r.id} className={`cite-card ${openIds.includes(r.id) ? 'open' : ''}`}>
          <div className="border-l-2 pl-4 py-1" style={{ borderColor: r.color || 'var(--accent-500)' }}>
            <div className="flex items-center gap-2 font-mono text-[10px] text-ink-4 mb-1.5 flex-wrap">
              {r.color && <span className="src-dot" style={{ background: r.color }} />}
              <span className="text-sienna-700">{r.id}{r.videoTitle ? ` · ${r.videoTitle}` : ` · 片段 #${pad(r.chunkIndex)}`}</span>
              <span>score {r.score.toFixed(2)}</span>
              {r.source && <span className="border border-ink-0/20 px-1">{r.source}</span>}
              {r.modality && <span className="border border-ink-0/20 px-1">{modalityLabel(r.modality)}</span>}
              {hasReplayRange(r) && <span>{formatTimeRange(r.startMS!, r.endMS!)}</span>}
              {r.finalRank && <span className="border border-ink-0/20 px-1">rank {r.finalRank}</span>}
            </div>
            <p className="font-sans text-[13.5px] leading-[1.75] text-ink-1 hit px-2 py-1">{r.displayContext || r.content}</p>
            {r.anchorQuote && r.displayContext && r.anchorQuote !== r.displayContext && (
              <p className="font-sans text-[11px] leading-relaxed text-sienna-800 bg-sienna-500/8 rounded-md mx-2 mt-1 px-2 py-1">
                支持片段：{r.anchorQuote}
              </p>
            )}
            {onPlayCitation && hasReplayRange(r) && (
              <button
                type="button"
                onClick={() => onPlayCitation(r)}
                className="mt-2 ml-2 inline-flex items-center gap-1.5 rounded-md border border-ink-0/15 px-2 py-1 text-[11px] text-ink-2 hover:bg-paper-2"
              >
                <Play className="w-3 h-3" fill="currentColor" />回放原视频
              </button>
            )}
          </div>
        </div>
      ))}
    </>
  )
}

function hasReplayRange(cite: CiteRef) {
  return Number.isFinite(cite.startMS) && Number.isFinite(cite.endMS) && (cite.endMS || 0) > (cite.startMS || 0)
}

function formatTimeRange(startMS: number, endMS: number) {
  return `${formatTime(startMS)}–${formatTime(endMS)}`
}

function formatTime(ms: number) {
  const total = Math.max(0, Math.floor(ms / 1000))
  const hours = Math.floor(total / 3600)
  const minutes = Math.floor((total % 3600) / 60)
  const seconds = total % 60
  return hours > 0
    ? `${hours}:${String(minutes).padStart(2, '0')}:${String(seconds).padStart(2, '0')}`
    : `${minutes}:${String(seconds).padStart(2, '0')}`
}

function modalityLabel(modality: string) {
  if (modality === 'transcript') return '字幕'
  if (modality === 'visual_ocr') return '画面 OCR'
  if (modality === 'visual_caption') return '画面描述'
  return modality
}

function pad(n: number) { return n < 10 ? `0${n}` : `${n}` }

// 把纯文本里的 [Cx] 标记（由后端 answer 文本里的引用占位）解析成 徽标 + 文本节点。
// 后端 answer 是纯文本，引用通过独立 citations 事件发，文本里通常没有 [Cx] 标记——
// 所以前端在渲染 answer 时按 citations 顺序在句末插入徽标更可控。这里提供两种：
// 1) 若文本含 [C1] 字面量 → 按引用位置拆分，每段仍走块级 Markdown
// 2) 否则直接整段块级 Markdown 渲染 + 末尾徽标群
export function renderAnswerWithCites(
  text: string,
  refIds: string[],
  onToggle: (id: string) => void,
  openIds: string[],
): React.ReactNode {
  // 若文本里没有 [Cx] 字面量，直接块级渲染 + 末尾徽标
  if (!/\[C\d+\]/.test(text)) {
    return (
      <>
        <Markdown content={text} />
        {refIds.length > 0 && (
          <span className="ml-1">{refIds.map(id => <CiteBadge key={id} id={id} onToggle={onToggle} active={openIds.includes(id)} />)}</span>
        )}
      </>
    )
  }
  // 文本含 [Cx] → 拆分替换（每段仍走块级渲染，保持列表/段落结构）
  const parts: React.ReactNode[] = []
  const re = /\[C(\d+)\]/g
  let last = 0
  let m: RegExpExecArray | null
  let i = 0
  while ((m = re.exec(text))) {
    const seg = text.slice(last, m.index)
    if (seg.trim()) parts.push(<Markdown key={`t${i}`} content={seg} />)
    const id = `C${m[1]}`
    parts.push(<CiteBadge key={i++} id={id} onToggle={onToggle} active={openIds.includes(id)} />)
    last = m.index + m[0].length
  }
  const tail = text.slice(last)
  if (tail && tail.trim()) parts.push(<Markdown key="t-last" content={tail} />)
  return parts
}
