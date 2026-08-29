'use client'

// 探索方案 B — 流水线看板：左侧图标轨 + 横向 Kanban 列（待处理/处理中/已完成/异常）
import Link from 'next/link'
import { useState } from 'react'
import {
  LayoutGrid, Library, Settings, Upload, MessageCircle, RotateCw,
  Film, AlertTriangle, CheckCircle2, Loader2, X, ChevronUp,
} from 'lucide-react'
import type { VideoTask } from '@/lib/types'
import { TaskStatusEnum } from '@/lib/types'
import { statusLabel, computePhases, fmtRelTime, fmtSize, sourceLabel } from '@/lib/format'
import type { DashboardPrototypeProps } from './types'
import { taskTitle } from './types'

export const displayName = '流水线看板'

type ColumnKey = 'pending' | 'running' | 'done' | 'error'

const COLUMNS: { key: ColumnKey; label: string; icon: React.ReactNode; accent: string }[] = [
  { key: 'pending', label: '待处理', icon: <Film className="w-3.5 h-3.5" />, accent: 'border-t-zinc-400' },
  { key: 'running', label: '处理中', icon: <Loader2 className="w-3.5 h-3.5" />, accent: 'border-t-cyan-400' },
  { key: 'done', label: '已完成', icon: <CheckCircle2 className="w-3.5 h-3.5" />, accent: 'border-t-emerald-400' },
  { key: 'error', label: '异常', icon: <AlertTriangle className="w-3.5 h-3.5" />, accent: 'border-t-rose-400' },
]

function columnOf(t: VideoTask): ColumnKey {
  if (t.status === TaskStatusEnum.Completed) return 'done'
  if (t.status === TaskStatusEnum.Failed || t.status === TaskStatusEnum.Dead) return 'error'
  if (t.status === TaskStatusEnum.Running || t.status === TaskStatusEnum.Queued) return 'running'
  return 'pending'
}

export default function VariantB({
  tasks, loading, error, onRefresh, onUpload, selectedId, onSelect,
}: DashboardPrototypeProps) {
  const selected = tasks.find(t => t.id === selectedId) ?? null
  const grouped = COLUMNS.map(col => ({
    ...col,
    items: tasks.filter(t => columnOf(t) === col.key),
  }))

  const stats = {
    total: tasks.length,
    running: grouped.find(c => c.key === 'running')?.items.length ?? 0,
    indexed: tasks.filter(t => t.has_transcription).length,
  }

  return (
    <div className="h-screen flex bg-[#0a0e14] text-slate-200 overflow-hidden font-mono">
      {/* 极窄图标轨 */}
      <aside className="w-14 shrink-0 border-r border-slate-800 flex flex-col items-center py-4 gap-2 bg-[#060a0f]">
        <div className="w-8 h-8 rounded bg-cyan-500/20 border border-cyan-500/40 flex items-center justify-center text-cyan-400 text-[11px] font-bold mb-4">
          VL
        </div>
        <IconRailBtn active icon={<LayoutGrid className="w-4 h-4" />} label="工作台" />
        <IconRailBtn icon={<Library className="w-4 h-4" />} label="知识库" href="/kb" />
        <IconRailBtn icon={<Settings className="w-4 h-4" />} label="设置" href="/settings" />
        <div className="flex-1" />
        <button
          onClick={onUpload}
          className="w-9 h-9 rounded-lg bg-cyan-600 hover:bg-cyan-500 text-white flex items-center justify-center transition-colors"
          title="上传视频"
        >
          <Upload className="w-4 h-4" />
        </button>
      </aside>

      <div className="flex-1 flex flex-col min-w-0">
        {/* 顶栏统计 */}
        <header className="shrink-0 border-b border-slate-800 px-5 py-3 flex items-center justify-between gap-4">
          <div>
            <div className="text-[10px] text-slate-500 uppercase tracking-widest">PIPELINE / WORKSPACE</div>
            <h1 className="text-[20px] font-semibold text-slate-100 tracking-tight mt-0.5">视频处理流水线</h1>
          </div>
          <div className="flex items-center gap-6 text-[11px]">
            <Stat label="TOTAL" value={stats.total} />
            <Stat label="RUNNING" value={stats.running} highlight={stats.running > 0} />
            <Stat label="INDEXED" value={stats.indexed} />
            <button onClick={onRefresh} className="h-7 px-2.5 border border-slate-700 rounded text-slate-400 hover:text-cyan-400 hover:border-cyan-600/50 flex items-center gap-1 transition-colors">
              <RotateCw className="w-3 h-3" />SYNC
            </button>
          </div>
        </header>

        {error && (
          <div className="shrink-0 px-5 py-2 bg-rose-950/50 border-b border-rose-900/50 text-rose-400 text-[12px]">
            ERR: {error}
          </div>
        )}

        {/* Kanban 列 */}
        <main className="flex-1 min-h-0 flex gap-3 p-4 overflow-x-auto">
          {loading ? (
            COLUMNS.map(col => (
              <div key={col.key} className="w-72 shrink-0 rounded-lg border border-slate-800 bg-slate-900/30 p-3 space-y-2">
                <div className="h-4 w-20 bg-slate-800 rounded animate-pulse" />
                {Array.from({ length: 3 }).map((_, i) => (
                  <div key={i} className="h-24 bg-slate-800/50 rounded animate-pulse" />
                ))}
              </div>
            ))
          ) : (
            grouped.map(col => (
              <KanbanColumn
                key={col.key}
                label={col.label}
                icon={col.icon}
                accent={col.accent}
                count={col.items.length}
                items={col.items}
                selectedId={selectedId}
                onSelect={onSelect}
              />
            ))
          )}
        </main>
      </div>

      {/* 底部抽屉详情 */}
      {selected && (
        <BottomSheet task={selected} onClose={() => onSelect(null)} />
      )}
    </div>
  )
}

function IconRailBtn({ icon, label, active, href }: { icon: React.ReactNode; label: string; active?: boolean; href?: string }) {
  const cls = `w-9 h-9 rounded-lg flex items-center justify-center transition-colors ${
    active ? 'bg-cyan-500/15 text-cyan-400 border border-cyan-500/30' : 'text-slate-500 hover:text-slate-300 hover:bg-slate-800/50'
  }`
  if (href) return <Link href={href} className={cls} title={label}>{icon}</Link>
  return <button className={cls} title={label}>{icon}</button>
}

function Stat({ label, value, highlight }: { label: string; value: number; highlight?: boolean }) {
  return (
    <div className="text-center">
      <div className="text-[9px] text-slate-600">{label}</div>
      <div className={`text-[16px] font-bold tabular-nums ${highlight ? 'text-cyan-400' : 'text-slate-300'}`}>{value}</div>
    </div>
  )
}

function KanbanColumn({
  label, icon, accent, count, items, selectedId, onSelect,
}: {
  label: string; icon: React.ReactNode; accent: string; count: number
  items: VideoTask[]; selectedId: number | null; onSelect: (id: number) => void
}) {
  return (
    <div className={`w-72 shrink-0 flex flex-col rounded-lg border border-slate-800 bg-slate-900/20 border-t-2 ${accent}`}>
      <div className="shrink-0 px-3 py-2.5 flex items-center justify-between border-b border-slate-800/80">
        <div className="flex items-center gap-2 text-[11px] text-slate-400 uppercase tracking-wide">
          {icon}{label}
        </div>
        <span className="text-[11px] text-slate-600 tabular-nums">{count}</span>
      </div>
      <div className="flex-1 overflow-y-auto p-2 space-y-2 min-h-[120px]">
        {items.length === 0 ? (
          <div className="py-8 text-center text-[10px] text-slate-600 uppercase">— empty —</div>
        ) : (
          items.map(t => (
            <KanbanCard
              key={t.id}
              task={t}
              selected={selectedId === t.id}
              onClick={() => onSelect(t.id)}
            />
          ))
        )}
      </div>
    </div>
  )
}

function KanbanCard({ task, selected, onClick }: { task: VideoTask; selected: boolean; onClick: () => void }) {
  const phases = computePhases(task)
  const active = phases.find(p => p.state === 'running')

  return (
    <button
      onClick={onClick}
      className={`w-full text-left p-3 rounded border transition-all ${
        selected
          ? 'border-cyan-500/60 bg-cyan-950/30 shadow-lg shadow-cyan-500/10'
          : 'border-slate-800 bg-slate-900/60 hover:border-slate-600'
      }`}
    >
      <div className="text-[12px] text-slate-200 font-medium truncate leading-snug">{taskTitle(task)}</div>
      <div className="flex items-center gap-2 mt-1.5 text-[9px] text-slate-500">
        <span>#{task.id}</span>
        <span>·</span>
        <span>{fmtSize(task.file_size)}</span>
        <span>·</span>
        <span>{fmtRelTime(task.created_at)}</span>
      </div>
      {active && (
        <div className="mt-2">
          <div className="flex justify-between text-[9px] text-cyan-500/80 mb-0.5">
            <span>{active.label}</span>
            <span>{active.pct}%</span>
          </div>
          <div className="h-1 bg-slate-800 rounded-full overflow-hidden">
            <div className="h-full bg-cyan-500 rounded-full transition-all" style={{ width: `${active.pct}%` }} />
          </div>
        </div>
      )}
      <div className="flex gap-1 mt-2">
        {phases.map(p => (
          <span key={p.label} className={`w-2 h-2 rounded-sm ${
            p.state === 'done' ? 'bg-emerald-500' : p.state === 'running' ? 'bg-cyan-400 animate-pulse' : 'bg-slate-700'
          }`} title={p.label} />
        ))}
      </div>
    </button>
  )
}

function BottomSheet({ task, onClose }: { task: VideoTask; onClose: () => void }) {
  const phases = computePhases(task)
  const [expanded, setExpanded] = useState(true)

  return (
    <>
      <div className="fixed inset-0 bg-black/50 z-40" onClick={onClose} />
      <div className={`fixed inset-x-0 bottom-0 z-50 bg-[#0d1117] border-t border-slate-700 rounded-t-2xl shadow-2xl transition-all ${expanded ? 'h-[55vh]' : 'h-14'}`}>
        <div className="flex items-center justify-between px-5 py-3 border-b border-slate-800">
          <div className="min-w-0">
            <div className="text-[10px] text-slate-500">TASK #{task.id}</div>
            <div className="text-[15px] font-semibold text-slate-100 truncate">{taskTitle(task)}</div>
          </div>
          <div className="flex items-center gap-2">
            <button
              onClick={() => setExpanded(e => !e)}
              className="w-8 h-8 rounded border border-slate-700 flex items-center justify-center text-slate-400 hover:text-slate-200"
            >
              <ChevronUp className={`w-4 h-4 transition-transform ${expanded ? '' : 'rotate-180'}`} />
            </button>
            <button onClick={onClose} className="w-8 h-8 rounded border border-slate-700 flex items-center justify-center text-slate-400 hover:text-slate-200">
              <X className="w-4 h-4" />
            </button>
          </div>
        </div>

        {expanded && (
          <div className="overflow-y-auto h-[calc(55vh-56px)] px-5 py-4">
            <div className="grid grid-cols-2 sm:grid-cols-4 gap-3 mb-5">
              <MetaBox label="STATUS" value={statusLabel(task.status)} />
              <MetaBox label="SOURCE" value={sourceLabel(task)} />
              <MetaBox label="SIZE" value={fmtSize(task.file_size)} />
              <MetaBox label="WORDS" value={task.transcription ? String(task.transcription.words) : '—'} />
            </div>

            <div className="text-[10px] text-slate-500 uppercase mb-2">Pipeline Stages</div>
            <div className="flex gap-2 mb-5">
              {phases.map((p, i) => (
                <div key={p.label} className="flex-1">
                  <div className={`h-2 rounded-full ${p.state === 'done' ? 'bg-emerald-500' : p.state === 'running' ? 'bg-cyan-400' : 'bg-slate-800'}`} />
                  <div className="text-[9px] text-slate-500 mt-1 text-center">{p.label}</div>
                </div>
              ))}
            </div>

            {task.last_error_msg && (
              <div className="mb-4 p-3 rounded border border-rose-900/50 bg-rose-950/30 text-rose-400 text-[11px]">
                {task.last_error_msg || task.error_msg}
              </div>
            )}

            <div className="flex gap-2">
              {task.has_transcription && (
                <Link
                  href={`/chat/${task.id}`}
                  className="flex-1 h-10 rounded bg-cyan-600 hover:bg-cyan-500 text-white text-[12px] font-medium flex items-center justify-center gap-2"
                >
                  <MessageCircle className="w-4 h-4" />RAG CHAT
                </Link>
              )}
            </div>
          </div>
        )}
      </div>
    </>
  )
}

function MetaBox({ label, value }: { label: string; value: string }) {
  return (
    <div className="p-2.5 rounded border border-slate-800 bg-slate-900/50">
      <div className="text-[9px] text-slate-600">{label}</div>
      <div className="text-[12px] text-slate-300 mt-0.5 truncate">{value}</div>
    </div>
  )
}
