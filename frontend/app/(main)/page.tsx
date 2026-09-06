'use client'

import { useCallback, useEffect, useState } from 'react'
import { useRouter } from 'next/navigation'
import { api, ApiError } from '@/lib/api'
import type { ChatSession, VideoTask } from '@/lib/types'
import { fmtRelTime, taskTitle } from '@/lib/format'
import { taskCategory } from '@/lib/taskStatus'
import { VideoCard } from '@/components/VideoCard'
import { useCrumb } from '@/components/shell/AppShell'
import { useToast } from '@/components/Toast'
import { Icon } from '@/components/ui/Icon'

export default function DashboardPage() {
  const router = useRouter()
  const toast = useToast()
  useCrumb([{ label: '工作台' }])

  const [tasks, setTasks] = useState<VideoTask[]>([])
  const [sessions, setSessions] = useState<ChatSession[]>([])
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    let active = true
    void (async () => {
      const [taskPage, sessionList] = await Promise.all([
        api.listTasks(1, 50).catch(() => null),
        api.listSessions().catch(() => []),
      ])
      if (!active) return
      setTasks(taskPage?.list || [])
      setSessions(sessionList)
      setLoading(false)
    })()
    return () => { active = false }
  }, [])

  const processing = tasks.filter(t => taskCategory(t) !== 'ready')
  const taskTitleById = useCallback((id: number) => {
    const t = tasks.find(x => x.id === id)
    return t ? taskTitle(t) : null
  }, [tasks])

  const retry = async (t: VideoTask) => {
    try {
      if (t.last_job_type === 'analyze') await api.analyze(t.id)
      else await api.transcribe(t.id)
      toast.success('已重新入队')
    } catch (e) {
      toast.error(e instanceof ApiError ? e.message : '重试失败')
    }
  }

  return (
    <div className="page">
      <div className="section-head" style={{ marginTop: 0 }}>
        <h2>继续处理</h2>
        <span className="more" onClick={() => router.push('/library')}>全部视频 <Icon name="chev-r" size="sm" /></span>
      </div>
      <div style={{ display: 'grid', gap: 10 }}>
        {loading && <div className="card card-pad" style={{ color: 'var(--tx-3)' }}>正在加载…</div>}
        {!loading && processing.map(t => {
          const failed = t.status === 4 || t.status === 5
          const noRetry = failed && t.last_job_type === 'download'
          return (
            <div key={t.id} className="proc-row" style={{ cursor: 'pointer' }} onClick={() => router.push(`/video/${t.id}`)}>
              <div className="proc-left">
                <h5>{taskTitle(t)}</h5>
                <div className="stage">
                  {failed
                    ? <span style={{ color: 'var(--bad)' }}>{t.error_msg || '处理失败'}</span>
                    : stageOrIdle(t)}
                </div>
              </div>
              {failed
                ? noRetry
                  ? <span className="chip chip-mute">请删除后重新添加</span>
                  : <button className="btn btn-sm" onClick={e => { e.stopPropagation(); void retry(t) }}>重试</button>
                : <span className={`chip ${t.status === 2 ? 'chip-acc' : 'chip-mute'}`}>{t.status === 2 ? '处理中' : '排队中'}</span>}
            </div>
          )
        })}
        {!loading && processing.length === 0 && (
          <div className="empty"><b>没有进行中的任务</b></div>
        )}
      </div>

      <div className="section-head">
        <h2>最近视频</h2>
        <span className="more" onClick={() => router.push('/library')}>视频库 <Icon name="chev-r" size="sm" /></span>
      </div>
      {tasks.length > 0 ? (
        <div className="video-grid">{tasks.slice(0, 4).map(t => <VideoCard key={t.id} task={t} />)}</div>
      ) : (
        !loading && (
          <div className="card">
            <div className="empty">
              <Icon name="video" size="lg" />
              <b>还没有视频</b>
              <button className="btn btn-sm btn-primary" onClick={() => router.push('/library')}>去视频库</button>
            </div>
          </div>
        )
      )}

      <div className="section-head"><h2>最近会话</h2></div>
      <div className="card" style={{ padding: 8 }}>
        {sessions.length > 0 ? sessions.slice(0, 8).map(s => {
          const isKb = s.knowledge_base_id > 0
          const where = isKb ? '知识库会话' : taskTitleById(s.task_id) || '单视频会话'
          const href = isKb ? `/chat/kb/${s.knowledge_base_id}?session=${s.id}` : `/chat/v/${s.task_id}?session=${s.id}`
          return (
            <button key={s.id} className="session-row" onClick={() => router.push(href)}>
              <Icon name="message" />
              <span className="q">{s.title || '未命名会话'}</span>
              <span className="where">{where}</span>
              <span className="where">{fmtRelTime(s.updated_at)}</span>
            </button>
          )
        }) : (
          !loading && <div className="empty"><b>还没有会话</b></div>
        )}
      </div>
    </div>
  )
}

function stageOrIdle(t: VideoTask): string {
  const labels: Record<string, string> = {
    downloading: '下载中', uploaded: '已上传', transcribing: '转写中',
    visual_indexing: '画面索引中', summarizing: '生成摘要中', indexing: '构建索引中', none: '处理中',
  }
  return labels[t.stage] || '处理中'
}
