'use client'

import { useEffect, useState } from 'react'
import { api, ApiError } from '@/lib/api'
import type { KnowledgeBase } from '@/lib/types'
import { ChatWorkspace } from '@/components/chat/ChatWorkspace'
import { useCrumb } from '@/components/shell/AppShell'
import { Icon } from '@/components/ui/Icon'

// 知识库问答(/chat/kb/:id)。后端只支持 strict 快速问答(跨视频检索),
// Agent/研究/漏斗在 UI 以禁用态呈现,不做假象。

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

  useCrumb(['知识库', kb?.name || `知识库 #${kbId}`, '问答'])

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
  }, [kbId])

  if (loading) {
    return <div className="page"><div className="empty"><b>加载中…</b></div></div>
  }
  if (loadError || !kb) {
    return (
      <div className="page">
        <div className="card">
          <div className="empty">
            <Icon name="alert" size="lg" />
            <b>知识库加载失败</b>
            <p>{loadError || '知识库不存在'}</p>
          </div>
        </div>
      </div>
    )
  }

  return (
    <ChatWorkspace
      scopeType="knowledge_base"
      targetId={kbId}
      scopeName={kb.name}
      playbackUrl={null}
      suggestions={SUGGESTIONS}
    />
  )
}
