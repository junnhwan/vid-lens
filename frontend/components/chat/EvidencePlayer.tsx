'use client'

import { useEffect, useRef } from 'react'
import { Play } from 'lucide-react'
import type { TimelineAtom, VideoTimeline } from '@/lib/types'

export interface PlaybackRange {
  startMS: number
  endMS: number
  label?: string
}

export default function EvidencePlayer({
  timeline,
  playbackUrl,
  activeRange,
  onSelectRange,
}: {
  timeline: VideoTimeline | null
  playbackUrl?: string
  activeRange?: PlaybackRange | null
  onSelectRange?: (range: PlaybackRange) => void
}) {
  const videoRef = useRef<HTMLVideoElement>(null)

  useEffect(() => {
    const video = videoRef.current
    if (!video || !activeRange || !playbackUrl) return
    const seekAndPlay = () => {
      video.currentTime = Math.max(0, activeRange.startMS / 1000)
      void video.play().catch(() => {})
    }
    if (video.readyState >= 1) seekAndPlay()
    else video.addEventListener('loadedmetadata', seekAndPlay, { once: true })
    return () => video.removeEventListener('loadedmetadata', seekAndPlay)
  }, [activeRange, playbackUrl])

  if (!timeline) return null

  return (
    <section className="rounded-2xl border border-ink-0/8 bg-paper-0 overflow-hidden ui-fade-in">
      <div className="flex items-center justify-between gap-3 px-4 py-3 border-b border-ink-0/8">
        <div>
          <div className="text-[12px] font-medium text-ink-1">证据时间线</div>
          <div className="text-[10px] text-ink-4 mt-0.5">
            {timeline.title || '当前视频'} · {timeline.atoms.length} 个来源观察
          </div>
        </div>
        {!playbackUrl && <span className="text-[10px] text-ink-4">回放链接暂不可用</span>}
      </div>

      {playbackUrl && (
        <video
          ref={videoRef}
          className="w-full aspect-video max-h-[360px] bg-ink-0 object-contain"
          src={playbackUrl}
          controls
          preload="metadata"
          aria-label="视频证据播放器"
        />
      )}

      <div className="max-h-48 overflow-y-auto divide-y divide-ink-0/6">
        {timeline.atoms.length === 0 ? (
          <p className="px-4 py-3 text-[11px] text-ink-4">尚无可展示的结构化时间线来源。</p>
        ) : timeline.atoms.map(atom => (
          <TimelineRow key={atom.id} atom={atom} active={isActive(atom, activeRange)} onClick={onSelectRange} />
        ))}
      </div>
    </section>
  )
}

function TimelineRow({ atom, active, onClick }: {
  atom: TimelineAtom
  active: boolean
  onClick?: (range: PlaybackRange) => void
}) {
  const playable = atom.end_ms > atom.start_ms
  const body = (
    <>
      <div className="flex items-center gap-2 text-[10px] text-ink-4 mb-1">
        <span className="font-mono text-sienna-700">{formatTime(atom.start_ms)}</span>
        <span className="border border-ink-0/15 px-1">{modalityLabel(atom.modality)}</span>
        {atom.time_range_status !== 'exact' && <span>{atom.time_range_status === 'unknown' ? '时间未知' : '粗略时间'}</span>}
      </div>
      <p className="text-[12px] leading-relaxed text-ink-1 line-clamp-2">{atom.content}</p>
    </>
  )

  if (!onClick || !playable) {
    return <div className="px-4 py-2.5">{body}</div>
  }
  return (
    <button
      type="button"
      className={`w-full text-left px-4 py-2.5 hover:bg-sienna-500/6 ${active ? 'bg-sienna-500/10' : ''}`}
      onClick={() => onClick({ startMS: atom.start_ms, endMS: atom.end_ms, label: atom.content })}
      title="跳转并播放此来源"
    >
      <span className="flex items-start gap-2">
        <Play className="w-3 h-3 mt-1 shrink-0 text-sienna-700" fill="currentColor" />
        <span className="min-w-0">{body}</span>
      </span>
    </button>
  )
}

function isActive(atom: TimelineAtom, range?: PlaybackRange | null) {
  return Boolean(range && atom.start_ms === range.startMS && atom.end_ms === range.endMS)
}

function modalityLabel(modality: string) {
  if (modality === 'transcript') return '字幕'
  if (modality === 'visual_ocr') return '画面 OCR'
  if (modality === 'visual_caption') return '画面描述'
  return modality
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
