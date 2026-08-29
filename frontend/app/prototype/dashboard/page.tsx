'use client'

// PROTOTYPE C+ — 视频库工作台（选定方向：C 研读工作台 + A 动效）
// 访问：http://localhost:3000/prototype/dashboard

import { Suspense, useCallback, useEffect, useRef, useState } from 'react'
import DashboardView from '@/components/prototype/c/DashboardView'
import UploadModal from '@/components/UploadModal'
import { api, ApiError } from '@/lib/api'
import { TaskStatusEnum } from '@/lib/types'
import type { VideoTask } from '@/lib/types'
import { MOCK_TASKS } from '@/components/prototype/c/mocks'

export default function PrototypeDashboardPage() {
  return (
    <Suspense fallback={<div className="h-screen bg-[#f7f4ef]" />}>
      <PrototypeDashboardView />
    </Suspense>
  )
}

function PrototypeDashboardView() {
  const [tasks, setTasks] = useState<VideoTask[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [usingMock, setUsingMock] = useState(false)
  const [showUpload, setShowUpload] = useState(false)
  const pollRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  const load = useCallback(async (silent = false) => {
    if (!silent) setLoading(true)
    try {
      const r = await api.listTasks(1, 100, '')
      const list = r.list || []
      if (list.length > 0) {
        setTasks(list)
        setUsingMock(false)
      } else {
        setTasks(MOCK_TASKS)
        setUsingMock(true)
        setError('暂无真实数据 — 展示 mock')
      }
    } catch (e) {
      setTasks(MOCK_TASKS)
      setUsingMock(true)
      setError(e instanceof ApiError && e.message.includes('401')
        ? '未登录 — 展示 mock 数据'
        : (e instanceof ApiError ? e.message : '加载失败') + ' — 展示 mock')
    } finally {
      if (!silent) setLoading(false)
    }
  }, [])

  useEffect(() => { load() }, [load])

  useEffect(() => {
    if (usingMock) return
    const isPending = (s: number) => s === TaskStatusEnum.Pending || s === TaskStatusEnum.Queued || s === TaskStatusEnum.Running
    if (!tasks.some(t => isPending(t.status))) {
      if (pollRef.current) { clearTimeout(pollRef.current); pollRef.current = null }
      return
    }
    pollRef.current = setTimeout(() => load(true), 2500)
    return () => { if (pollRef.current) clearTimeout(pollRef.current) }
  }, [tasks, load, usingMock])

  return (
    <>
      <DashboardView
        tasks={tasks}
        loading={loading}
        error={error}
        onRefresh={() => load()}
        onUpload={() => setShowUpload(true)}
      />
      {showUpload && (
        <UploadModal
          onClose={() => setShowUpload(false)}
          onUploaded={() => { setShowUpload(false); load() }}
        />
      )}
    </>
  )
}
