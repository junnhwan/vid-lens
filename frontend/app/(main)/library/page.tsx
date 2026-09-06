'use client'

import { useEffect, useMemo, useState } from 'react'
import { api } from '@/lib/api'
import type { VideoTask } from '@/lib/types'
import { taskCategory, type TaskCategory } from '@/lib/taskStatus'
import { taskTitle } from '@/lib/format'
import { VideoCard } from '@/components/VideoCard'
import { useCrumb, useShell } from '@/components/shell/AppShell'
import { Icon } from '@/components/ui/Icon'

const PAGE_SIZE = 100

export default function LibraryPage() {
  const { openUpload } = useShell()
  useCrumb([{ label: '视频库' }])

  const [tasks, setTasks] = useState<VideoTask[]>([])
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [filter, setFilter] = useState<'all' | TaskCategory>('all')
  const [keyword, setKeyword] = useState('')

  useEffect(() => {
    let active = true
    void (async () => {
      try {
        const page = await api.listTasks(1, PAGE_SIZE)
        if (!active) return
        setTasks(page.list)
        setTotal(page.total)
      } catch {
        if (active) setError('视频列表加载失败')
      } finally {
        if (active) setLoading(false)
      }
    })()
    return () => { active = false }
  }, [])

  const list = useMemo(() => tasks.filter(t => {
    if (keyword && !taskTitle(t).toLowerCase().includes(keyword.toLowerCase())) return false
    if (filter === 'all') return true
    return taskCategory(t) === filter
  }), [tasks, keyword, filter])

  const readyCount = tasks.filter(t => taskCategory(t) === 'ready').length

  const segs: { key: 'all' | TaskCategory; label: string }[] = [
    { key: 'all', label: '全部' },
    { key: 'ready', label: '可问答' },
    { key: 'processing', label: '处理中' },
    { key: 'failed', label: '失败' },
  ]

  return (
    <div className="page page-wide">
      <div className="section-head" style={{ marginTop: 0 }}>
        <h2>视频库</h2>
        <span style={{ fontSize: 13, color: 'var(--tx-3)' }}>
          {loading ? '加载中…' : `${tasks.length} 个视频 · ${readyCount} 个可问答`}
        </span>
        <span className="more" onClick={openUpload}><Icon name="plus" size="sm" />上传视频</span>
      </div>

      <div className="lib-toolbar">
        <input
          id="libFilter"
          className="input"
          placeholder="按标题过滤…"
          value={keyword}
          onChange={e => setKeyword(e.target.value)}
        />
        <div className="seg">
          {segs.map(s => (
            <button key={s.key} className={filter === s.key ? 'on' : ''} onClick={() => setFilter(s.key)}>{s.label}</button>
          ))}
        </div>
      </div>

      {error && <div className="card card-pad" style={{ color: 'var(--bad)' }}>{error}</div>}

      {!error && !loading && tasks.length === 0 && (
        <div className="card">
          <div className="empty">
            <Icon name="video" size="lg" />
            <b>还没有视频</b>
            <button className="btn btn-sm btn-primary" onClick={openUpload}><Icon name="upload" size="sm" />上传视频</button>
          </div>
        </div>
      )}

      {list.length > 0 && (
        <div className="video-grid">
          {list.map(t => <VideoCard key={t.id} task={t} />)}
        </div>
      )}

      {!error && !loading && tasks.length > 0 && list.length === 0 && (
        <div className="card">
          <div className="empty">
            <Icon name="search" size="lg" />
            <b>没有匹配的视频</b>
          </div>
        </div>
      )}

      {total > tasks.length && (
        <p style={{ fontSize: 13, color: 'var(--tx-4)', marginTop: 16 }}>
          已显示最近 {PAGE_SIZE} 个,共 {total} 个
        </p>
      )}
    </div>
  )
}
