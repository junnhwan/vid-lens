import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useCallback } from 'react'
import Link from '@/lib/router'
import { useRouter } from '@/lib/router'
import { api, ApiError } from '@/lib/api'
import type { ChatSession } from '@/lib/types'
import { fmtRelTime, taskTitle } from '@/lib/format'
import { taskCategory, taskNeedsPolling, taskStateView } from '@/lib/taskStatus'
import { summaryFailureView } from '@/lib/summaryFailure'
import { VideoCard } from '@/components/VideoCard'
import { useCrumb, useShell } from '@/components/shell/AppShell'
import { useToast } from '@/components/Toast'
import { Icon } from '@/components/ui/Icon'
import { CardSkeleton, EmptyState, ErrorState, ProductSkeleton } from '@/components/ui/AsyncState'
import { RecentProductWork } from '@/components/artifacts/RecentProductWork'
import { ProductHero } from '@/components/product/ProductHero'
import { ProcessStrip } from '@/components/ProcessStrip'
import { artifactApi } from '@/lib/artifacts/api'
import { formatClock } from '@/lib/format'

export default function DashboardPage() {
  const router = useRouter()
  const toast = useToast()
  const { uploadRevision, openUpload } = useShell()
  useCrumb([{ label: '工作台' }])

  const queryClient = useQueryClient()
  const taskQuery = useQuery({ queryKey:['home-videos',uploadRevision], queryFn:() => api.listTasks(1,50), refetchInterval:query => query.state.data?.list.some(t => taskNeedsPolling(t) || summaryFailureView(t)?.scheduled) ? 5000 : false })
  const sessionQuery = useQuery({ queryKey:['home-sessions'], queryFn:() => api.listSessions() })
  const positionQuery = useQuery({ queryKey:['home-learning-position'], queryFn:async () => { const position=await artifactApi.position(); return position ? {position,task:await api.getTask(position.task_id)} : null } })
  const tasks=taskQuery.data?.list || []
  const total=taskQuery.data?.total || 0
  const sessions=sessionQuery.data || []
  const loading=taskQuery.isPending
  const resume=positionQuery.data
  const processing=tasks.filter(t => t.status !== 1 && t.status !== 2 && taskCategory(t) !== 'ready')
  const activeCount=tasks.filter(t => t.status === 1 || t.status === 2 || t.summary_job?.status === 1 || t.summary_job?.status === 2).length
  const taskTitleById = useCallback((id: number) => {
    const t = tasks.find(x => x.id === id)
    return t ? taskTitle(t) : null
  }, [tasks])

  const removeSession = async (s: ChatSession) => {
    if (!window.confirm('删除这个会话?删除后聊天记录不可恢复。')) return
    try {
      await api.deleteSession(s.id)
      queryClient.setQueryData<ChatSession[]>(['home-sessions'], list => list?.filter(item => item.id !== s.id))
      toast.success('会话已删除')
    } catch (e) {
      toast.error(e instanceof ApiError ? e.message : '删除失败')
    }
  }

  return (
    <div className="page">
      <ProductHero onImport={openUpload} current={resume?.task} resumeUpdatedAt={resume?.position.updated_at} resumeHref={resume ? resume.position.artifact_id && resume.position.block_id ? `/artifacts/${encodeURIComponent(resume.position.artifact_id)}?block=${encodeURIComponent(resume.position.block_id)}` : `/video/${resume.position.task_id}?t=${resume.position.time_ms}` : undefined} resumeLabel={resume ? resume.position.artifact_id ? resume.position.fallback ? '原段落或版本已变化，已回退到可读位置' : '已保存笔记段落' : `视频 ${formatClock(resume.position.time_ms)}` : undefined} loading={positionQuery.isPending} />
      {positionQuery.error && <div className="artifact-notice" role="alert">学习位置读取失败<button className="btn btn-sm" onClick={() => void positionQuery.refetch()}>重试</button></div>}
      {activeCount > 0 && <p className="home-background-status">近期有 {activeCount} 个后台任务正在处理。<Link href="/tasks">查看任务</Link></p>}
      <div className="product-metrics">
        <Link href="/library" className="product-metric"><span>视频资料</span><strong>{loading || !taskQuery.data ? '—' : String(total).padStart(2, '0')}</strong></Link>
        <Link href="/chat" className="product-metric"><span>保存的会话</span><strong>{sessionQuery.isPending || sessionQuery.error ? '—' : String(sessions.length).padStart(2, '0')}</strong></Link>
        <Link href="/library" className="product-metric"><span>近期需处理</span><strong>{loading || !taskQuery.data ? '—' : String(processing.length).padStart(2, '0')}</strong></Link>
      </div>
      {processing.length > 0 && (
        <>
          <div className="section-head" style={{ marginTop: 0 }}>
            <h2>需要处理</h2>
            <Link className="more" href="/library">全部视频 <Icon name="chev-r" size="sm" /></Link>
          </div>
          <div style={{ display: 'grid', gap: 10 }}>
            {processing.slice(0, 3).map(t => {
              const failed = taskCategory(t) === 'failed'
              const summaryFailure = summaryFailureView(t)
              return (
                <div key={t.id} className="proc-row">
                  <div className="proc-left">
                    <h5><Link href={`/video/${t.id}`}>{taskTitle(t)}</Link></h5>
                    {t.summary_job && [1, 2, 4, 5].includes(t.summary_job.status)
                      ? <span className={`chip ${taskStateView(t).chip}`}>{taskStateView(t).text}</span>
                      : <ProcessStrip status={t.status} stage={t.stage} has_transcription={t.has_transcription} last_job_type={t.last_job_type} has_rag_index={t.has_rag_index} visual_status={t.visual_status} />}
                    {summaryFailure && <span style={{ fontSize: 12, color: 'var(--tx-3)' }}>{summaryFailure.category} · {summaryFailure.retry}</span>}
                  </div>
                  <Link className="btn btn-sm" href={`/video/${t.id}`}>{failed ? '查看原因' : t.summary_job && (t.summary_job.status === 1 || t.summary_job.status === 2) ? '查看摘要进度' : '选择处理方式'}</Link>
                </div>
              )
            })}
          </div>
        </>
      )}

      {loading && processing.length === 0 && (
        <ProductSkeleton kind="rows" count={2} />
      )}

      <div className="section-head" style={{ marginTop: processing.length > 0 || loading ? undefined : 0 }}>
        <h2>最近视频</h2>
        <Link className="more" href="/library">视频库 <Icon name="chev-r" size="sm" /></Link>
      </div>
      {taskQuery.error && <ErrorState message="视频资料加载失败" onRetry={() => void taskQuery.refetch()} />}
      {loading ? <CardSkeleton count={3} /> : tasks.length > 0 ? (
        <div className="video-grid">{tasks.slice(0, 3).map(t => <VideoCard key={t.id} task={t} />)}</div>
      ) : (
        !loading && !taskQuery.error && (
          <EmptyState
            icon="video"
            title="还没有视频"
            action={<button className="btn btn-sm btn-primary" onClick={() => router.push('/library')}>去视频库</button>}
          />
        )
      )}

      <div className="section-head">
        <h2>最近会话</h2>
        {sessions.length > 4 && <Link className="more" href="/chat">全部会话 <Icon name="chev-r" size="sm" /></Link>}
      </div>
      <div className="card" style={{ padding: 8 }}>
        {sessionQuery.error && <ErrorState message="会话加载失败" onRetry={() => void sessionQuery.refetch()} />}
        {sessionQuery.isPending ? <ProductSkeleton kind="rows" count={3} /> : sessions.length > 0 ? sessions.slice(0, 4).map(s => {
          const isKb = s.knowledge_base_id > 0
          const where = s.scope_type === 'video_library' ? '视频库会话' : isKb ? '知识库会话' : taskTitleById(s.task_id) || '单视频会话'
          const href = s.scope_type === 'video_library' ? `/chat/library?session=${s.id}` : isKb ? `/chat/kb/${s.knowledge_base_id}?session=${s.id}` : `/chat/v/${s.task_id}?session=${s.id}`
          return (
            <div key={s.id} className="session-row" onClick={() => router.push(href)}>
              <Icon name="message" />
              <Link className="q" href={href}>{s.title || '未命名会话'}</Link>
              <span className="where">{where}</span>
              <span className="where">{fmtRelTime(s.updated_at)}</span>
              <button
                className="session-del"
                title="删除会话"
                aria-label="删除会话"
                onClick={e => { e.stopPropagation(); void removeSession(s) }}
              >
                <Icon name="trash" size="sm" />
              </button>
            </div>
          )
        }) : (
          !sessionQuery.isPending && !sessionQuery.error && <EmptyState variant="bare" title="还没有会话" />
        )}
      </div>
      <RecentProductWork />

    </div>
  )
}
