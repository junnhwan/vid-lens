'use client'

import { useEffect } from 'react'
import { Brain, Database, Wrench, Sparkles, Video, Library, ChevronDown, Play, RotateCw } from 'lucide-react'
import type { AgentStep, QaScope } from '@/components/prototype/agent/types'
import { DEMO_KBS, DEMO_VIDEOS } from '@/components/prototype/agent/types'
import { useTypewriter } from '@/components/prototype/agent/useTypewriter'

export function StepIcon({ kind, className = 'w-3.5 h-3.5' }: { kind: AgentStep['kind']; className?: string }) {
  if (kind === 'think') return <Brain className={className} />
  if (kind === 'retrieve') return <Database className={className} />
  if (kind === 'tool') return <Wrench className={className} />
  return <Sparkles className={className} />
}

export function StatusDot({ status }: { status: AgentStep['status'] }) {
  if (status === 'running') return <span className="w-2 h-2 rounded-full bg-amber-500 proto-agent-pulse" />
  if (status === 'done') return <span className="w-2 h-2 rounded-full bg-emerald-500" />
  if (status === 'error') return <span className="w-2 h-2 rounded-full bg-red-500" />
  return <span className="w-2 h-2 rounded-full bg-stone-300" />
}

/** 问答范围选择：本视频 vs 知识库（三种布局通过 layout 区分） */
export function ScopePicker({
  scope, onScope, layout = 'tabs', videoId, kbId, onVideo, onKb,
}: {
  scope: QaScope
  onScope: (s: QaScope) => void
  layout?: 'tabs' | 'pills' | 'cards'
  videoId: number
  kbId: number
  onVideo: (id: number) => void
  onKb: (id: number) => void
}) {
  const video = DEMO_VIDEOS.find(v => v.id === videoId) ?? DEMO_VIDEOS[0]
  const kb = DEMO_KBS.find(k => k.id === kbId) ?? DEMO_KBS[0]

  if (layout === 'cards') {
    return (
      <div className="grid grid-cols-2 gap-2">
        <button
          onClick={() => onScope('video')}
          className={`p-3 rounded-xl border text-left transition-all proto-card-hover ${
            scope === 'video' ? 'border-amber-400 bg-amber-50/80 shadow-sm' : 'border-stone-200 bg-white'
          }`}
        >
          <div className="flex items-center gap-2 text-[11px] text-stone-500 mb-1"><Video className="w-3.5 h-3.5" />单视频问答</div>
          <div className="text-[12px] font-medium text-stone-900 truncate">{video.title}</div>
          <div className="text-[10px] text-stone-400 mt-1">仅检索当前视频转写</div>
        </button>
        <button
          onClick={() => onScope('kb')}
          className={`p-3 rounded-xl border text-left transition-all proto-card-hover ${
            scope === 'kb' ? 'border-violet-400 bg-violet-50/80 shadow-sm' : 'border-stone-200 bg-white'
          }`}
        >
          <div className="flex items-center gap-2 text-[11px] text-stone-500 mb-1"><Library className="w-3.5 h-3.5" />知识库问答</div>
          <div className="text-[12px] font-medium text-stone-900 truncate">{kb.name}</div>
          <div className="text-[10px] text-stone-400 mt-1">跨 {kb.videos} 个视频检索</div>
        </button>
      </div>
    )
  }

  if (layout === 'pills') {
    return (
      <div className="flex flex-wrap items-center gap-2">
        <div className="flex rounded-lg border border-stone-200 overflow-hidden text-[11px]">
          <button onClick={() => onScope('video')} className={`px-3 py-1.5 flex items-center gap-1.5 ${scope === 'video' ? 'bg-stone-900 text-white' : 'text-stone-500'}`}>
            <Video className="w-3 h-3" />单视频
          </button>
          <button onClick={() => onScope('kb')} className={`px-3 py-1.5 flex items-center gap-1.5 ${scope === 'kb' ? 'bg-violet-700 text-white' : 'text-stone-500'}`}>
            <Library className="w-3 h-3" />知识库
          </button>
        </div>
        <select
          value={scope === 'video' ? videoId : kbId}
          onChange={e => scope === 'video' ? onVideo(Number(e.target.value)) : onKb(Number(e.target.value))}
          className="h-8 px-2 rounded-lg border border-stone-200 text-[11px] bg-white max-w-[200px] truncate"
        >
          {scope === 'video'
            ? DEMO_VIDEOS.map(v => <option key={v.id} value={v.id}>{v.title}</option>)
            : DEMO_KBS.map(k => <option key={k.id} value={k.id}>{k.name}</option>)}
        </select>
      </div>
    )
  }

  return (
    <div className="space-y-2">
      <div className="flex gap-1 p-1 rounded-lg bg-stone-100 text-[11px]">
        <button onClick={() => onScope('video')} className={`flex-1 py-1.5 rounded-md flex items-center justify-center gap-1.5 ${scope === 'video' ? 'bg-white shadow-sm text-stone-900 font-medium' : 'text-stone-500'}`}>
          <Video className="w-3 h-3" />单视频
        </button>
        <button onClick={() => onScope('kb')} className={`flex-1 py-1.5 rounded-md flex items-center justify-center gap-1.5 ${scope === 'kb' ? 'bg-white shadow-sm text-violet-800 font-medium' : 'text-stone-500'}`}>
          <Library className="w-3 h-3" />知识库
        </button>
      </div>
      <div className="text-[11px] text-stone-500 px-1">
        当前：{scope === 'video' ? `「${video.title}」` : `「${kb.name}」· ${kb.videos} 个视频`}
      </div>
    </div>
  )
}

export function DemoControls({ running, onRun, onReset }: { running: boolean; onRun: () => void; onReset: () => void }) {
  return (
    <div className="flex items-center gap-2">
      <button
        onClick={onRun}
        disabled={running}
        className="h-8 px-3 rounded-lg bg-stone-900 text-white text-[11px] flex items-center gap-1.5 proto-btn-lift disabled:opacity-50"
      >
        <Play className="w-3 h-3" />{running ? '演示中…' : '重播 Agent 流程'}
      </button>
      <button onClick={onReset} className="h-8 px-2 rounded-lg border border-stone-200 text-[11px] text-stone-500 proto-btn-lift">
        <RotateCw className="w-3 h-3" />
      </button>
    </div>
  )
}

export function UserBubble({ text }: { text: string }) {
  return (
    <div className="flex justify-end proto-fade-in">
      <div className="bg-stone-900 text-white text-[14px] px-4 py-2.5 rounded-2xl rounded-br-md max-w-[85%]">{text}</div>
    </div>
  )
}

export function CiteChips({ cites, open, onToggle }: { cites: { id: string; text: string; video?: string }[]; open: string[]; onToggle: (id: string) => void }) {
  return (
    <div className="space-y-2 mt-3">
      <div className="flex flex-wrap gap-1">
        {cites.map(c => (
          <button
            key={c.id}
            onClick={() => onToggle(c.id)}
            className={`text-[10px] font-mono px-2 py-0.5 rounded border transition-all proto-btn-lift ${
              open.includes(c.id) ? 'bg-amber-600 text-white border-amber-600' : 'bg-amber-50 text-amber-800 border-amber-200'
            }`}
          >
            {c.id}
          </button>
        ))}
      </div>
      {cites.filter(c => open.includes(c.id)).map(c => (
        <div key={c.id} className="p-3 rounded-lg bg-white border border-amber-200/60 text-[12px] text-stone-600 proto-expand">
          {c.video && <div className="text-[10px] text-violet-600 mb-1">{c.video}</div>}
          {c.text}
        </div>
      ))}
    </div>
  )
}

/** 最终回答：柔和打字机 + 完成后淡入引用 */
export function AnswerTypewriter({
  content,
  active,
  cites,
  openCites,
  onToggleCite,
  className = 'text-[15px] leading-relaxed text-stone-900',
  onTypingChange,
}: {
  content: string
  active: boolean
  cites?: { id: string; text: string; video?: string }[]
  openCites?: string[]
  onToggleCite?: (id: string) => void
  className?: string
  onTypingChange?: (typing: boolean) => void
}) {
  const { displayed, finished, typing } = useTypewriter(content, active)

  useEffect(() => {
    onTypingChange?.(typing || (active && !finished))
  }, [typing, active, finished, onTypingChange])

  return (
    <>
      <p className={className}>
        {displayed}
        {active && !finished && (
          <span className="proto-typewriter-cursor" aria-hidden />
        )}
      </p>
      {finished && cites && openCites && onToggleCite && (
        <div className="proto-typewriter-cites-in">
          <CiteChips cites={cites} open={openCites} onToggle={onToggleCite} />
        </div>
      )}
    </>
  )
}

export function Collapse({ title, open, onToggle, children, accent }: {
  title: string; open: boolean; onToggle: () => void; children: React.ReactNode; accent?: string
}) {
  return (
    <div className={`rounded-xl border overflow-hidden ${accent || 'border-stone-200 bg-white'}`}>
      <button onClick={onToggle} className="w-full flex items-center justify-between px-3 py-2 text-[11px] text-stone-600 hover:bg-stone-50">
        <span>{title}</span>
        <ChevronDown className={`w-3.5 h-3.5 transition-transform ${open ? 'rotate-180' : ''}`} />
      </button>
      {open && <div className="px-3 pb-3 border-t border-stone-100">{children}</div>}
    </div>
  )
}
