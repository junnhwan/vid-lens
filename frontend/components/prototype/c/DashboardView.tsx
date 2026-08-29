'use client'

// 原型：视频库工作台（侧栏导航、表格列表、顶部画廊条、渐变封面、居中详情弹窗）
import { useMemo, useState } from 'react'
import Link from 'next/link'
import {
  Search, MessageCircle, BookOpen, Upload, Sparkles, FileText,
  ArrowRight, BarChart3, LayoutGrid, List,
} from 'lucide-react'
import { TaskStatusEnum, type VideoTask } from '@/lib/types'
import { statusBadge, statusLabel, computePhases, fmtRelTime, fmtSize, sourceLabel } from '@/lib/format'
import { ProtoShell, PageHero, MiniStat } from '@/components/prototype/c/Shell'
import { taskTitle } from '@/components/prototype/c/mocks'
import { VideoDetailModal, GalleryCard, TableThumb } from '@/components/prototype/c/VideoDrawer'

type ViewMode = 'list' | 'gallery'

interface Props {
  tasks: VideoTask[]
  loading: boolean
  error: string
  onRefresh: () => void
  onUpload: () => void
}

export default function DashboardView({ tasks, loading, error, onRefresh, onUpload }: Props) {
  const [query, setQuery] = useState('')
  const [sort, setSort] = useState<'newest' | 'title'>('newest')
  const [viewMode, setViewMode] = useState<ViewMode>('list')
  const [drawerId, setDrawerId] = useState<number | null>(null)

  const filtered = useMemo(() => {
    let list = [...tasks]
    const q = query.trim().toLowerCase()
    if (q) list = list.filter(t => taskTitle(t).toLowerCase().includes(q))
    if (sort === 'newest') list.sort((a, b) => new Date(b.created_at).getTime() - new Date(a.created_at).getTime())
    else list.sort((a, b) => taskTitle(a).localeCompare(taskTitle(b), 'zh-CN'))
    return list
  }, [tasks, query, sort])

  // 「继续研读」：已可问答的视频，最多 4 个
  const readyStrip = useMemo(() =>
    [...tasks]
      .filter(t => t.has_transcription)
      .sort((a, b) => new Date(b.updated_at).getTime() - new Date(a.updated_at).getTime())
      .slice(0, 4),
    [tasks],
  )

  const drawerTask = drawerId != null ? tasks.find(t => t.id === drawerId) ?? null : null

  const stats = {
    total: tasks.length,
    ready: tasks.filter(t => t.has_transcription).length,
    processing: tasks.filter(t => t.status === TaskStatusEnum.Running || t.status === TaskStatusEnum.Queued).length,
  }

  const openDrawer = (id: number) => setDrawerId(id)

  return (
    <ProtoShell active="dashboard" onUpload={onUpload}>
      <PageHero
        kicker="视频库 · 原型预览"
        title="视频知识库"
        desc="看起来像视频窗口，点进去是转写与摘要——管理用列表，研读用画廊。"
        actions={
          <div className="hidden lg:flex items-center gap-3 shrink-0 proto-fade-in">
            <HeroPill icon={<Sparkles className="w-3.5 h-3.5" />} text="ASR 转写" />
            <HeroPill icon={<FileText className="w-3.5 h-3.5" />} text="LLM 摘要" />
            <HeroPill icon={<BookOpen className="w-3.5 h-3.5" />} text="RAG 问答" />
          </div>
        }
      />

      {/* 工具栏 */}
      <div className="px-8 pb-3 flex flex-wrap items-center gap-3">
        <div className="grid grid-cols-3 gap-2 w-44 shrink-0">
          <MiniStat value={stats.total} label="视频" />
          <MiniStat value={stats.ready} label="可问答" accent />
          <MiniStat value={stats.processing} label="处理中" warn={stats.processing > 0} />
        </div>

        <div className="relative flex-1 min-w-[200px] max-w-md">
          <Search className="absolute left-3.5 top-1/2 -translate-y-1/2 w-4 h-4 text-stone-400" />
          <input
            value={query}
            onChange={e => setQuery(e.target.value)}
            placeholder="搜索视频标题…"
            className="w-full h-10 pl-10 pr-4 rounded-xl border border-stone-200 bg-white text-[14px] shadow-sm focus:outline-none focus:ring-2 focus:ring-amber-600/20 focus:border-amber-500/40 transition-all duration-200"
          />
        </div>

        <div className="flex rounded-lg border border-stone-200 bg-white p-0.5 text-[12px]">
          <ViewToggle active={viewMode === 'list'} onClick={() => setViewMode('list')} icon={<List className="w-3.5 h-3.5" />} label="列表" />
          <ViewToggle active={viewMode === 'gallery'} onClick={() => setViewMode('gallery')} icon={<LayoutGrid className="w-3.5 h-3.5" />} label="画廊" />
        </div>

        <select
          value={sort}
          onChange={e => setSort(e.target.value as 'newest' | 'title')}
          className="h-10 px-3 rounded-lg border border-stone-200 bg-white text-[12px] text-stone-600"
        >
          <option value="newest">最新优先</option>
          <option value="title">按标题</option>
        </select>
        <button onClick={onRefresh} className="h-10 px-3 rounded-lg border border-stone-200 bg-white text-[12px] text-stone-500 hover:text-stone-800 proto-btn-lift">
          刷新
        </button>
      </div>

      <main className="flex-1 overflow-y-auto px-8 pb-24">
        {error && (
          <div className="py-3 px-4 mb-4 text-[13px] text-amber-800 bg-amber-50 border border-amber-200 rounded-lg proto-fade-in">{error}</div>
        )}

        {/* A 式画廊条：可问答视频 */}
        {!loading && readyStrip.length > 0 && viewMode === 'list' && (
          <section className="mb-6 proto-fade-in">
            <div className="flex items-center justify-between mb-3">
              <div>
                <h2 className="text-[15px] font-medium text-stone-900 proto-serif">继续研读</h2>
                <p className="text-[11px] text-stone-500 mt-0.5">点击封面查看转写与摘要，不是播放视频</p>
              </div>
            </div>
            <div className="grid grid-cols-2 md:grid-cols-4 gap-3">
              {readyStrip.map((t, i) => (
                <div key={t.id} className="proto-fade-in" style={{ animationDelay: `${i * 50}ms` }}>
                  <GalleryCard task={t} selected={drawerId === t.id} onClick={() => openDrawer(t.id)} />
                </div>
              ))}
            </div>
          </section>
        )}

        {loading ? (
          <div className="space-y-3">
            {viewMode === 'gallery'
              ? <div className="grid grid-cols-2 md:grid-cols-3 lg:grid-cols-4 gap-4">{Array.from({ length: 8 }).map((_, i) => <div key={i} className="aspect-[4/3] rounded-xl bg-stone-200 animate-pulse" />)}</div>
              : Array.from({ length: 5 }).map((_, i) => <div key={i} className="h-14 bg-stone-100 rounded-lg animate-pulse" />)
            }
          </div>
        ) : filtered.length === 0 ? (
          <EmptyState onUpload={onUpload} hasTasks={tasks.length > 0} />
        ) : viewMode === 'gallery' ? (
          <GalleryGrid tasks={filtered} drawerId={drawerId} onOpen={openDrawer} />
        ) : (
          <ListTable tasks={filtered} drawerId={drawerId} onOpen={openDrawer} />
        )}
      </main>

      {drawerTask && <VideoDetailModal task={drawerTask} onClose={() => setDrawerId(null)} />}
    </ProtoShell>
  )
}

function ViewToggle({ active, onClick, icon, label }: { active: boolean; onClick: () => void; icon: React.ReactNode; label: string }) {
  return (
    <button
      onClick={onClick}
      className={`flex items-center gap-1.5 px-3 py-1.5 rounded-md transition-all duration-200 ${
        active ? 'bg-stone-900 text-white shadow-sm' : 'text-stone-500 hover:text-stone-700'
      }`}
    >
      {icon}{label}
    </button>
  )
}

function HeroPill({ icon, text }: { icon: React.ReactNode; text: string }) {
  return (
    <div className="flex items-center gap-2 px-3 py-2 rounded-full bg-white border border-stone-200 text-[11px] text-stone-600 shadow-sm proto-card-hover">
      {icon}{text}
    </div>
  )
}

function GalleryGrid({ tasks, drawerId, onOpen }: { tasks: VideoTask[]; drawerId: number | null; onOpen: (id: number) => void }) {
  return (
    <div className="grid grid-cols-2 md:grid-cols-3 lg:grid-cols-4 gap-4 proto-fade-in">
      {tasks.map((t, i) => (
        <div key={t.id} className="proto-fade-in" style={{ animationDelay: `${i * 30}ms` }}>
          <GalleryCard task={t} selected={drawerId === t.id} onClick={() => onOpen(t.id)} />
        </div>
      ))}
    </div>
  )
}

function ListTable({ tasks, drawerId, onOpen }: { tasks: VideoTask[]; drawerId: number | null; onOpen: (id: number) => void }) {
  return (
    <div className="bg-white rounded-xl border border-stone-200 overflow-hidden shadow-sm proto-fade-in">
      <div className="grid grid-cols-[64px_1fr_96px_130px_100px_72px] gap-3 px-5 py-3 border-b border-stone-100 bg-stone-50/80 text-[10px] uppercase tracking-wider text-stone-400 font-medium">
        <span />
        <span>标题</span>
        <span>状态</span>
        <span>处理阶段</span>
        <span>来源</span>
        <span className="text-right">操作</span>
      </div>
      {tasks.map((t, i) => (
        <TaskRow key={t.id} task={t} index={i} active={drawerId === t.id} onOpen={() => onOpen(t.id)} onThumbClick={() => onOpen(t.id)} />
      ))}
    </div>
  )
}

function TaskRow({ task, index, active, onOpen, onThumbClick }: {
  task: VideoTask; index: number; active: boolean; onOpen: () => void; onThumbClick: () => void
}) {
  const badge = statusBadge(task.status)
  const phases = computePhases(task)

  return (
    <div
      className={`border-b border-stone-100 last:border-0 proto-fade-in ${active ? 'bg-amber-50/40' : ''}`}
      style={{ animationDelay: `${index * 30}ms` }}
    >
      <div
        role="button"
        tabIndex={0}
        onClick={onOpen}
        onKeyDown={e => { if (e.key === 'Enter') onOpen() }}
        className="w-full grid grid-cols-[64px_1fr_96px_130px_100px_72px] gap-3 px-5 py-3 text-left items-center proto-row-hover cursor-pointer"
      >
        <div onClick={e => e.stopPropagation()}>
          <TableThumb taskId={task.id} onClick={e => { e.stopPropagation(); onThumbClick() }} />
        </div>
        <div className="min-w-0">
          <div className="text-[14px] font-medium text-stone-900 truncate">{taskTitle(task)}</div>
          <div className="text-[11px] text-stone-400 mt-0.5">{fmtSize(task.file_size)} · {fmtRelTime(task.created_at)}</div>
        </div>
        <div>
          <span className={`inline-flex items-center gap-1 px-2 py-0.5 rounded text-[11px] ${badge.cls}`}>
            {badge.live && <span className="w-1.5 h-1.5 rounded-full bg-amber-500 proto-pulse" />}
            {statusLabel(task.status)}
          </span>
        </div>
        <div className="flex items-center gap-1">
          {phases.map(p => (
            <span
              key={p.label}
              title={`${p.label} ${p.pct}%`}
              className={`h-1.5 flex-1 rounded-full ${
                p.state === 'done' ? 'bg-emerald-500' : p.state === 'running' ? 'bg-amber-500' : 'bg-stone-200'
              }`}
            />
          ))}
        </div>
        <div className="text-[12px] text-stone-500 truncate">{sourceLabel(task)}</div>
        <div className="text-right" onClick={e => e.stopPropagation()}>
          {task.has_transcription ? (
            <Link href={`/prototype/chat/${task.id}`} className="inline-flex items-center gap-0.5 text-[11px] text-amber-800 font-medium proto-btn-lift">
              问答<ArrowRight className="w-3 h-3" />
            </Link>
          ) : (
            <span className="text-[10px] text-stone-400">—</span>
          )}
        </div>
      </div>
    </div>
  )
}

function EmptyState({ onUpload, hasTasks }: { onUpload: () => void; hasTasks: boolean }) {
  return (
    <div className="py-16 text-center proto-fade-in">
      <BarChart3 className="w-8 h-8 text-stone-300 mx-auto mb-3" />
      <div className="text-[14px] text-stone-500">{hasTasks ? '无匹配结果' : '上传第一个视频开始'}</div>
      {!hasTasks && (
        <button onClick={onUpload} className="mt-4 h-9 px-4 rounded-lg bg-stone-900 text-white text-[12px] inline-flex items-center gap-1.5 proto-btn-lift">
          <Upload className="w-3.5 h-3.5" />上传视频
        </button>
      )}
    </div>
  )
}
