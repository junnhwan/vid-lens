'use client'

import { useEffect, useMemo, useState } from 'react'
import { useRouter } from 'next/navigation'
import { api } from '@/lib/api'
import type { VideoTask } from '@/lib/types'
import { taskCategory, type TaskCategory } from '@/lib/taskStatus'
import { taskTitle } from '@/lib/format'
import { VideoCard } from '@/components/VideoCard'
import { useShell } from '@/components/shell/AppShell'
import { Icon } from '@/components/ui/Icon'

// 视频库:过滤(全部/可问答/处理中/失败)+ 标题过滤 + 上传入口。
// 对应原型 #/library。

const PAGE_SIZE = 100

export default function LibraryPage() {
  const router = useRouter()
  const { openUpload } = useShell()

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
        if (active) setError('视频列表加载失败,请确认后端服务可用后刷新')
      } finally {
        if (active) setLoading(false)
      }
    })()
    return () => { active = false }
  }, [])

  // 外壳的搜索框与 "/" 键带 ?focus=1 进入并聚焦过滤框(与原型一致)
  useEffect(() => {
    if (new URLSearchParams(window.location.search).get('focus')) {
      const el = document.getElementById('libFilter') as HTMLInputElement | null
      el?.focus()
    }
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
        <span style={{ fontSize: 12.5, color: 'var(--tx-3)' }}>
          {loading ? '加载中…' : `${tasks.length} 个视频 · ${readyCount} 个已可问答`}
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

      {error && <div className="card card-pad" style={{ color: 'var(--bad)', fontSize: 12.5 }}>{error}</div>}

      {!error && !loading && tasks.length === 0 && (
        <div className="card">
          <div className="empty">
            <Icon name="video" size="lg" />
            <b>还没有视频</b>
            <p>上传第一个视频,转写完成后就可以向它提问。</p>
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
            <p>换个关键词,或清空过滤器。</p>
          </div>
        </div>
      )}

      {total > tasks.length && (
        <p style={{ fontSize: 11.5, color: 'var(--tx-4)', marginTop: 16 }}>
          已显示最近 {PAGE_SIZE} 个(共 {total} 个),可用标题过滤缩小范围。
        </p>
      )}
    </div>
  )
}
