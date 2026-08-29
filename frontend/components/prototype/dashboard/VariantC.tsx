'use client'

// Variant C — 研读工作台：左侧富信息导航 + 右侧表格行 + 手风琴展开详情
import Link from 'next/link'
import { useMemo, useState } from 'react'
import {
  Upload, MessageCircle, ChevronDown, BookOpen, Settings, Library,
  Video, BarChart3, Sparkles, FileText, ArrowRight, Search,
} from 'lucide-react'
import type { VideoTask } from '@/lib/types'
import { TaskStatusEnum } from '@/lib/types'
import { statusBadge, statusLabel, computePhases, fmtRelTime, fmtSize, sourceLabel } from '@/lib/format'
import type { DashboardPrototypeProps } from './types'
import { taskTitle } from './types'

export const displayName = '研读工作台'

export default function VariantC({
  tasks, loading, error, onRefresh, onUpload, selectedId, onSelect,
}: DashboardPrototypeProps) {
  const [query, setQuery] = useState('')
  const [sort, setSort] = useState<'newest' | 'title'>('newest')

  const filtered = useMemo(() => {
    let list = [...tasks]
    const q = query.trim().toLowerCase()
    if (q) list = list.filter(t => taskTitle(t).toLowerCase().includes(q))
    if (sort === 'newest') list.sort((a, b) => new Date(b.created_at).getTime() - new Date(a.created_at).getTime())
    else list.sort((a, b) => taskTitle(a).localeCompare(taskTitle(b), 'zh-CN'))
    return list
  }, [tasks, query, sort])

  const stats = {
    total: tasks.length,
    ready: tasks.filter(t => t.has_transcription).length,
    processing: tasks.filter(t =>
      t.status === TaskStatusEnum.Running || t.status === TaskStatusEnum.Queued
    ).length,
  }

  return (
    <div className="h-screen flex bg-[#f7f4ef] text-stone-800 overflow-hidden">
      {/* 左侧富导航 */}
      <aside className="w-[260px] shrink-0 bg-[#faf8f5] border-r border-stone-200 flex flex-col">
        <div className="p-6 border-b border-stone-200">
          <div className="text-[22px] font-semibold tracking-tight text-stone-900" style={{ fontFamily: 'Georgia, "Noto Serif SC", serif' }}>
            映知
          </div>
          <p className="text-[12px] text-stone-500 mt-1 italic leading-relaxed">观之以映，释之以知</p>
        </div>

        <nav className="p-4 space-y-1">
          <NavItem active icon={<Video className="w-4 h-4" />} label="我的视频" desc="管理与处理" />
          <NavItem icon={<Library className="w-4 h-4" />} label="知识库" desc="跨视频问答" href="/kb" />
          <NavItem icon={<Settings className="w-4 h-4" />} label="AI 配置" desc="BYOK 密钥" href="/settings" />
        </nav>

        <div className="px-4 py-3">
          <div className="text-[10px] uppercase tracking-widest text-stone-400 mb-2 font-medium">概览</div>
          <div className="grid grid-cols-3 gap-2">
            <MiniStat value={stats.total} label="视频" />
            <MiniStat value={stats.ready} label="可问答" accent />
            <MiniStat value={stats.processing} label="处理中" warn={stats.processing > 0} />
          </div>
        </div>

        <div className="flex-1 px-4 py-3 overflow-y-auto">
          <div className="text-[10px] uppercase tracking-widest text-stone-400 mb-3 font-medium">最近动态</div>
          <div className="space-y-3">
            {tasks.slice(0, 5).map(t => (
              <button
                key={t.id}
                onClick={() => onSelect(t.id)}
                className="w-full text-left group"
              >
                <div className="text-[12px] text-stone-700 truncate group-hover:text-amber-800 transition-colors">{taskTitle(t)}</div>
                <div className="text-[10px] text-stone-400 mt-0.5">{statusLabel(t.status)} · {fmtRelTime(t.created_at)}</div>
              </button>
            ))}
            {tasks.length === 0 && <div className="text-[11px] text-stone-400 italic">暂无动态</div>}
          </div>
        </div>

        <div className="p-4 border-t border-stone-200">
          <button
            onClick={onUpload}
            className="w-full h-10 rounded-lg bg-stone-900 text-[#faf8f5] text-[13px] font-medium flex items-center justify-center gap-2 hover:bg-stone-800 transition-colors"
          >
            <Upload className="w-4 h-4" />上传视频
          </button>
        </div>
      </aside>

      {/* 右侧主区 */}
      <div className="flex-1 flex flex-col min-w-0">
        {/* Hero 区 */}
        <header className="shrink-0 px-8 pt-8 pb-6 bg-gradient-to-b from-[#faf8f5] to-transparent">
          <div className="flex items-start justify-between gap-6">
            <div>
              <div className="text-[11px] text-stone-500 uppercase tracking-wider mb-1">工作台</div>
              <h1 className="text-[32px] font-semibold text-stone-900 leading-tight" style={{ fontFamily: 'Georgia, "Noto Serif SC", serif' }}>
                视频知识库
              </h1>
              <p className="text-[14px] text-stone-500 mt-2 max-w-lg leading-relaxed">
                将长视频转写为可检索文本，建立引用式问答索引。每个回答都可追溯到原始片段。
              </p>
            </div>
            <div className="hidden lg:flex items-center gap-3 shrink-0">
              <HeroPill icon={<Sparkles className="w-3.5 h-3.5" />} text="ASR 转写" />
              <HeroPill icon={<FileText className="w-3.5 h-3.5" />} text="LLM 摘要" />
              <HeroPill icon={<BookOpen className="w-3.5 h-3.5" />} text="RAG 问答" />
            </div>
          </div>
        </header>

        {/* 工具栏 */}
        <div className="shrink-0 px-8 pb-4 flex items-center gap-3">
          <div className="relative flex-1 max-w-sm">
            <Search className="absolute left-3 top-1/2 -translate-y-1/2 w-3.5 h-3.5 text-stone-400" />
            <input
              value={query}
              onChange={e => setQuery(e.target.value)}
              placeholder="筛选标题…"
              className="w-full h-9 pl-9 pr-3 rounded-lg border border-stone-200 bg-white text-[13px] focus:outline-none focus:ring-2 focus:ring-amber-600/20 focus:border-amber-600/40"
            />
          </div>
          <select
            value={sort}
            onChange={e => setSort(e.target.value as 'newest' | 'title')}
            className="h-9 px-3 rounded-lg border border-stone-200 bg-white text-[12px] text-stone-600"
          >
            <option value="newest">最新优先</option>
            <option value="title">按标题</option>
          </select>
          <button onClick={onRefresh} className="h-9 px-3 rounded-lg border border-stone-200 bg-white text-[12px] text-stone-500 hover:text-stone-800">
            刷新
          </button>
        </div>

        {/* 表格列表 */}
        <main className="flex-1 min-h-0 overflow-y-auto px-8 pb-8">
          {error && (
            <div className="py-6 text-[13px] text-red-700 bg-red-50 border border-red-200 rounded-lg px-4 mb-4">{error}</div>
          )}

          <div className="bg-white rounded-xl border border-stone-200 overflow-hidden shadow-sm">
            {/* 表头 */}
            <div className="grid grid-cols-[1fr_100px_140px_120px_80px] gap-3 px-5 py-3 border-b border-stone-100 bg-stone-50/80 text-[10px] uppercase tracking-wider text-stone-400 font-medium">
              <span>标题</span>
              <span>状态</span>
              <span>处理阶段</span>
              <span>来源</span>
              <span className="text-right">操作</span>
            </div>

            {loading ? (
              Array.from({ length: 6 }).map((_, i) => (
                <div key={i} className="px-5 py-4 border-b border-stone-50">
                  <div className="h-4 w-2/3 bg-stone-100 rounded animate-pulse" />
                </div>
              ))
            ) : filtered.length === 0 ? (
              <div className="py-16 text-center">
                <BarChart3 className="w-8 h-8 text-stone-300 mx-auto mb-3" />
                <div className="text-[14px] text-stone-500">{tasks.length === 0 ? '上传第一个视频开始' : '无匹配结果'}</div>
                {tasks.length === 0 && (
                  <button onClick={onUpload} className="mt-4 h-9 px-4 rounded-lg bg-stone-900 text-white text-[12px] inline-flex items-center gap-1.5">
                    <Upload className="w-3.5 h-3.5" />上传视频
                  </button>
                )}
              </div>
            ) : (
              filtered.map(t => (
                <TaskRow
                  key={t.id}
                  task={t}
                  expanded={selectedId === t.id}
                  onToggle={() => onSelect(selectedId === t.id ? null : t.id)}
                />
              ))
            )}
          </div>
        </main>
      </div>
    </div>
  )
}

function NavItem({ icon, label, desc, active, href }: {
  icon: React.ReactNode; label: string; desc: string; active?: boolean; href?: string
}) {
  const inner = (
    <>
      <div className={`w-8 h-8 rounded-lg flex items-center justify-center ${active ? 'bg-amber-100 text-amber-800' : 'bg-stone-100 text-stone-500'}`}>
        {icon}
      </div>
      <div>
        <div className={`text-[13px] ${active ? 'font-medium text-stone-900' : 'text-stone-700'}`}>{label}</div>
        <div className="text-[10px] text-stone-400">{desc}</div>
      </div>
    </>
  )
  const cls = `w-full flex items-center gap-3 px-3 py-2.5 rounded-lg transition-colors ${
    active ? 'bg-amber-50/80' : 'hover:bg-stone-100/80'
  }`
  if (href) return <Link href={href} className={cls}>{inner}</Link>
  return <button className={cls}>{inner}</button>
}

function MiniStat({ value, label, accent, warn }: { value: number; label: string; accent?: boolean; warn?: boolean }) {
  return (
    <div className="p-2 rounded-lg bg-white border border-stone-200 text-center">
      <div className={`text-[18px] font-semibold tabular-nums ${
        warn ? 'text-amber-600' : accent ? 'text-emerald-700' : 'text-stone-800'
      }`}>{value}</div>
      <div className="text-[9px] text-stone-400 mt-0.5">{label}</div>
    </div>
  )
}

function HeroPill({ icon, text }: { icon: React.ReactNode; text: string }) {
  return (
    <div className="flex items-center gap-2 px-3 py-2 rounded-full bg-white border border-stone-200 text-[11px] text-stone-600 shadow-sm">
      {icon}{text}
    </div>
  )
}

function TaskRow({ task, expanded, onToggle }: { task: VideoTask; expanded: boolean; onToggle: () => void }) {
  const badge = statusBadge(task.status)
  const phases = computePhases(task)

  return (
    <div className="border-b border-stone-100 last:border-0">
      <button
        onClick={onToggle}
        className={`w-full grid grid-cols-[1fr_100px_140px_120px_80px] gap-3 px-5 py-3.5 text-left items-center transition-colors ${
          expanded ? 'bg-amber-50/50' : 'hover:bg-stone-50/80'
        }`}
      >
        <div className="min-w-0 flex items-center gap-2">
          <ChevronDown className={`w-4 h-4 text-stone-400 shrink-0 transition-transform ${expanded ? 'rotate-180' : ''}`} />
          <div className="min-w-0">
            <div className="text-[14px] font-medium text-stone-900 truncate">{taskTitle(task)}</div>
            <div className="text-[11px] text-stone-400 mt-0.5">{fmtSize(task.file_size)} · {fmtRelTime(task.created_at)}</div>
          </div>
        </div>
        <div>
          <span className={`inline-flex items-center gap-1 px-2 py-0.5 rounded text-[11px] ${badge.cls}`}>
            {badge.live && <span className="w-1.5 h-1.5 rounded-full bg-amber-500 animate-pulse" />}
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
        <div className="text-[12px] text-stone-500">{sourceLabel(task)}</div>
        <div className="text-right" onClick={e => e.stopPropagation()}>
          {task.has_transcription && (
            <Link
              href={`/chat/${task.id}`}
              className="inline-flex items-center gap-1 text-[11px] text-amber-800 hover:text-amber-900 font-medium"
            >
              问答<ArrowRight className="w-3 h-3" />
            </Link>
          )}
        </div>
      </button>

      {/* 手风琴展开 */}
      {expanded && (
        <div className="px-5 pb-5 pt-1 bg-amber-50/30 border-t border-amber-100/50">
          <div className="ml-6 grid md:grid-cols-2 gap-4">
            <div>
              <div className="text-[10px] uppercase tracking-wider text-stone-400 mb-2">处理详情</div>
              <dl className="space-y-1.5 text-[12px]">
                <div className="flex justify-between"><dt className="text-stone-400">任务 ID</dt><dd className="text-stone-700 font-mono">#{task.id}</dd></div>
                <div className="flex justify-between"><dt className="text-stone-400">转写</dt><dd className="text-stone-700">{task.has_transcription ? `${task.transcription?.words ?? '—'} 词` : '未完成'}</dd></div>
                <div className="flex justify-between"><dt className="text-stone-400">摘要</dt><dd className="text-stone-700">{task.has_summary ? '已生成' : '未生成'}</dd></div>
              </dl>
            </div>
            {task.summary?.content && (
              <div>
                <div className="text-[10px] uppercase tracking-wider text-stone-400 mb-2">摘要预览</div>
                <p className="text-[12px] text-stone-600 leading-relaxed line-clamp-4">{task.summary.content.replace(/[#*`]/g, '')}</p>
              </div>
            )}
            {task.transcription?.content && (
              <div className="md:col-span-2">
                <div className="text-[10px] uppercase tracking-wider text-stone-400 mb-2">转写片段</div>
                <p className="text-[12px] text-stone-500 leading-relaxed line-clamp-3 font-mono bg-white rounded-lg p-3 border border-stone-200">
                  {task.transcription.content.slice(0, 400)}…
                </p>
              </div>
            )}
          </div>
          <div className="ml-6 mt-4 flex gap-2">
            {task.has_transcription && (
              <Link href={`/chat/${task.id}`} className="h-8 px-4 rounded-lg bg-stone-900 text-white text-[12px] inline-flex items-center gap-1.5">
                <MessageCircle className="w-3.5 h-3.5" />开始引用式问答
              </Link>
            )}
            <Link href="/kb" className="h-8 px-4 rounded-lg border border-stone-300 text-stone-600 text-[12px] inline-flex items-center gap-1.5 hover:bg-white">
              <BookOpen className="w-3.5 h-3.5" />加入知识库
            </Link>
          </div>
        </div>
      )}
    </div>
  )
}
