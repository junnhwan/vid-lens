'use client'

import { Suspense, useCallback, useEffect, useRef, useState } from 'react'
import { useRouter, useSearchParams } from 'next/navigation'
import LibraryView from '@/components/library/LibraryView'
import VideoDetailModal from '@/components/library/VideoDetailModal'
import UploadModal from '@/components/UploadModal'
import { api, ApiError } from '@/lib/api'
import { TaskStatusEnum } from '@/lib/types'
import type { TaskStatus, VideoTask } from '@/lib/types'

type StatusFilter = 'all' | 'running' | 'completed' | 'failed' | 'dead'
type SourceFilter = 'all' | 'upload' | 'url'

export default function LibraryPage() {
  return (
    <Suspense fallback={<div className="h-screen bg-paper-1" />}>
      <LibraryPageView />
    </Suspense>
  )
}

function LibraryPageView() {
  const router = useRouter()
  const sp = useSearchParams()
  const selectedId = sp.get('task') ? Number(sp.get('task')) : null

  const [tasks, setTasks] = useState<VideoTask[]>([])
  const [loading, setLoading] = useState(true)
  const [err, setErr] = useState('')
  const [keyword, setKeyword] = useState('')
  const [statusFilter, setStatusFilter] = useState<StatusFilter>('all')
  const [sourceFilter, setSourceFilter] = useState<SourceFilter>('all')
  const [showUpload, setShowUpload] = useState(false)
  const [selectedTask, setSelectedTask] = useState<VideoTask | null>(null)
  const [detailLoading, setDetailLoading] = useState(false)
  const pollRef = useRef<ReturnType<typeof setTimeout> | null>(null)

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

  // 选中任务：拉详情（含转写/摘要全文）
  useEffect(() => {
    if (selectedId == null) { setSelectedTask(null); return }
    let cancelled = false
    setDetailLoading(true)
    api.getTask(selectedId).then(t => { if (!cancelled) setSelectedTask(t) }).catch(() => {
      if (!cancelled) setSelectedTask(tasks.find(x => x.id === selectedId) ?? null)
    }).finally(() => { if (!cancelled) setDetailLoading(false) })
    return () => { cancelled = true }
  }, [selectedId, tasks])

  const selectTask = (id: number | null) => {
    const q = new URLSearchParams(sp.toString())
    if (id == null) q.delete('task')
    else q.set('task', String(id))
    router.push(`?${q.toString()}`, { scroll: false })
  }

  const onUploaded = (taskId: number) => {
    load()
    selectTask(taskId)
  }

  return (
    <>
      <LibraryView
        tasks={tasks}
        loading={loading}
        error={err}
        keyword={keyword}
        onKeywordChange={setKeyword}
        onKeywordSubmit={() => load()}
        statusFilter={statusFilter}
        onStatusFilter={setStatusFilter}
        sourceFilter={sourceFilter}
        onSourceFilter={setSourceFilter}
        selectedId={selectedId}
        onSelect={selectTask}
        onRefresh={() => load()}
        onUpload={() => setShowUpload(true)}
      />

      {(selectedId != null || detailLoading) && (
        <VideoDetailModal
          task={selectedTask}
          loading={detailLoading && !selectedTask}
          onClose={() => selectTask(null)}
          onChanged={() => load(true)}
        />
      )}

      {showUpload && (
        <UploadModal onClose={() => setShowUpload(false)} onUploaded={onUploaded} />
      )}
    </>
  )
}
