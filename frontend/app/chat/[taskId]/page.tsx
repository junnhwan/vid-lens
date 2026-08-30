'use client'

import { useState, useEffect, useRef, useCallback, Suspense } from 'react'
import { useParams, useRouter } from 'next/navigation'
import { Database, AlertTriangle, Trash2 } from 'lucide-react'
import ChatInput from '@/components/ChatInput'
import ChatShell, { ChatHeader, ChatSidebar, ChatFooter, ChatModePicker, modeLabel as chatModeLabel } from '@/components/chat/ChatShell'
import ChatMessageRow from '@/components/chat/ChatMessageRow'
import AgentLensOverlay from '@/components/chat/AgentLensOverlay'
import { fmtSession } from '@/components/chat/chatUtils'
import { useConversationSession } from '@/components/chat/useConversationSession'
import { useToast } from '@/components/Toast'
import { api, ApiError } from '@/lib/api'
import { taskTitle } from '@/lib/format'
import type { VideoTask, VideoChatMode } from '@/lib/types'

export default function ChatPage() {
  return (
    <Suspense fallback={<div className="h-screen bg-paper-1" />}>
      <ChatView />
    </Suspense>
  )
}

function ChatView() {
  const params = useParams<{ taskId: string }>()
  const taskId = Number(params.taskId)
  const router = useRouter()

  const [task, setTask] = useState<VideoTask | null>(null)
  const [ragStatus, setRagStatus] = useState<{ indexed: boolean; chunks: number } | null>(null)
  const [mode, setMode] = useState<VideoChatMode>('strict_rag')
  const topK = 4
  const [failClosed, setFailClosed] = useState(false)
  const scrollRef = useRef<HTMLDivElement>(null)
  const toast = useToast()

  useEffect(() => {
    api.getTask(taskId).then(setTask).catch(() => {})
    api.getRagIndex(taskId).then(r => setRagStatus({ indexed: r.indexed, chunks: r.chunks })).catch(() => {})
  }, [taskId])

  useEffect(() => {
    const modeParam = new URLSearchParams(location.search).get('mode')
    if (modeParam === 'agent' || modeParam === 'video_assistant' || modeParam === 'strict_rag') setMode(modeParam)
  }, [taskId])

  const canSend = !((mode === 'strict_rag' || mode === 'agent') && ragStatus != null && !ragStatus.indexed)
  const onBlocked = useCallback(() => setFailClosed(true), [])
  const onBeforeSend = useCallback(() => setFailClosed(false), [])
  const {
    session, sessions, sessionReady, messages, agentTrace, streaming,
    switchSession, newSession: resetSession, send, stop, toggleCite,
  } = useConversationSession({
    scopeType: 'video', targetId: taskId, basePath: `/chat/${taskId}`,
    mode, topK, canSend, onBlocked, onBeforeSend,
  })

  const newSession = useCallback(() => {
    resetSession()
    setFailClosed(false)
  }, [resetSession])

  const changeMode = (next: VideoChatMode) => {
    setMode(next)
    const url = new URLSearchParams(location.search)
    if (next === 'strict_rag') url.delete('mode')
    else url.set('mode', next)
    const qs = url.toString()
    history.replaceState(null, '', qs ? `/chat/${taskId}?${qs}` : `/chat/${taskId}`)
  }

  useEffect(() => {
    const el = scrollRef.current
    if (el) el.scrollTop = el.scrollHeight
  }, [messages, streaming])

  useEffect(() => {
    setFailClosed((mode === 'strict_rag' || mode === 'agent') && ragStatus != null && !ragStatus.indexed)
  }, [mode, ragStatus])

  const isAgentMode = mode === 'agent'

  const triggerIndex = async () => {
    try {
      const r = await api.triggerRagIndex(taskId)
      setRagStatus({ indexed: r.indexed, chunks: r.chunks })
      setFailClosed(false)
    } catch { /* ignore */ }
  }

  const copyAnswer = async (content: string) => {
    if (!content) return
    try {
      await navigator.clipboard.writeText(content)
      toast.success('已复制回答')
    } catch {
      toast.error('复制失败')
    }
  }

  const retryQuestion = (msgIdx: number) => {
    for (let i = msgIdx - 1; i >= 0; i--) {
      if (messages[i].role === 'user') {
        send(messages[i].content)
        return
      }
    }
  }

  const clearSession = async () => {
    if (!session) return
    if (!window.confirm('确认清空当前会话的所有消息？此操作不可撤销。')) return
    try {
      await api.deleteSession(session.id)
      toast.success('会话已清空')
      router.push('/')
    } catch (e) {
      toast.error(e instanceof ApiError ? e.message : '清空失败')
    }
  }

  const modeLabel = chatModeLabel(mode)
  const placeholder = mode === 'agent'
    ? '需要对比、归纳时用 Agent…'
    : mode === 'video_assistant'
      ? '就这部视频随便问问…'
      : '就转写内容提问…'

  const lastAgentMsg = [...messages].reverse().find(m => m.role === 'assistant' && m.agentRun)
  const lensCites = lastAgentMsg?.cites || []

  return (
    <ChatShell
      scrollRef={scrollRef}
      overlay={
        isAgentMode ? (
          <AgentLensOverlay steps={agentTrace.steps} cites={lensCites} />
        ) : undefined
      }
      header={
        <ChatHeader
          backHref="/"
          backLabel="视频库"
          kicker="单视频问答"
          title={task ? taskTitle(task) : '加载中…'}
        />
      }
      sidebar={
        <ChatSidebar>
          <div className="h-14 px-4 border-b border-ink-0/8 flex items-center justify-between shrink-0">
            <span className="text-[13px] font-medium text-ink-2">会话</span>
            <button onClick={newSession} className="text-[12px] text-sienna-700 hover:text-sienna-600" title="新建会话">
              新建
            </button>
          </div>
          <div className="flex-1 overflow-y-auto px-2 py-2">
            <ul className="space-y-0.5">
              {sessions.map(s => (
                <li key={s.id}>
                  <button
                    onClick={() => switchSession(s.id)}
                    title={fmtSession(s.created_at)}
                    className={`w-full flex items-center gap-2 px-2.5 py-2 rounded-lg text-left text-[13px] ui-row-hover ${
                      session?.id === s.id ? 'bg-sienna-500/8 text-sienna-800 font-medium' : 'text-ink-3'
                    }`}
                  >
                    <span className="truncate">{s.title || '新会话'}</span>
                  </button>
                </li>
              ))}
              {sessions.length === 0 && <li className="text-[12px] text-ink-4 px-2.5 py-2">还没有会话</li>}
            </ul>
          </div>
        </ChatSidebar>
      }
      footer={
        <ChatFooter
          footerAction={
            session ? (
              <button onClick={clearSession} className="text-ink-4 hover:text-rust flex items-center gap-1 text-[11px]">
                <Trash2 className="w-3 h-3" />清空会话
              </button>
            ) : undefined
          }
        >
          <ChatInput
            onSend={send}
            onStop={stop}
            streaming={streaming}
            placeholder={placeholder}
            leading={<ChatModePicker mode={mode} onChange={changeMode} disabled={streaming} />}
          />
        </ChatFooter>
      }
    >
      <>
        {messages.length === 0 && sessionReady && !failClosed && (
          <p className="text-[13px] text-ink-4 ui-fade-in">
            {mode === 'agent'
              ? 'Agent 会分步检索并归纳。适合对比、跨片段的问题。'
              : '就这部视频的转写提问。回答会附带来源。'}
          </p>
        )}

        {!sessionReady ? (
          <div className="space-y-2">
            <div className="h-4 w-1/3 bg-paper-2 rounded animate-pulse" />
            <div className="h-4 w-2/3 bg-paper-2 rounded animate-pulse" />
          </div>
        ) : (
          messages.map((m, i) => (
            <ChatMessageRow
              key={i}
              msg={m}
              idx={i}
              onToggleCite={toggleCite}
              modeLabel={modeLabel}
              onCopy={copyAnswer}
              onRetry={retryQuestion}
            />
          ))
        )}

        {failClosed && (
          <div className="flex gap-3 p-4 rounded-xl bg-red-50 border border-red-200 ui-fade-in">
            <AlertTriangle className="w-5 h-5 text-red-600 shrink-0" />
            <div>
              <div className="text-[14px] font-medium text-red-800">还没有建立检索索引</div>
              <p className="text-[12px] text-red-700/80 mt-1">
                {mode === 'agent' ? 'Agent' : '引用问答'}需要先索引转写。建好后就可以带着来源回答。
              </p>
              <button
                onClick={triggerIndex}
                className="mt-2 h-8 px-3 rounded-lg border border-red-300 text-[11px] text-red-700 flex items-center gap-1 hover:bg-red-50"
              >
                <Database className="w-3 h-3" />建立索引
              </button>
            </div>
          </div>
        )}
      </>
    </ChatShell>
  )
}
