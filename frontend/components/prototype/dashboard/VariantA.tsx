'use client'

// 探索方案 A — 沉浸网格：无侧栏，大卡片瀑布流 + 居中搜索 + 右侧滑出详情
import Link from 'next/link'
import { useMemo, useState } from 'react'
import {
  Search, Upload, X, MessageCircle, Play, Film, BookOpen, Settings,
  Layers, Sparkles, Clock, ChevronRight,
} from 'lucide-react'
import type { VideoTask } from '@/lib/types'
import { TaskStatusEnum } from '@/lib/types'
import { statusBadge, statusLabel, computePhases, fmtRelTime, sourceLabel } from '@/lib/format'
import type { DashboardPrototypeProps } from './types'
import { taskTitle } from './types'

export const displayName = '沉浸网格'

const GRADIENTS = [
  'from-violet-600/80 via-fuchsia-500/60 to-amber-400/70',
  'from-cyan-600/70 via-blue-500/60 to-indigo-600/80',
  'from-emerald-600/70 via-teal-500/60 to-cyan-500/70',
  'from-rose-600/70 via-orange-500/60 to-amber-500/70',
  'from-slate-700/80 via-zinc-600/70 to-stone-500/60',
]

function thumbGradient(id: number) {
  return GRADIENTS[id % GRADIENTS.length]
}

export default function VariantA({
  tasks, loading, error, onRefresh, onUpload, selectedId, onSelect,
}: DashboardPrototypeProps) {
  const [query, setQuery] = useState('')
  const selected = tasks.find(t => t.id === selectedId) ?? null

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase()
    if (!q) return tasks
    return tasks.filter(t => taskTitle(t).toLowerCase().includes(q))
  }, [tasks, query])

  const running = tasks.filter(t =>
    t.status === TaskStatusEnum.Running || t.status === TaskStatusEnum.Queued || t.status === TaskStatusEnum.Pending
  ).length

  return (
    <div className="h-screen flex flex-col bg-[#0c0c0f] text-zinc-100 overflow-hidden">
      {/* 顶栏：浮动胶囊导航 */}
      <header className="shrink-0 px-6 pt-5 pb-3">
        <div className="max-w-6xl mx-auto flex items-center justify-between gap-4">
          <div className="flex items-center gap-3">
            <div className="w-9 h-9 rounded-xl bg-gradient-to-br from-violet-500 to-fuchsia-600 flex items-center justify-center text-sm font-bold shadow-lg shadow-violet-500/25">
              映
            </div>
            <div>
              <div className="text-[15px] font-semibold tracking-tight">映知</div>
              <div className="text-[10px] text-zinc-500">观之以映，释之以知</div>
            </div>
          </div>

          <nav className="hidden sm:flex items-center gap-1 px-1.5 py-1 rounded-full bg-zinc-900/80 border border-zinc-800">
            <NavPill active href="#">视频库</NavPill>
            <NavPill href="/kb">知识库</NavPill>
            <NavPill href="/settings">设置</NavPill>
          </nav>

          <button
            onClick={onUpload}
            className="h-9 px-4 rounded-full bg-white text-zinc-900 text-[13px] font-medium flex items-center gap-1.5 hover:bg-zinc-100 transition-colors shadow-lg shadow-white/10"
          >
            <Upload className="w-3.5 h-3.5" />上传
          </button>
        </div>
      </header>

      {/* Spotlight 搜索 */}
      <div className="shrink-0 px-6 pb-4">
        <div className="max-w-xl mx-auto relative">
          <Search className="absolute left-4 top-1/2 -translate-y-1/2 w-4 h-4 text-zinc-500" />
          <input
            value={query}
            onChange={e => setQuery(e.target.value)}
            placeholder="搜索视频标题…"
            className="w-full h-12 pl-11 pr-4 rounded-2xl bg-zinc-900/90 border border-zinc-800 text-[14px] placeholder:text-zinc-600 focus:outline-none focus:ring-2 focus:ring-violet-500/40 focus:border-violet-500/50"
          />
        </div>
        <div className="max-w-xl mx-auto mt-2 flex items-center justify-center gap-4 text-[11px] text-zinc-500">
          <span>{tasks.length} 个视频</span>
          {running > 0 && <span className="text-violet-400 flex items-center gap-1"><span className="w-1.5 h-1.5 rounded-full bg-violet-400 animate-pulse" />{running} 处理中</span>}
        </div>
      </div>

      {/* 主网格 */}
      <main className="flex-1 min-h-0 overflow-y-auto scroll-thin px-6 pb-24">
        {error && (
          <div className="max-w-6xl mx-auto py-8 text-center text-rose-400 text-[13px]">
            {error} <button onClick={onRefresh} className="underline ml-1">重试</button>
          </div>
        )}
        {loading ? (
          <div className="max-w-6xl mx-auto grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4 gap-4">
            {Array.from({ length: 8 }).map((_, i) => (
              <div key={i} className="aspect-[4/3] rounded-2xl bg-zinc-900 animate-pulse" />
            ))}
          </div>
        ) : filtered.length === 0 ? (
          <EmptyState onUpload={onUpload} hasTasks={tasks.length > 0} />
        ) : (
          <div className="max-w-6xl mx-auto grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4 gap-4">
            {filtered.map(t => (
              <VideoTile
                key={t.id}
                task={t}
                selected={selectedId === t.id}
                onClick={() => onSelect(t.id)}
              />
            ))}
          </div>
        )}
      </main>

      {/* 右侧滑出详情 */}
      {selected && (
        <DetailDrawer task={selected} onClose={() => onSelect(null)} />
      )}
    </div>
  )
}

function NavPill({ children, href, active }: { children: React.ReactNode; href: string; active?: boolean }) {
  const cls = `px-3.5 py-1.5 rounded-full text-[12px] transition-colors ${
    active ? 'bg-zinc-800 text-white font-medium' : 'text-zinc-400 hover:text-zinc-200'
  }`
  if (active) return <span className={cls}>{children}</span>
  return <Link href={href} className={cls}>{children}</Link>
}

function VideoTile({ task, selected, onClick }: { task: VideoTask; selected: boolean; onClick: () => void }) {
  const badge = statusBadge(task.status)
  const phases = computePhases(task)
  const runningPhase = phases.find(p => p.state === 'running')

  return (
    <button
      onClick={onClick}
      className={`group text-left rounded-2xl overflow-hidden border transition-all duration-200 ${
        selected
          ? 'border-violet-500 ring-2 ring-violet-500/30 scale-[1.02]'
          : 'border-zinc-800 hover:border-zinc-600 hover:scale-[1.01]'
      }`}
    >
      <div className={`relative aspect-[4/3] bg-gradient-to-br ${thumbGradient(task.id)}`}>
        <div className="absolute inset-0 bg-black/20 group-hover:bg-black/10 transition-colors" />
        <div className="absolute inset-0 flex items-center justify-center opacity-0 group-hover:opacity-100 transition-opacity">
          <div className="w-12 h-12 rounded-full bg-white/20 backdrop-blur flex items-center justify-center">
            <Play className="w-5 h-5 text-white ml-0.5" fill="white" />
          </div>
        </div>
        <div className="absolute top-3 left-3">
          <span className={`inline-flex items-center gap-1 px-2 py-0.5 rounded-full text-[10px] font-medium backdrop-blur-md bg-black/40 text-white`}>
            {badge.live && <span className="w-1.5 h-1.5 rounded-full bg-violet-400 animate-pulse" />}
            {statusLabel(task.status)}
          </span>
        </div>
        {runningPhase && (
          <div className="absolute bottom-0 inset-x-0 h-1 bg-black/30">
            <div className="h-full bg-violet-400 transition-all" style={{ width: `${runningPhase.pct}%` }} />
          </div>
        )}
      </div>
      <div className="p-3.5 bg-zinc-900/80">
        <div className="text-[14px] font-medium truncate text-zinc-100">{taskTitle(task)}</div>
        <div className="flex items-center justify-between mt-1.5 text-[11px] text-zinc-500">
          <span>{sourceLabel(task)}</span>
          <span className="flex items-center gap-1"><Clock className="w-3 h-3" />{fmtRelTime(task.created_at)}</span>
        </div>
        <div className="flex gap-1.5 mt-2.5">
          {phases.map(p => (
            <span
              key={p.label}
              className={`text-[9px] px-1.5 py-0.5 rounded ${
                p.state === 'done' ? 'bg-emerald-500/20 text-emerald-400'
                : p.state === 'running' ? 'bg-violet-500/20 text-violet-300'
                : 'bg-zinc-800 text-zinc-600'
              }`}
            >
              {p.label}
            </span>
          ))}
        </div>
      </div>
    </button>
  )
}

function DetailDrawer({ task, onClose }: { task: VideoTask; onClose: () => void }) {
  const phases = computePhases(task)
  const canChat = task.has_transcription

  return (
    <>
      <div className="fixed inset-0 bg-black/60 z-40 backdrop-blur-sm" onClick={onClose} />
      <aside className="fixed top-0 right-0 bottom-0 w-full max-w-md z-50 bg-zinc-950 border-l border-zinc-800 flex flex-col shadow-2xl">
        <div className={`shrink-0 h-40 bg-gradient-to-br ${thumbGradient(task.id)} relative`}>
          <button onClick={onClose} className="absolute top-4 right-4 w-8 h-8 rounded-full bg-black/40 flex items-center justify-center text-white hover:bg-black/60">
            <X className="w-4 h-4" />
          </button>
          <div className="absolute bottom-4 left-5 right-5">
            <div className="text-[18px] font-semibold text-white leading-tight">{taskTitle(task)}</div>
            <div className="text-[12px] text-white/70 mt-1">{statusLabel(task.status)} · {sourceLabel(task)}</div>
          </div>
        </div>

        <div className="flex-1 overflow-y-auto p-5 space-y-5">
          <section>
            <SectionLabel icon={<Layers className="w-3.5 h-3.5" />} text="处理进度" />
            <div className="mt-3 space-y-2">
              {phases.map(p => (
                <div key={p.label} className="flex items-center gap-3">
                  <span className="text-[12px] text-zinc-400 w-10">{p.label}</span>
                  <div className="flex-1 h-1.5 rounded-full bg-zinc-800 overflow-hidden">
                    <div
                      className={`h-full rounded-full transition-all ${
                        p.state === 'done' ? 'bg-emerald-500' : p.state === 'running' ? 'bg-violet-500' : 'bg-zinc-700'
                      }`}
                      style={{ width: `${p.pct}%` }}
                    />
                  </div>
                  <span className="text-[10px] text-zinc-500 w-8 text-right">{p.pct}%</span>
                </div>
              ))}
            </div>
          </section>

          {task.summary?.content && (
            <section>
              <SectionLabel icon={<Sparkles className="w-3.5 h-3.5" />} text="AI 摘要" />
              <p className="mt-2 text-[13px] text-zinc-400 leading-relaxed line-clamp-6">{task.summary.content.replace(/[#*`]/g, '')}</p>
            </section>
          )}

          {task.transcription && (
            <section>
              <SectionLabel icon={<Film className="w-3.5 h-3.5" />} text="转写" />
              <p className="mt-2 text-[12px] text-zinc-500 font-mono leading-relaxed line-clamp-4">{task.transcription.content}</p>
            </section>
          )}
        </div>

        <div className="shrink-0 p-5 border-t border-zinc-800 space-y-2">
          {canChat ? (
            <Link
              href={`/chat/${task.id}`}
              className="w-full h-11 rounded-xl bg-violet-600 hover:bg-violet-500 text-white text-[14px] font-medium flex items-center justify-center gap-2 transition-colors"
            >
              <MessageCircle className="w-4 h-4" />开始问答
            </Link>
          ) : (
            <div className="text-[12px] text-zinc-500 text-center py-2">转写完成后可开始引用式问答</div>
          )}
          <Link href="/kb" className="w-full h-9 rounded-xl border border-zinc-700 text-zinc-400 text-[12px] flex items-center justify-center gap-1.5 hover:bg-zinc-900">
            <BookOpen className="w-3.5 h-3.5" />加入知识库
          </Link>
        </div>
      </aside>
    </>
  )
}

function SectionLabel({ icon, text }: { icon: React.ReactNode; text: string }) {
  return (
    <div className="flex items-center gap-2 text-[11px] uppercase tracking-wider text-zinc-500 font-medium">
      {icon}{text}
    </div>
  )
}

function EmptyState({ onUpload, hasTasks }: { onUpload: () => void; hasTasks: boolean }) {
  return (
    <div className="max-w-md mx-auto py-20 text-center">
      <div className="w-16 h-16 rounded-2xl bg-zinc-900 border border-zinc-800 flex items-center justify-center mx-auto mb-4">
        <Film className="w-7 h-7 text-zinc-600" />
      </div>
      <div className="text-[16px] font-medium text-zinc-300">{hasTasks ? '无匹配结果' : '还没有视频'}</div>
      <p className="text-[13px] text-zinc-500 mt-2">上传视频，自动转写并建立可检索的知识索引</p>
      {!hasTasks && (
        <button onClick={onUpload} className="mt-5 h-10 px-5 rounded-full bg-violet-600 text-white text-[13px] font-medium inline-flex items-center gap-2">
          <Upload className="w-4 h-4" />上传第一个视频
        </button>
      )}
    </div>
  )
}
