'use client'

import { useEffect, useState } from 'react'
import { api, ApiError } from '@/lib/api'
import type { KnowledgeBase } from '@/lib/types'
import { ChatWorkspace } from '@/components/chat/ChatWorkspace'
import { useCrumb } from '@/components/shell/AppShell'
import { LoadingBlock, ErrorState } from '@/components/ui/AsyncState'

// 知识库问答与跨视频研究共用实时会话工作区。

const SUGGESTIONS = [
  '这些视频共同讨论了什么主题?',
  '不同视频里的观点有什么差异?',
  '哪些视频提到了具体数据或结论?',
]

export default function KBChatPage({ params }: { params: { kbId: string } }) {
  const kbId = Number(params.kbId)
  const [kb, setKb] = useState<KnowledgeBase | null>(null)
  const [loading, setLoading] = useState(true)
  const [loadError, setLoadError] = useState('')
  const [reloadKey, setReloadKey] = useState(0)

  useCrumb([
    { label: '知识库', href: '/kb' },
    { label: kb?.name || `知识库 #${kbId}`, href: `/kb/${kbId}` },
    { label: '问答' },
  ])

  useEffect(() => {
    let active = true
    setLoading(true)
    setLoadError('')
    setKb(null)
    void (async () => {
      try {
        const detail = await api.getKB(kbId)
        if (!active) return
        setKb(detail)
      } catch (e) {
        if (!active) return
        setLoadError(e instanceof ApiError ? e.message : '知识库加载失败')
      } finally {
        if (active) setLoading(false)
      }
    })()
    return () => { active = false }
  }, [kbId, reloadKey])

  if (loading) {
    return <div className="page"><LoadingBlock label="正在加载…" variant="card" /></div>
  }
  if (loadError || !kb) {
    return (
      <div className="page">
        <ErrorState message={loadError || '知识库加载失败'} onRetry={() => setReloadKey(k => k + 1)} />
      </div>
    )
  }

  return (
    <ChatWorkspace
      knowledgeBase={kb}
      scopeType="knowledge_base"
      targetId={kbId}
      scopeName={kb.name}
      playbackUrl={null}
      suggestions={SUGGESTIONS}
    />
  )
}
