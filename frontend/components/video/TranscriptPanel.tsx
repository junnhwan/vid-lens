import { useEffect,useRef,useState,type ReactNode } from 'react'
import type { TimelineAtom,VideoTask } from '@/lib/types'
import { formatTime,formatTimeRange } from '@/components/Citation'
import { Icon } from '@/components/ui/Icon'
import { ClampRead } from './ReadText'
function visualTipSections(text: string): { label: string; body: string }[] {
  const raw = text.trim()
  if (!raw) return []
  const chunks = raw.split(/(?=\d+\)\s*)/).map(s => s.trim()).filter(Boolean)
  if (chunks.length > 1) {
    return chunks.map(chunk => {
      const m = chunk.match(/^\d+\)\s*([^:：\n]+)[:：]?\s*([\s\S]*)$/)
      if (m) return { label: m[1].trim(), body: m[2].trim() }
      return { label: '', body: chunk }
    })
  }
  return [{ label: '', body: raw }]
}

export function TranscriptPanel({task,transcriptAtoms,transcriptRows,visualAtoms,timelineMs,playheadMs,headSnap,liveIndex,seek,children}: {
 task:VideoTask; transcriptAtoms:TimelineAtom[]; transcriptRows:(TimelineAtom & { paragraphs:string[]; source_ids:string[] })[];
 visualAtoms:TimelineAtom[]; timelineMs:number; playheadMs:number; headSnap:boolean; liveIndex:number; seek:(ms:number)=>void; children:ReactNode
}) {
 const [railTip,setRailTip]=useState<{left:number;text:string;timeMs:number}|null>(null)
 const liveRowRef=useRef<HTMLDivElement>(null)
 const durPct=timelineMs>0?timelineMs:1
 const hasRail=timelineMs>0
  useEffect(() => {
    const el = liveRowRef.current
    if (!el) return
    const root = el.closest('.rail-pane')
    if (!(root instanceof HTMLElement)) return
    const rootBox = root.getBoundingClientRect()
    const box = el.getBoundingClientRect()
    if (box.top < rootBox.top + 12 || box.bottom > rootBox.bottom - 12) {
      el.scrollIntoView({ block: 'center', behavior: 'smooth' })
    }
  }, [liveIndex])


    if (!task.has_transcription) {
      return (
        <div className="empty">
          <Icon name="activity" size="lg" />
          <b>{task.visual_status === 'completed' ? '这段视频已有画面内容' : '还没有声音转写'}</b><span>{task.visual_status === 'completed' ? '在「画面证据」中查看关键帧与文字。需要声音文字时再开始转写。' : '讲解视频可开始转写；无声演示可直接分析画面。'}</span>
        </div>
      )
    }
    if (transcriptAtoms.length === 0 && visualAtoms.length === 0) {
      return (
        <div className="empty">
          <Icon name="activity" size="lg" />
          <b>时间轴暂无数据</b>
        </div>
      )
    }
    return (
      <>
        {hasRail && (
          <>
            <div className="tl-rail-wrap">
            <div
              className="tl-rail"
              onPointerDown={e => {
                const rect = e.currentTarget.getBoundingClientRect()
                const ratio = Math.max(0, Math.min(1, (e.clientX - rect.left) / rect.width))
                seek(ratio * timelineMs)
              }}
              onPointerMove={e => {
                const rect = e.currentTarget.getBoundingClientRect()
                const ratio = Math.max(0, Math.min(1, (e.clientX - rect.left) / rect.width))
                const ms = ratio * timelineMs
                const hits = visualAtoms.filter(a => ms >= a.start_ms && ms <= Math.max(a.end_ms, a.start_ms + 1))
                const atom = hits.length > 0 ? hits[hits.length - 1] : null
                if (!atom?.content) { setRailTip(null); return }
                setRailTip({
                  left: Math.max(16, Math.min(rect.width - 16, e.clientX - rect.left)),
                  text: atom.content,
                  timeMs: atom.start_ms,
                })
              }}
              onPointerLeave={() => setRailTip(null)}
            >
              {transcriptRows.map(a => (
                <div
                  key={`tt-${a.id}`}
                  className="tl-seg t-transcript"
                  style={{ left: `${(a.start_ms / durPct) * 100}%`, width: `${Math.max(0.3, ((a.end_ms - a.start_ms) / durPct) * 100)}%` }}
                />
              ))}
              {visualAtoms.map(a => (
                <div
                  key={`tv-${a.id}`}
                  className={`tl-seg ${a.modality === 'visual_ocr' ? 't-ocr' : 't-caption'}`}
                  style={{ left: `${(a.start_ms / durPct) * 100}%`, width: `max(6px, ${((a.end_ms - a.start_ms) / durPct) * 100}%)` }}
                />
              ))}
              <div className={`tl-head${headSnap ? ' snap' : ''}`} style={{ left: `${(playheadMs / durPct) * 100}%` }} />
            </div>
            {railTip && (
              <div className="tl-tip" style={{ ['--tip-x' as string]: `${railTip.left}px` }} role="tooltip">
                <span className="tl-tip-time">{formatTime(railTip.timeMs)}</span>
                {visualTipSections(railTip.text).map((sec, i) => (
                  <div key={i} className="tl-tip-sec">
                    {sec.label && <div className="tl-tip-k">{sec.label}</div>}
                    <div className="tl-tip-v">{sec.body}</div>
                  </div>
                ))}
              </div>
            )}
            </div>
            <div className="tl-scale mono">
              {Array.from({ length: 6 }, (_, i) => (
                <span key={i}>{formatTime((timelineMs * i) / 5)}</span>
              ))}
            </div>
            <div className="tl-legend">
              <span><i style={{ background: 'color-mix(in srgb, var(--mute) 50%, transparent)' }} />解说转写</span>
              <span><i style={{ background: 'color-mix(in srgb, var(--acc) 60%, transparent)' }} />画面 OCR</span>
              <span><i style={{ background: 'color-mix(in srgb, var(--info) 55%, transparent)' }} />画面描述</span>
              <span style={{ marginLeft: 'auto', color: 'var(--tx-4)' }}>悬停色块看画面 · 点击跳转</span>
            </div>
          </>
        )}
        {transcriptRows.length > 0 && (
          <div id="transcript" className="transcript-list">
            {children}
            {transcriptRows.map((a, i) => (
              <div
                key={a.id}
                ref={i === liveIndex ? liveRowRef : undefined}
                className={`t-row${a.time_range_status !== 'exact' ? ' coarse' : ''}${i === liveIndex ? ' live' : ''}`}
              >
                <button type="button" className="transcript-time" disabled={a.time_range_status === 'unknown'} aria-label={a.time_range_status === 'unknown' ? '来源时间未知' : `回放 ${formatTime(a.start_ms)}`} onClick={() => seek(a.start_ms)}>
                  <span className="ts">{a.time_range_status === 'unknown' ? '时间未知' : a.time_range_status === 'exact' ? formatTimeRange(a.start_ms, a.end_ms) : <>{formatTimeRange(a.start_ms, a.end_ms)}<small>{a.end_ms - a.start_ms <= 30000 ? '约定位' : '原片段'}</small></>}</span>
                  {a.time_range_status !== 'unknown' && <Icon name="play" size="sm" />}
                </button>
                <ClampRead className="tx transcript-paragraphs">{a.paragraphs.map((text, paragraph) => <p key={paragraph}>{text}</p>)}</ClampRead>
              </div>
            ))}
          </div>
        )}
      </>
    )
  }
