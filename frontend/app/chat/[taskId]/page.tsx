'use client'

import { useState, useEffect, useRef, useCallback, Suspense } from 'react'
import { useParams, useRouter } from 'next/navigation'
import { Database, AlertTriangle, Trash2 } from 'lucide-react'
import ChatInput from '@/components/ChatInput'
import ChatShell, { ChatHeader, ChatSidebar, ChatFooter, ChatModePicker, modeLabel as chatModeLabel } from '@/components/chat/ChatShell'
import ChatMessageRow from '@/components/chat/ChatMessageRow'
import AgentLensOverlay from '@/components/chat/AgentLensOverlay'
import { parseMessages, fmtSession, type ChatMsg } from '@/components/chat/chatUtils'
import {
  agentTraceReducer,
  emptyAgentTraceState,
  streamTraceReducer,
  type AgentSSEPayload,
  type AgentTraceState,
  type ChatTraceStep,
} from '@/components/chat/traceTypes'
import { CiteRef } from '@/components/Citation'
import { useToast } from '@/components/Toast'
import { api, streamAsk, streamAgent, ApiError } from '@/lib/api'
import { taskTitle } from '@/lib/format'
import type { VideoTask, ChatSession, Citation, VideoChatMode } from '@/lib/types'

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
  const [session, setSession] = useState<ChatSession | null>(null)
  const [sessions, setSessions] = useState<ChatSession[]>([])
  const [ragStatus, setRagStatus] = useState<{ indexed: boolean; chunks: number } | null>(null)
  const [mode, setMode] = useState<VideoChatMode>('strict_rag')
  const topK = 4
  const [messages, setMessages] = useState<ChatMsg[]>([])
  const [ragTrace, setRagTrace] = useState<ChatTraceStep[]>([])
  const [agentTrace, setAgentTrace] = useState<AgentTraceState>(emptyAgentTraceState())
  const [streaming, setStreaming] = useState(false)
  const [failClosed, setFailClosed] = useState(false)
  const [sessionReady, setSessionReady] = useState(false)
  const abortRef = useRef<AbortController | null>(null)
  const scrollRef = useRef<HTMLDivElement>(null)
  const toast = useToast()

  const loadSessions = useCallback(async () => {
    try { setSessions(await api.listSessions({ task_id: taskId })) } catch { /* ignore */ }
  }, [taskId])

  useEffect(() => {
    api.getTask(taskId).then(setTask).catch(() => {})
    api.getRagIndex(taskId).then(r => setRagStatus({ indexed: r.indexed, chunks: r.chunks })).catch(() => {})

    const init = async () => {
      const url = new URLSearchParams(location.search)
      const modeParam = url.get('mode')
      if (modeParam === 'agent' || modeParam === 'video_assistant' || modeParam === 'strict_rag') {
        setMode(modeParam)
      }
      const sidParam = url.get('session')
      const sid = sidParam ? Number(sidParam) : null
      if (sid) {
        try {
          const list = await api.listSessions({ task_id: taskId })
          const s = list.find(x => x.id === sid) || null
          setSession(s)
          if (s) {
            api.getMessages(sid).then(msgs => setMessages(parseMessages(msgs))).catch(() => {})
          }
        } catch { /* ignore */ }
      }
      setSessionReady(true)
      loadSessions()
    }
    init()
  }, [taskId, loadSessions])

  const switchSession = async (sid: number) => {
    const s = sessions.find(x => x.id === sid)
    if (!s || s.id === session?.id) return
    setSession(s)
    setMessages([])
    const url = new URLSearchParams(location.search)
    url.set('session', String(sid))
    history.replaceState(null, '', `/chat/${taskId}?${url.toString()}`)
    api.getMessages(sid).then(msgs => setMessages(parseMessages(msgs))).catch(() => {})
  }

  const newSession = () => {
    setSession(null)
    setMessages([])
    setRagTrace([])
    setAgentTrace(emptyAgentTraceState())
    setFailClosed(false)
    const url = new URLSearchParams(location.search)
    url.delete('session')
    const qs = url.toString()
    history.replaceState(null, '', qs ? `/chat/${taskId}?${qs}` : `/chat/${taskId}`)
  }

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

  const appendDelta = useCallback((delta: string) => {
    setMessages(prev => {
      if (prev.length === 0) return prev
      const next = [...prev]
      const last = next[next.length - 1]
      if (last && last.role === 'assistant') {
        next[next.length - 1] = { ...last, content: last.content + delta, streaming: true }
      }
      return next
    })
  }, [])

  const send = useCallback(async (q: string) => {
    if (streaming) return
    if ((mode === 'strict_rag' || mode === 'agent') && ragStatus && !ragStatus.indexed) {
      setFailClosed(true)
      return
    }

    let sid = session?.id
    if (!sid) {
      try {
        const s = await api.createSession({ task_id: taskId, scope_type: 'video' })
        setSession(s)
        sid = s.id
        const url = new URLSearchParams(location.search)
        url.set('session', String(sid))
        history.replaceState(null, '', `/chat/${taskId}?${url.toString()}`)
        loadSessions()
      } catch (e) {
        setMessages(prev => [...prev, { role: 'user', content: q }, { role: 'assistant', content: '', error: e instanceof ApiError ? e.message : '创建会话失败' }])
        return
      }
    }

    const ctrl = new AbortController()
    abortRef.current = ctrl
    setStreaming(true)
    setFailClosed(false)

    const patchLast = (patch: Partial<ChatMsg>) => {
      setMessages(prev => {
        if (prev.length === 0) return prev
        const next = [...prev]
        const last = next[next.length - 1]
        if (last && last.role === 'assistant') next[next.length - 1] = { ...last, ...patch }
        return next
      })
    }

    const mapCitations = (cs: Citation[]): CiteRef[] => cs.map((c, i) => ({
      id: `C${i + 1}`,
      chunkIndex: c.chunk_index,
      score: c.score,
      content: c.content,
      source: c.source,
      videoTitle: c.video_title,
      finalRank: c.final_rank,
    }))

    if (isAgentMode) {
      const initial = emptyAgentTraceState()
      setAgentTrace(initial)
      setMessages(prev => [
        ...prev,
        { role: 'user', content: q },
        { role: 'assistant', content: '', cites: [], openCiteIds: [], streaming: true, trace: [], agentRun: true },
      ])

      const bumpAgent = (event: AgentSSEPayload) => {
        setAgentTrace(prev => {
          const next = agentTraceReducer(prev, event)
          patchLast({ trace: next.steps, agentRun: true })
          return next
        })
      }

      try {
        await streamAgent(sid!, q, { top_k: topK, mode: 'agent' }, {
          onRunStart: d => bumpAgent({ type: 'run_start', data: d }),
          onStepStart: d => bumpAgent({ type: 'step_start', data: d }),
          onStepDone: d => bumpAgent({ type: 'step_done', data: d }),
          onStepError: d => bumpAgent({ type: 'step_error', data: d }),
          onToolCall: d => bumpAgent({ type: 'tool_call', data: d }),
          onToolResult: d => bumpAgent({ type: 'tool_result', data: d }),
          onRetrieveHits: d => bumpAgent({ type: 'retrieve_hits', data: d }),
          onAnswer: delta => appendDelta(delta),
          onCitations: cs => patchLast({ cites: mapCitations(cs) }),
          onDone: () => {
            bumpAgent({ type: 'done' })
            patchLast({ streaming: false })
          },
          onError: e => {
            bumpAgent({ type: 'error', data: { message: e.message, step_id: e.step_id } })
            patchLast({ streaming: false, error: e.message })
          },
        }, ctrl.signal)
      } catch (e) {
        if (e instanceof DOMException && e.name === 'AbortError') {
          patchLast({ streaming: false })
        } else {
          const msg = e instanceof ApiError ? e.message : 'Agent 流式请求失败'
          patchLast({ streaming: false, error: msg })
        }
      } finally {
        setStreaming(false)
        abortRef.current = null
      }
      return
    }

    const startTrace = streamTraceReducer([], 'start')
    setRagTrace(startTrace)
    let answerStarted = false

    setMessages(prev => [
      ...prev,
      { role: 'user', content: q },
      { role: 'assistant', content: '', cites: [], openCiteIds: [], streaming: true, trace: startTrace },
    ])

    const bumpTrace = (
      event: Parameters<typeof streamTraceReducer>[1],
      payload?: Parameters<typeof streamTraceReducer>[2],
    ) => {
      setRagTrace(prev => {
        const next = streamTraceReducer(prev, event, payload)
        patchLast({ trace: next })
        return next
      })
    }

    try {
      await streamAsk(sid!, q, topK, mode, {
        onAnswer: (delta) => {
          appendDelta(delta)
          if (!answerStarted) {
            answerStarted = true
            bumpTrace('answer')
          }
        },
        onCitations: (cs: Citation[]) => {
          patchLast({ cites: mapCitations(cs) })
          const sources = [...new Set(cs.map(c => c.video_title || c.source).filter(Boolean))] as string[]
          bumpTrace('citations', { hits: cs.length, sources })
        },
        onDone: (d) => {
          bumpTrace('done')
          patchLast({ ...(d.answer ? { content: d.answer } : {}), streaming: false, degraded: d.degraded })
        },
        onError: (e) => {
          bumpTrace('error', { error: e.message })
          patchLast({ streaming: false, error: e.message })
        },
      }, ctrl.signal)
    } catch (e) {
      if (e instanceof DOMException && e.name === 'AbortError') {
        patchLast({ streaming: false })
      } else {
        const msg = e instanceof ApiError ? e.message : '流式请求失败'
        patchLast({ streaming: false, error: msg })
      }
    } finally {
      setStreaming(false)
      abortRef.current = null
    }
  }, [session?.id, streaming, mode, isAgentMode, ragStatus, topK, appendDelta, loadSessions, taskId])

  const stop = () => { abortRef.current?.abort(); setStreaming(false) }

  const toggleCite = (msgIdx: number, id: string) => {
    setMessages(prev => {
      const next = [...prev]
      const m = next[msgIdx]
      if (!m) return prev
      const open = m.openCiteIds || []
      next[msgIdx] = { ...m, openCiteIds: open.includes(id) ? open.filter(x => x !== id) : [...open, id] }
      return next
    })
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
