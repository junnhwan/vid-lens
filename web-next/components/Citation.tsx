'use client'

import { useState } from 'react'
import { renderMarkdownInline } from '@/components/Markdown'
import type { Citation } from '@/lib/types'

// 引用脚注系统：
// [Cx] 是上标小徽标，点击在回答正下方内联展开引用卡片（max-height 过渡 350ms），不要浮层。
// 一条回答可能有多个 citation，共享一组卡片。
export interface CiteRef {
  id: string          // "C1"
  chunkIndex: number
  score: number
  content: string
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
    const cs = JSON.parse(snapshot) as Citation[]
    return cs.map((c) => ({
      id: c.citation_id || `C${c.chunk_index}`,
      chunkIndex: c.chunk_index,
      score: c.score,
      content: c.content,
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
export function CitationCards({ refs, openIds }: { refs: CiteRef[]; openIds: string[] }) {
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
              {r.finalRank && <span className="border border-ink-0/20 px-1">rank {r.finalRank}</span>}
            </div>
            <p className="font-sans text-[13.5px] leading-[1.75] text-ink-1 hit px-2 py-1">{r.content}</p>
          </div>
        </div>
      ))}
    </>
  )
}

function pad(n: number) { return n < 10 ? `0${n}` : `${n}` }

// 把纯文本里的 [Cx] 标记（由后端 answer 文本里的引用占位）解析成 徽标 + 文本节点。
// 后端 answer 是纯文本，引用通过独立 citations 事件发，文本里通常没有 [Cx] 标记——
// 所以前端在渲染 answer 时按 citations 顺序在句末插入徽标更可控。这里提供两种：
// 1) 若文本含 [C1] 字面量 → 替换成徽标
// 2) 否则按传入的 refIds 在末尾追加徽标群
export function renderAnswerWithCites(
  text: string,
  refIds: string[],
  onToggle: (id: string) => void,
  openIds: string[],
): React.ReactNode {
  // 若文本里没有 [Cx] 字面量，直接渲染纯文本 + 末尾徽标
  if (!/\[C\d+\]/.test(text)) {
    return (
      <>
        {renderMarkdownInline(text)}
        {refIds.length > 0 && (
          <span className="ml-1">{refIds.map(id => <CiteBadge key={id} id={id} onToggle={onToggle} active={openIds.includes(id)} />)}</span>
        )}
      </>
    )
  }
  // 文本含 [Cx] → 拆分替换
  const parts: React.ReactNode[] = []
  const re = /\[C(\d+)\]/g
  let last = 0
  let m: RegExpExecArray | null
  let i = 0
  while ((m = re.exec(text))) {
    parts.push(<span key={`t${i}`}>{renderMarkdownInline(text.slice(last, m.index))}</span>)
    const id = `C${m[1]}`
    parts.push(<CiteBadge key={i++} id={id} onToggle={onToggle} active={openIds.includes(id)} />)
    last = m.index + m[0].length
  }
  parts.push(<span key="t-last">{renderMarkdownInline(text.slice(last))}</span>)
  return parts
}
