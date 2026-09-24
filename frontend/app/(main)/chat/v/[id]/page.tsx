'use client'

import { useEffect, useState } from 'react'
import { api, ApiError } from '@/lib/api'
import type { VideoTask, VideoQuestionResult } from '@/lib/types'
import { taskTitle } from '@/lib/format'
import { ChatWorkspace } from '@/components/chat/ChatWorkspace'
import { useCrumb } from '@/components/shell/AppShell'
import { LoadingBlock, ErrorState } from '@/components/ui/AsyncState'

// 单视频问答(/chat/v/:id)。本阶段仅快速问答(strict_rag SSE);
// 播放源签名 URL 供右栏迷你播放器与引用回放使用。

export default function VideoChatPage({ params }: { params: { id: string } }) {
  const taskId = Number(params.id)
  const [task, setTask] = useState<VideoTask | null>(null)
  const [playbackUrl, setPlaybackUrl] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)
  const [loadError, setLoadError] = useState('')
  const [reloadKey, setReloadKey] = useState(0)
  const [questions, setQuestions] = useState<VideoQuestionResult | null>(null)

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
      void api.getVideoQuestions(taskId).then(result => { if (active) setQuestions(result) }).catch(() => { if (active) setQuestions({ status: 'no_evidence', message: '推荐问题暂时不可用，可以直接提问。', questions: [] }) })
      const playback = await api.playbackSrc(taskId).catch(() => null)
      if (active && playback) setPlaybackUrl(playback)
    })()
    return () => { active = false }
  }, [taskId, reloadKey])

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

  return (
    <ChatWorkspace
      scopeType="video"
      targetId={taskId}
      scopeName={taskTitle(task)}
      playbackUrl={playbackUrl}
      refreshPlaybackUrl={refreshPlaybackUrl}
      suggestions={[]}
      videoQuestions={questions}
      refreshQuestions={() => void api.getVideoQuestions(taskId).then(setQuestions).catch(() => {})}
    />
  )
}
