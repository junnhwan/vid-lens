'use client'

import { useState, useEffect, useCallback, useRef, Suspense } from 'react'
import { useRouter, useSearchParams } from 'next/navigation'
import { Search, Plus } from 'lucide-react'
import Header from '@/components/Header'
import UploadModal from '@/components/UploadModal'
import TaskCard, { TaskCardSkeleton } from '@/components/TaskCard'
import TaskDetailPanel from '@/components/TaskDetailPanel'
import { api, ApiError } from '@/lib/api'
import { TaskStatusEnum } from '@/lib/types'
import { useRole } from '@/lib/useRole'
import type { VideoTask, TaskStatus } from '@/lib/types'

type StatusFilter = 'all' | 'running' | 'completed' | 'failed' | 'dead'
type SourceFilter = 'all' | 'upload' | 'url'

export default function LibraryPage() {
  // useSearchParams 必须包 Suspense，否则 build prerender 失败
  return (
    <Suspense fallback={<div className="flex-1" />}>
      <LibraryView />
    </Suspense>
  )
}

function LibraryView() {
  const router = useRouter()
  const sp = useSearchParams()

  // ?task=:id 入 URL 支持刷新还原；expanded 也入 URL（&ex=1）；全屏 &fs=1
  const selectedId = sp.get('task') ? Number(sp.get('task')) : null
  const expanded = sp.get('ex') === '1'
  const fullscreen = sp.get('fs') === '1'

  const [tasks, setTasks] = useState<VideoTask[]>([])
  const [loading, setLoading] = useState(true)
  const [err, setErr] = useState('')
  const [keyword, setKeyword] = useState('')
  const [statusFilter, setStatusFilter] = useState<StatusFilter>('all')
  const [sourceFilter, setSourceFilter] = useState<SourceFilter>('all')
  const [showUpload, setShowUpload] = useState(false)
  const [selectedTask, setSelectedTask] = useState<VideoTask | null>(null)
  const pollRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const { isDemo } = useRole()

  // 拉列表
  const load = useCallback(async (silent = false) => {
    if (!silent) setLoading(true)
    try {
      const r = await api.listTasks(1, 100, keyword)
      setTasks(r.list || [])
      setErr('')
    } catch (e) {
      setErr(e instanceof ApiError ? e.message : '加载失败')
    } finally {
      if (!silent) setLoading(false)
    }
  }, [keyword])

  useEffect(() => { load() }, [load])

  // 轮询：仅当存在 Running/Pending/Queued 任务时，2.5s 间隔
  const isPending = (s: TaskStatus) => s === TaskStatusEnum.Pending || s === TaskStatusEnum.Queued || s === TaskStatusEnum.Running
  useEffect(() => {
    const hasRunning = tasks.some(t => isPending(t.status))
    if (!hasRunning) {
      if (pollRef.current) { clearTimeout(pollRef.current); pollRef.current = null }
      return
    }
    pollRef.current = setTimeout(() => load(true), 2500)
    return () => { if (pollRef.current) clearTimeout(pollRef.current) }
  }, [tasks, load])

  // 同步选中任务详情（轮询时也更新选中项）
  useEffect(() => {
    if (selectedId == null) { setSelectedTask(null); return }
    let cancelled = false
    api.getTask(selectedId).then(t => { if (!cancelled) setSelectedTask(t) }).catch(() => {})
    return () => { cancelled = true }
  }, [selectedId, tasks])

  // 选中 / 关闭 → 写 URL
  const selectTask = (id: number) => {
    const q = new URLSearchParams(sp.toString())
    q.set('task', String(id))
    q.delete('ex')
    router.push(`?${q.toString()}`, { scroll: false })
  }
  const closePanel = () => {
    const q = new URLSearchParams(sp.toString())
    q.delete('task'); q.delete('ex')
    router.push(`?${q.toString()}`, { scroll: false })
  }
  // 三态视图：narrow(380px 只看进度) / expand(58% 摘要+转写上下) / fullscreen(全屏 摘要+转写左右并排)
  const viewMode = fullscreen ? 'fullscreen' : expanded ? 'expand' : 'narrow'
  const setViewMode = (m: 'narrow' | 'expand' | 'fullscreen') => {
    const q = new URLSearchParams(sp.toString())
    if (m === 'expand') { q.set('ex', '1'); q.delete('fs') }
    else if (m === 'fullscreen') { q.set('ex', '1'); q.set('fs', '1') }
    else { q.delete('ex'); q.delete('fs') }
    router.push(`?${q.toString()}`, { scroll: false })
  }

  // 计数
  const counts = {
    all: tasks.length,
    running: tasks.filter(t => isPending(t.status)).length,
    completed: tasks.filter(t => t.status === TaskStatusEnum.Completed).length,
    failed: tasks.filter(t => t.status === TaskStatusEnum.Failed).length,
    dead: tasks.filter(t => t.status === TaskStatusEnum.Dead).length,
  }

  // 应用筛选
  const filtered = tasks.filter(t => {
    if (statusFilter === 'running' && !isPending(t.status)) return false
    if (statusFilter === 'completed' && t.status !== TaskStatusEnum.Completed) return false
    if (statusFilter === 'failed' && t.status !== TaskStatusEnum.Failed) return false
    if (statusFilter === 'dead' && t.status !== TaskStatusEnum.Dead) return false
    if (sourceFilter === 'upload' && t.source_type !== 'upload' && t.source_type !== 'chunked') return false
    if (sourceFilter === 'url' && t.source_type !== 'url') return false
    return true
  })

  const onUploaded = (taskId: number) => {
    load()
    selectTask(taskId)
  }

  return (
    <div className="flex flex-col h-screen">
      <Header active="library" onUpload={() => setShowUpload(true)} />

      <div className="flex flex-1 min-h-0">
        {/* 左侧筛选栏 */}
        <aside className="w-56 shrink-0 bg-paper-0 border-r border-ink-2/20 flex flex-col">
          <div className="px-5 py-4 border-b border-ink-2/20">
            <div className="text-[11px] font-medium text-ink-3 mb-1.5">搜索</div>
            <div className="relative">
              <input
                value={keyword}
                onChange={(e) => setKeyword(e.target.value)}
                onKeyDown={(e) => { if (e.key === 'Enter') load() }}
                type="search"
                placeholder="视频标题"
                className="w-full h-8 pl-7 pr-2 bg-paper-1 border border-ink-2/20 rounded-md text-[13px] placeholder:text-ink-4 focus:outline-none focus:ring-2 focus:ring-sienna-500/30 focus:border-sienna-500"
              />
              <Search className="w-3.5 h-3.5 absolute left-2 top-1/2 -translate-y-1/2 text-ink-4" />
            </div>
          </div>

          <FilterGroup title="状态">
            <FilterItem label="全部" count={counts.all} active={statusFilter === 'all'} onClick={() => setStatusFilter('all')} />
            <FilterItem label="处理中" count={counts.running} active={statusFilter === 'running'} onClick={() => setStatusFilter('running')} live />
            <FilterItem label="已完成" count={counts.completed} active={statusFilter === 'completed'} onClick={() => setStatusFilter('completed')} />
            <FilterItem label="失败" count={counts.failed} active={statusFilter === 'failed'} onClick={() => setStatusFilter('failed')} danger />
            <FilterItem label="已废弃" count={counts.dead} active={statusFilter === 'dead'} onClick={() => setStatusFilter('dead')} />
          </FilterGroup>

          <FilterGroup title="来源">
            <SourceItem label="全部" dotCls="bg-ink-2/40" active={sourceFilter === 'all'} onClick={() => setSourceFilter('all')} />
            <SourceItem label="本地上传" dotCls="bg-sienna-500" active={sourceFilter === 'upload'} onClick={() => setSourceFilter('upload')} />
            <SourceItem label="URL 下载" dotCls="bg-sky-500" active={sourceFilter === 'url'} onClick={() => setSourceFilter('url')} />
          </FilterGroup>
        </aside>

        {/* 中：列表主体；列表列单独滚动，右侧详情面板固定不随列表滚动 */}
        <main className="flex-1 min-w-0 min-h-0 flex">
          <div className="flex-1 min-w-0 overflow-y-auto">
            <div className="px-8 pt-7 pb-4 border-b border-ink-2/20">
              <div className="flex items-end justify-between gap-6">
                <div>
                  <h1 className="text-[26px] leading-tight font-semibold tracking-tight text-ink-0">我的视频</h1>
                  <p className="text-[13px] text-ink-4 mt-1">共 {counts.all} 个任务{counts.running > 0 ? `，${counts.running} 个处理中` : ''}</p>
                </div>
                {counts.running > 0 && (
                  <div className="text-[11px] text-ink-4 text-right leading-relaxed shrink-0">
                    <div className="flex items-center gap-1.5 justify-end text-sienna-700"><span className="w-1.5 h-1.5 rounded-full bg-sienna-500 live" />自动刷新中</div>
                  </div>
                )}
              </div>
            </div>

            <div className="px-8 py-2">
              {err ? (
                <div className="py-10 text-center text-[13px] text-rust">{err}<button onClick={() => load()} className="ml-2 underline">重试</button></div>
              ) : loading ? (
                Array.from({ length: 5 }).map((_, i) => <TaskCardSkeleton key={i} />)
              ) : filtered.length === 0 ? (
                <div className="py-16 text-center">
                  <div className="font-mono text-[10px] text-ink-4 wide uppercase mb-2">— {tasks.length === 0 ? '暂无视频' : '无匹配结果'} —</div>
                  {tasks.length === 0 && !isDemo && (
                    <button onClick={() => setShowUpload(true)} className="btn-line h-8 px-3 mt-3 text-[12px] font-medium inline-flex items-center gap-1.5"><Plus className="w-3.5 h-3.5" />上传第一个视频</button>
                  )}
                </div>
              ) : (
                filtered.map((t, i) => (
                  <TaskCard
                    key={t.id}
                    task={t}
                    index={i}
                    selected={selectedId === t.id}
                    onSelect={() => selectTask(t.id)}
                    onRetried={load}
                  />
                ))
              )}
              {!loading && filtered.length > 0 && (
                <div className="px-8 py-6 font-mono text-[10px] text-ink-4 wide uppercase">— 已到底部 —</div>
              )}
            </div>
          </div>

          {/* 右：详情面板 */}
          {selectedId != null && (
            <TaskDetailPanel
              task={selectedTask}
              onClose={closePanel}
              viewMode={viewMode}
              onViewMode={setViewMode}
            />
          )}
        </main>
      </div>

      {showUpload && <UploadModal onClose={() => setShowUpload(false)} onUploaded={onUploaded} />}
    </div>
  )
}

// ============ 筛选项子组件 ============
function FilterGroup({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <div className="px-5 py-4 border-b border-ink-2/20">
      <div className="text-[11px] font-medium text-ink-3 mb-2">{title}</div>
      <ul className="space-y-0.5 text-[13px]">{children}</ul>
    </div>
  )
}
function FilterItem({ label, count, active, onClick, live, danger }: {
  label: string; count: number; active: boolean; onClick: () => void; live?: boolean; danger?: boolean
}) {
  return (
    <li>
      <button onClick={onClick} className={`w-full flex justify-between px-2 py-1.5 rounded-md ${active ? 'bg-sienna-500/10 text-sienna-700 font-medium' : 'text-ink-2 hover:bg-ink-2/10'}`}>
        <span className="flex items-center gap-1.5">
          {live && <span className="w-1.5 h-1.5 rounded-full bg-sienna-500 live" />}
          {danger && count > 0 ? <span className="text-rust">{label}</span> : label}
        </span>
        <span className={`text-[11px] ${danger && count > 0 ? 'text-rust' : 'text-ink-4'}`}>{count || (danger ? '' : '—')}</span>
      </button>
    </li>
  )
}
function SourceItem({ label, dotCls, active, onClick }: { label: string; dotCls: string; active: boolean; onClick: () => void }) {
  return (
    <li>
      <button onClick={onClick} className={`w-full flex items-center gap-2 px-2 py-1.5 rounded-md ${active ? 'bg-sienna-500/10 text-sienna-700 font-medium' : 'text-ink-2 hover:bg-ink-2/10'}`}>
        <span className={`w-1 h-3.5 rounded-full ${dotCls}`} />{label}
      </button>
    </li>
  )
}
