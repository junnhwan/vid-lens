'use client'

import { useEffect, useState } from 'react'
import { api, ApiError } from '@/lib/api'
import type { VideoTask } from '@/lib/types'
import { taskTitle } from '@/lib/format'
import { ChatWorkspace } from '@/components/chat/ChatWorkspace'
import { useCrumb } from '@/components/shell/AppShell'
import { Icon } from '@/components/ui/Icon'

// 单视频问答(/chat/v/:id)。本阶段仅快速问答(strict_rag SSE);
// 播放源签名 URL 供右栏迷你播放器与引用回放使用。

const SUGGESTIONS = [
  '这段视频的主旨是什么?',
  '视频里提到了哪些关键信息?',
  '帮我把视频的结构梳理一下',
]

export default function VideoChatPage({ params }: { params: { id: string } }) {
  const taskId = Number(params.id)
  const [task, setTask] = useState<VideoTask | null>(null)
  const [playbackUrl, setPlaybackUrl] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)
  const [loadError, setLoadError] = useState('')

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
      const playback = await api.getTaskPlaybackUrl(taskId).catch(() => null)
      if (active && playback?.playback_url) setPlaybackUrl(playback.playback_url)
    })()
    return () => { active = false }
  }, [taskId])

  // 签名播放 URL 只有 5 分钟有效期:播放器加载失败时重取一次
  const refreshPlaybackUrl = async () => {
    try {
      const playback = await api.getTaskPlaybackUrl(taskId)
      if (playback?.playback_url) {
        setPlaybackUrl(playback.playback_url)
        return playback.playback_url
      }
    } catch { /* 保持失败态 */ }
    return null
  }

  if (loading) {
    return <div className="page"><div className="empty"><b>加载中…</b></div></div>
  }
  if (loadError || !task) {
    return (
      <div className="page">
        <div className="card">
          <div className="empty">
            <Icon name="alert" size="lg" />
            <b>视频加载失败</b>
            <p>{loadError || '任务不存在'}</p>
          </div>
        </div>
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
      suggestions={SUGGESTIONS}
    />
  )
}
