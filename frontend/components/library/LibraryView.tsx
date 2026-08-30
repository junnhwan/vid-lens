'use client'

import { useMemo, useState } from 'react'
import Link from 'next/link'
import {
  Search, ArrowRight, BarChart3, LayoutGrid, List, Upload,
  Sparkles, FileText, BookOpen,
} from 'lucide-react'
import type { VideoTask, TaskStatus } from '@/lib/types'
import { TaskStatusEnum } from '@/lib/types'
import {
  statusBadge, statusLabel, computePhases, fmtRelTime, fmtSize, sourceLabel,
  taskTitle, stripMdPreview,
} from '@/lib/format'
import AppShell, { PageHero, MiniStat } from '@/components/layout/AppShell'
import { VideoThumb } from '@/components/library/VideoMedia'

type ViewMode = 'list' | 'gallery'
type StatusFilter = 'all' | 'running' | 'completed' | 'failed' | 'dead'
type SourceFilter = 'all' | 'upload' | 'url'

interface Props {
  tasks: VideoTask[]
  loading: boolean
  error: string
  keyword: string
  onKeywordChange: (v: string) => void
  onKeywordSubmit: () => void
  statusFilter: StatusFilter
  onStatusFilter: (v: StatusFilter) => void
  sourceFilter: SourceFilter
  onSourceFilter: (v: SourceFilter) => void
  selectedId: number | null
  onSelect: (id: number | null) => void
  onRefresh: () => void
  onUpload: () => void
}

export default function LibraryView(props: Props) {
  const {
    tasks, loading, error, keyword, onKeywordChange, onKeywordSubmit,
    statusFilter, onStatusFilter, sourceFilter, onSourceFilter,
    selectedId, onSelect, onRefresh, onUpload,
  } = props

  const [sort, setSort] = useState<'newest' | 'title'>('newest')
  const [viewMode, setViewMode] = useState<ViewMode>('list')

  const isPending = (s: TaskStatus) => s === TaskStatusEnum.Pending || s === TaskStatusEnum.Queued || s === TaskStatusEnum.Running

  const filtered = useMemo(() => {
    let list = [...tasks]
    if (statusFilter === 'running') list = list.filter(t => isPending(t.status))
    else if (statusFilter === 'completed') list = list.filter(t => t.status === TaskStatusEnum.Completed)
    else if (statusFilter === 'failed') list = list.filter(t => t.status === TaskStatusEnum.Failed)
    else if (statusFilter === 'dead') list = list.filter(t => t.status === TaskStatusEnum.Dead)
    if (sourceFilter === 'upload') list = list.filter(t => t.source_type === 'upload' || t.source_type === 'chunked')
    else if (sourceFilter === 'url') list = list.filter(t => t.source_type === 'url')
    if (sort === 'newest') list.sort((a, b) => new Date(b.created_at).getTime() - new Date(a.created_at).getTime())
    else list.sort((a, b) => taskTitle(a).localeCompare(taskTitle(b), 'zh-CN'))
    return list
  }, [tasks, statusFilter, sourceFilter, sort])

  const readyStrip = useMemo(() =>
    [...tasks].filter(t => t.has_transcription)
      .sort((a, b) => new Date(b.updated_at).getTime() - new Date(a.updated_at).getTime())
      .slice(0, 4),
    [tasks],
  )

  const stats = {
    total: tasks.length,
    ready: tasks.filter(t => t.has_transcription).length,
    processing: tasks.filter(t => isPending(t.status)).length,
  }

  return (
    <AppShell onUpload={onUpload}>
      <PageHero
        kicker="视频库"
        title="视频知识库"
        desc="看起来像视频窗口，点进去是转写与摘要。管理用列表，研读用画廊。"
        actions={
          <div className="hidden lg:flex items-center gap-3 shrink-0 ui-fade-in">
            <Pill icon={<Sparkles className="w-3.5 h-3.5" />} text="ASR 转写" />
            <Pill icon={<FileText className="w-3.5 h-3.5" />} text="LLM 摘要" />
            <Pill icon={<BookOpen className="w-3.5 h-3.5" />} text="RAG 问答" />
          </div>
        }
      />

      <div className="px-8 pb-3 flex flex-wrap items-center gap-3">
        <div className="grid grid-cols-3 gap-2 w-44 shrink-0">
          <MiniStat value={stats.total} label="视频" />
          <MiniStat value={stats.ready} label="可问答" accent />
          <MiniStat value={stats.processing} label="处理中" warn={stats.processing > 0} />
        </div>

        <div className="relative flex-1 min-w-[180px] max-w-md">
          <Search className="absolute left-3.5 top-1/2 -translate-y-1/2 w-4 h-4 text-ink-4" />
          <input
            value={keyword}
            onChange={e => onKeywordChange(e.target.value)}
            onKeyDown={e => { if (e.key === 'Enter') onKeywordSubmit() }}
            placeholder="搜索视频标题…"
            className="w-full h-10 pl-10 pr-4 rounded-xl border border-ink-0/10 bg-paper-0 text-[14px] shadow-sm focus:outline-none focus:ring-2 focus:ring-sienna-500/20"
          />
        </div>

        <select value={statusFilter} onChange={e => onStatusFilter(e.target.value as StatusFilter)} className="h-10 px-2 rounded-lg border border-ink-0/10 bg-paper-0 text-[12px] text-ink-2">
          <option value="all">全部状态</option>
          <option value="running">处理中</option>
          <option value="completed">已完成</option>
          <option value="failed">失败</option>
          <option value="dead">已废弃</option>
        </select>
        <select value={sourceFilter} onChange={e => onSourceFilter(e.target.value as SourceFilter)} className="h-10 px-2 rounded-lg border border-ink-0/10 bg-paper-0 text-[12px] text-ink-2">
          <option value="all">全部来源</option>
          <option value="upload">本地上传</option>
          <option value="url">URL</option>
        </select>

        <div className="flex rounded-lg border border-ink-0/10 bg-paper-0 p-0.5 text-[12px]">
          <Toggle active={viewMode === 'list'} onClick={() => setViewMode('list')} icon={<List className="w-3.5 h-3.5" />} label="列表" />
          <Toggle active={viewMode === 'gallery'} onClick={() => setViewMode('gallery')} icon={<LayoutGrid className="w-3.5 h-3.5" />} label="画廊" />
        </div>

        <select value={sort} onChange={e => setSort(e.target.value as 'newest' | 'title')} className="h-10 px-3 rounded-lg border border-ink-0/10 bg-paper-0 text-[12px]">
          <option value="newest">最新</option>
          <option value="title">标题</option>
        </select>
        <button onClick={onRefresh} className="h-10 px-3 rounded-lg border border-ink-0/10 bg-paper-0 text-[12px] text-ink-3 hover:text-ink-1">刷新</button>
      </div>

      <main className="flex-1 overflow-y-auto px-8 pb-8">
        {error && <div className="py-3 px-4 mb-4 text-[13px] text-rust bg-red-50 border border-red-200 rounded-lg">{error}<button onClick={onRefresh} className="ml-2 underline">重试</button></div>}

        {!loading && readyStrip.length > 0 && viewMode === 'list' && (
          <section className="mb-6 ui-fade-in">
            <h2 className="text-[15px] font-medium text-ink-0 ui-serif">继续研读</h2>
            <p className="text-[11px] text-ink-4 mt-0.5 mb-3">点击封面查看转写与摘要</p>
            <div className="grid grid-cols-2 md:grid-cols-4 gap-3">
              {readyStrip.map(t => (
                <GalleryCard key={t.id} task={t} selected={selectedId === t.id} onClick={() => onSelect(t.id)} />
              ))}
            </div>
          </section>
        )}

        {loading ? (
          <div className="space-y-3">
            {viewMode === 'gallery'
              ? <div className="grid grid-cols-2 md:grid-cols-4 gap-4">{Array.from({ length: 8 }).map((_, i) => <div key={i} className="aspect-[16/10] rounded-xl bg-paper-2 animate-pulse" />)}</div>
              : Array.from({ length: 5 }).map((_, i) => <div key={i} className="h-14 bg-paper-2 rounded-lg animate-pulse" />)}
          </div>
        ) : filtered.length === 0 ? (
          <Empty onUpload={onUpload} hasTasks={tasks.length > 0} />
        ) : viewMode === 'gallery' ? (
          <div className="grid grid-cols-2 md:grid-cols-3 lg:grid-cols-4 gap-4">
            {filtered.map(t => (
              <GalleryCard key={t.id} task={t} selected={selectedId === t.id} onClick={() => onSelect(t.id)} />
            ))}
          </div>
        ) : (
          <div className="bg-paper-0 rounded-xl border border-ink-0/8 overflow-hidden shadow-sm">
            <div className="grid grid-cols-[64px_1fr_96px_120px_88px_64px] gap-2 px-5 py-3 border-b border-ink-0/6 bg-paper-1/80 text-[10px] uppercase tracking-wider text-ink-4 font-medium">
              <span /><span>标题</span><span>状态</span><span>阶段</span><span>来源</span><span className="text-right">操作</span>
            </div>
            {filtered.map((t, i) => (
              <TaskRow key={t.id} task={t} active={selectedId === t.id} onOpen={() => onSelect(t.id)} delay={i * 30} />
            ))}
          </div>
        )}
      </main>
    </AppShell>
  )
}

function GalleryCard({ task, selected, onClick }: { task: VideoTask; selected?: boolean; onClick: () => void }) {
  const phases = computePhases(task)
  const running = phases.some(p => p.state === 'running')
  return (
    <button
      onClick={onClick}
      className={`group text-left rounded-xl overflow-hidden border bg-paper-0 ui-card-hover transition-all ${
        selected ? 'border-sienna-500 ring-2 ring-sienna-500/25 shadow-md' : 'border-ink-0/8'
      }`}
    >
      <VideoThumb taskId={task.id} className="aspect-[16/10]" showPlay showStatus status={task.status} running={running} />
      <div className="p-3">
        <div className="text-[13px] font-medium text-ink-0 truncate">{taskTitle(task)}</div>
        {task.summary?.content && (
          <p className="text-[11px] text-ink-3 mt-1 line-clamp-2">{stripMdPreview(task.summary.content, 80)}</p>
        )}
        <div className="flex gap-1 mt-2">
          {phases.map(p => (
            <span key={p.label} className={`text-[9px] px-1.5 py-0.5 rounded ${
              p.state === 'done' ? 'bg-moss/10 text-moss' : p.state === 'running' ? 'bg-sienna-500/10 text-sienna-700' : 'bg-paper-2 text-ink-4'
            }`}>{p.label}</span>
          ))}
        </div>
      </div>
    </button>
  )
}

function TaskRow({ task, active, onOpen, delay }: { task: VideoTask; active: boolean; onOpen: () => void; delay: number }) {
  const badge = statusBadge(task.status)
  const phases = computePhases(task)
  return (
    <div className={`border-b border-ink-0/6 last:border-0 ui-fade-in ${active ? 'bg-sienna-500/6' : ''}`} style={{ animationDelay: `${delay}ms` }}>
      <div role="button" tabIndex={0} onClick={onOpen} onKeyDown={e => { if (e.key === 'Enter') onOpen() }}
        className="grid grid-cols-[64px_1fr_96px_120px_88px_64px] gap-2 px-5 py-3 items-center cursor-pointer ui-row-hover">
        <button onClick={e => { e.stopPropagation(); onOpen() }} className="w-16 h-10 rounded-md overflow-hidden ui-card-hover hover:ring-2 hover:ring-sienna-500/40">
          <VideoThumb taskId={task.id} className="w-full h-full" />
        </button>
        <div className="min-w-0">
          <div className="text-[14px] font-medium truncate">{taskTitle(task)}</div>
          <div className="text-[11px] text-ink-4">{fmtSize(task.file_size)} · {fmtRelTime(task.created_at)}</div>
        </div>
        <span className={`inline-flex items-center gap-1 px-2 py-0.5 rounded text-[11px] w-fit ${badge.cls}`}>
          {badge.live && <span className="w-1.5 h-1.5 rounded-full bg-sienna-500 ui-pulse" />}
          {statusLabel(task.status)}
        </span>
        <div className="flex gap-1">
          {phases.map(p => (
            <span key={p.label} className={`h-1.5 flex-1 rounded-full ${p.state === 'done' ? 'bg-moss' : p.state === 'running' ? 'bg-sienna-500' : 'bg-paper-3'}`} />
          ))}
        </div>
        <span className="text-[12px] text-ink-3 truncate">{sourceLabel(task)}</span>
        <div className="text-right" onClick={e => e.stopPropagation()}>
          {task.has_transcription ? (
            <Link href={`/chat/${task.id}`} className="text-[11px] text-sienna-700 font-medium inline-flex items-center gap-0.5">问答<ArrowRight className="w-3 h-3" /></Link>
          ) : <span className="text-ink-4 text-[10px]">—</span>}
        </div>
      </div>
    </div>
  )
}

function Pill({ icon, text }: { icon: React.ReactNode; text: string }) {
  return <div className="flex items-center gap-2 px-3 py-2 rounded-full bg-paper-0 border border-ink-0/8 text-[11px] text-ink-3 ui-card-hover">{icon}{text}</div>
}

function Toggle({ active, onClick, icon, label }: { active: boolean; onClick: () => void; icon: React.ReactNode; label: string }) {
  return (
    <button onClick={onClick} className={`flex items-center gap-1.5 px-3 py-1.5 rounded-md ${active ? 'bg-ink-0 text-paper-0' : 'text-ink-3'}`}>
      {icon}{label}
    </button>
  )
}

function Empty({ onUpload, hasTasks }: { onUpload: () => void; hasTasks: boolean }) {
  return (
    <div className="py-16 text-center">
      <BarChart3 className="w-8 h-8 text-ink-5 mx-auto mb-3" />
      <p className="text-[14px] text-ink-3">{hasTasks ? '无匹配结果' : '上传第一个视频开始'}</p>
      {!hasTasks && (
        <button onClick={onUpload} className="mt-4 h-9 px-4 rounded-lg bg-ink-0 text-paper-0 text-[12px] inline-flex items-center gap-1.5">
          <Upload className="w-3.5 h-3.5" />上传视频
        </button>
      )}
    </div>
  )
}
