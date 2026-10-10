import { useEffect, useRef, useState } from 'react'
import { api, ApiError } from '@/lib/api'
import type { EffectiveSummaryView, VideoTask, VideoQuestionResult } from '@/lib/types'
import { taskTitle } from '@/lib/format'
import { ChatWorkspace } from '@/components/chat/ChatWorkspace'
import { useCrumb, useShell } from '@/components/shell/AppShell'
import { LoadingBlock, ErrorState } from '@/components/ui/AsyncState'
import { artifactApi, artifactError } from '@/lib/artifacts/api'
import type { StudyBlock } from '@/lib/artifacts/schema'
import { useVideoAIPreflight } from '@/components/settings/VideoAIPreflight'
import { SummaryReadView } from '@/components/summary/SummaryReadView'
import { useSummarySessionView, useSummaryTaskView } from '@/components/summary/useSummaryViewState'
import type { SummaryContextRef } from '@/lib/summaryExperience'
import '@/components/summary/VideoChatSummary.css'
import Link from '@/lib/router'

// 单视频问答(/chat/v/:id)。本阶段仅快速问答(strict_rag SSE);
// 播放源签名 URL 供右栏迷你播放器与引用回放使用。

export default function VideoChatPage({ params, searchParams }: { params: { id: string }; searchParams?: { artifact?: string; version?: string; block?: string; ask?: string } }) {
  const taskId = Number(params.id)
  const { user } = useShell()
  const [view, patchView] = useSummaryTaskView(user?.id, taskId)
  const [sessionView, patchSessionView] = useSummarySessionView(user?.id, taskId, view.sessionID)
  const [summary, setSummary] = useState<EffectiveSummaryView | null>(null)
  const [summaryError, setSummaryError] = useState('')
  const [summaryLoading, setSummaryLoading] = useState(false)
  const [summaryReload, setSummaryReload] = useState(0)
  const [referenceNotice, setReferenceNotice] = useState('')
  const [returnReference, setReturnReference] = useState<SummaryContextRef | null>(null)
  const readerRef = useRef<HTMLElement>(null)
  const [task, setTask] = useState<VideoTask | null>(null)
  const [playbackUrl, setPlaybackUrl] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)
  const [loadError, setLoadError] = useState('')
  const [reloadKey, setReloadKey] = useState(0)
  const [questions, setQuestions] = useState<VideoQuestionResult | null>(null)
  const [questionsLoading, setQuestionsLoading] = useState(true)
  const [studyBlock, setStudyBlock] = useState<StudyBlock | null>(null)
  const [studyError, setStudyError] = useState('')
  const [preflightAccepted, setPreflightAccepted] = useState(false)
  const videoPreflight = useVideoAIPreflight()
  const returnHref = searchParams?.artifact && searchParams?.block ? `/artifacts/${encodeURIComponent(searchParams.artifact)}?block=${encodeURIComponent(searchParams.block)}` : ''

  useEffect(() => {
    let live = true
    setStudyBlock(null); setStudyError('')
    if (!searchParams?.artifact || !searchParams.version || !searchParams.block) return
    void artifactApi.blockContext(searchParams.artifact,searchParams.version,searchParams.block).then(context => {
      if (!live) return
      if (context.task_id !== taskId) { setStudyError('段落来源与当前视频不一致'); return }
      setStudyBlock(context.block)
    }).catch(error => { if (live) setStudyError(artifactError(error)) })
    return () => { live = false }
  }, [user?.id,taskId,searchParams?.artifact,searchParams?.version,searchParams?.block])
  useEffect(() => { setSummary(null); setSummaryError(''); setReferenceNotice(''); setReturnReference(null) }, [user?.id, taskId])
  useEffect(() => {
    if (!view.summaryOpen || !task || task.id !== taskId) return
    let active = true
    setSummaryLoading(true); setSummaryError('')
    void api.getSummary(taskId).then(value => { if (active) setSummary(value) }).catch(error => { if (active) setSummaryError(error instanceof Error ? error.message : '摘要读取失败') }).finally(() => { if (active) setSummaryLoading(false) })
    return () => { active = false }
  }, [user?.id, taskId, task?.id, view.summaryOpen, summaryReload])
  useEffect(() => {
    if (!view.summaryOpen || !summary || !returnReference) return
    if (returnReference.document_digest !== summary.content_digest) {
      setReferenceNotice('这段引用来自较早的摘要版本，当前正文已变化；提问仍会保留原引用快照。')
      setReturnReference(null)
    } else {
      const frame = requestAnimationFrame(() => {
        document.getElementById(`summary-block-${encodeURIComponent(returnReference.block_id)}`)?.scrollIntoView({ block: 'center' })
        setReturnReference(null)
      })
      return () => cancelAnimationFrame(frame)
    }
  }, [view.summaryOpen, summary, returnReference])
  const addReference = (ref: SummaryContextRef) => {
    if (sessionView.contexts.some(item => JSON.stringify(item) === JSON.stringify(ref))) return
    const next = [...sessionView.contexts, ref]
    if (next.length > 3 || next.reduce((total, item) => total + Array.from(item.quote).length, 0) > 3000) { setReferenceNotice('一次问题最多引用 3 段、合计 3000 字；请先移除部分引用。'); return }
    patchSessionView({ contexts: next })
  }

  useCrumb([
    { label: '视频库', href: '/library' },
    { label: task ? taskTitle(task) : `视频 #${taskId}`, href: `/video/${taskId}` },
    { label: '问答' },
  ])

  useEffect(() => {
    let active = true
    setLoading(true)
    setLoadError('')
    setTask(null)
    setPlaybackUrl(null)
    setQuestions(null)
    setQuestionsLoading(false)
    setPreflightAccepted(false)
    void (async () => {
      let detail: VideoTask
      try {
        detail = await api.getTask(taskId)
      } catch (e) {
        if (!active) return
        setLoadError(e instanceof ApiError ? e.message : '视频加载失败')
        setLoading(false)
        return
      }
      if (!active) return
      setTask(detail)
      setLoading(false)
      videoPreflight.request('单视频问答', () => {
        if (!active) return
        setPreflightAccepted(true)
        if (searchParams?.ask) return
        setQuestionsLoading(true)
        void (async () => {
          try { const result = await api.generateVideoQuestions(taskId); if (active) setQuestions(result) }
          catch { if (active) setQuestions({ status: 'no_evidence', message: '推荐问题暂时不可用，可以直接提问。', questions: [] }) }
          finally { if (active) setQuestionsLoading(false) }
        })()
      })
      const playback = await api.playbackSrc(taskId).catch(() => null)
      if (active && playback) setPlaybackUrl(playback)
    })()
    return () => { active = false }
  }, [user?.id, taskId, reloadKey, videoPreflight.request, searchParams?.ask])

  // 播放地址为站内路径 + 任务级凭证,不再有 5 分钟签名到期问题;
  // 加载失败时重取一次,覆盖凭证过期或对象临时不可用。
  const refreshPlaybackUrl = async () => {
    try {
      const src = await api.playbackSrc(taskId)
      if (src) {
        setPlaybackUrl(src)
        return src
      }
    } catch { /* 保持失败态 */ }
    return null
  }

  if (loading) {
    return <div className="page"><LoadingBlock /></div>
  }
  if (loadError || !task) {
    return (
      <div className="page">
        <ErrorState message={loadError || '视频加载失败'} onRetry={() => setReloadKey(k => k + 1)} />
      </div>
    )
  }

  return <div className={`video-chat-with-summary${view.summaryOpen ? ' summary-open' : ''}`}>
    <header className="video-chat-summary-toggle"><button className="btn btn-sm" aria-expanded={view.summaryOpen} aria-controls="video-chat-summary-reader" onClick={() => patchView({ summaryOpen: !view.summaryOpen })}>{view.summaryOpen ? '收起摘要' : '展开当前有效摘要'}</button><Link className="btn btn-sm" href={`/video/${taskId}${view.sessionID ? `?session=${view.sessionID}` : ''}`}>返回摘要详情</Link><span className="muted">从摘要选段提问，正文与引用分别发送</span></header>
    <div className="video-chat-summary-layout">
    <article id="video-chat-summary-reader" ref={readerRef} className="video-chat-summary-reader" aria-label="当前有效摘要" hidden={!view.summaryOpen}>
      {summaryLoading && !summary && <LoadingBlock />}
      {summaryError && <ErrorState message={summaryError} onRetry={() => setSummaryReload(key => key + 1)} />}
      {referenceNotice && <p role="status">{referenceNotice}</p>}
      {summary && <SummaryReadView readerRef={readerRef} taskId={taskId} summary={summary} mediaRevision={task.file_md5} playbackReady={false} onSeek={() => {}} onReference={addReference} onMessage={setReferenceNotice} readOnly={user?.role === 'DEMO'} />}
    </article>
    <div className="video-chat-summary-conversation">
    <ChatWorkspace
      sharedSummaryState
      scopeType="video"
      targetId={taskId}
      scopeName={taskTitle(task)}
      videoVisualMode={task.visual_mode}
      videoRetrievable={task.retrievable}
      videoHasTranscript={task.has_transcription || !!task.active_text_source_id || task.has_summary}
      aiPreflightAccepted={preflightAccepted}
      playbackUrl={playbackUrl}
      refreshPlaybackUrl={refreshPlaybackUrl}
      suggestions={[]}
      videoQuestions={questions}
      questionsLoading={questionsLoading}
      studyBlock={studyBlock}
      studyError={studyError}
      returnToStudy={returnHref}
      summaryContextRefs={sessionView.contexts}
      onRemoveSummaryContext={index => patchSessionView({ contexts: sessionView.contexts.filter((_, i) => i !== index) })}
      onContextSent={() => patchSessionView({ contexts: [] })}
      onReturnSummaryContext={index => {
        const ref = sessionView.contexts[index]
        if (!ref) return
        patchView({ summaryOpen: true })
        setReturnReference(ref)
      }}
    />
    </div>
    </div>
    {videoPreflight.dialog}
  </div>
}
