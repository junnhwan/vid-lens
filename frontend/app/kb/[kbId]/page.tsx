'use client'

import { useState, useEffect, useRef, useCallback } from 'react'
import { useParams, useRouter } from 'next/navigation'
import { Settings2, Plus, Trash2, MessageCircle } from 'lucide-react'
import ChatInput from '@/components/ChatInput'
import ChatShell, { ChatHeader, ChatSidebar, ChatFooter, SidebarSection } from '@/components/chat/ChatShell'
import ChatMessageRow from '@/components/chat/ChatMessageRow'
import AgentTracePanel from '@/components/chat/AgentTracePanel'
import { parseMessages, fmtSession, type ChatMsg } from '@/components/chat/chatUtils'
import { streamTraceReducer, type ChatTraceStep } from '@/components/chat/traceTypes'
import KBModal from '@/components/KBModal'
import { CiteRef } from '@/components/Citation'
import { api, streamAsk, ApiError } from '@/lib/api'
import { useRole } from '@/lib/useRole'
import type { KnowledgeBase, Citation, ChatSession } from '@/lib/types'

const DOT_COLORS = ['#9A4A1A', '#2A6B5E', '#3F62C2', '#6B7A2A', '#8C3A4A', '#B8842E', '#5C2A0D', '#1D3468']

export default function KBChatPage() {
  const params = useParams<{ kbId: string }>()
  const kbId = Number(params.kbId)
  const router = useRouter()

  const [kb, setKb] = useState<KnowledgeBase | null>(null)
  const [session, setSession] = useState<ChatSession | null>(null)
  const [sessions, setSessions] = useState<ChatSession[]>([])
  const [messages, setMessages] = useState<ChatMsg[]>([])
  const [activeTrace, setActiveTrace] = useState<ChatTraceStep[]>([])
  const [streaming, setStreaming] = useState(false)
  const topK = 8
  const [showManage, setShowManage] = useState(false)
  const abortRef = useRef<AbortController | null>(null)
  const scrollRef = useRef<HTMLDivElement>(null)
  const { isDemo } = useRole()

  const loadSessions = useCallback(async () => {
    try { setSessions(await api.listSessions({ knowledge_base_id: kbId })) } catch { /* ignore */ }
  }, [kbId])

  const kbRef = useRef(kb)
  useEffect(() => { kbRef.current = kb }, [kb])
  const colorFor = useCallback((taskId: number) => {
    const members = kbRef.current?.videos || []
    const idx = members.findIndex(v => v.task_id === taskId)
    return DOT_COLORS[idx >= 0 ? idx % DOT_COLORS.length : 0]
  }, [])

  useEffect(() => {
    api.getKB(kbId).then(setKb).catch(() => {})
    const init = async () => {
      const url = new URLSearchParams(location.search)
      const sidParam = url.get('session')
      const sid = sidParam ? Number(sidParam) : null
      if (sid) {
        try {
          const s = (await api.listSessions({ knowledge_base_id: kbId })).find(x => x.id === sid) || null
          setSession(s)
          if (s) {
            api.getMessages(sid).then(msgs => setMessages(parseMessages(msgs, colorFor))).catch(() => {})
          }
        } catch { /* ignore */ }
      }
      loadSessions()
    }
    init()
  }, [kbId, loadSessions, colorFor])

  const switchSession = async (sid: number) => {
    const s = sessions.find(x => x.id === sid)
    if (!s || s.id === session?.id) return
    setSession(s)
    setMessages([])
    const url = new URLSearchParams(location.search)
    url.set('session', String(sid))
    history.replaceState(null, '', `/kb/${kbId}?${url.toString()}`)
    api.getMessages(sid).then(msgs => setMessages(parseMessages(msgs, colorFor))).catch(() => {})
  }

  const newSession = () => {
    setSession(null)
    setMessages([])
    setActiveTrace([])
    const url = new URLSearchParams(location.search)
    url.delete('session')
    history.replaceState(null, '', `/kb/${kbId}?${url.toString()}`)
  }

  useEffect(() => {
    const el = scrollRef.current
    if (el) el.scrollTop = el.scrollHeight
  }, [messages, streaming])

  const appendDelta = useCallback((delta: string) => {
    setMessages(prev => {
      if (prev.length === 0) return prev
      const next = [...prev]
      const last = next[next.length - 1]
      if (last && last.role === 'assistant') next[next.length - 1] = { ...last, content: last.content + delta, streaming: true }
      return next
    })
  }, [])

  const send = useCallback(async (q: string) => {
    if (streaming) return
    let sid = session?.id
    if (!sid) {
      try {
        const s = await api.createSession({ knowledge_base_id: kbId, scope_type: 'knowledge_base' })
        setSession(s)
        sid = s.id
        const url = new URLSearchParams(location.search)
        url.set('session', String(sid))
        history.replaceState(null, '', `/kb/${kbId}?${url.toString()}`)
        loadSessions()
      } catch (e) {
        setMessages(prev => [...prev, { role: 'user', content: q }, { role: 'assistant', content: '', error: e instanceof ApiError ? e.message : '创建会话失败' }])
        return
      }
    }
    const ctrl = new AbortController()
    abortRef.current = ctrl
    setStreaming(true)

    const startTrace = streamTraceReducer([], 'start')
    setActiveTrace(startTrace)
    let answerStarted = false

    setMessages(prev => [
      ...prev,
      { role: 'user', content: q },
      { role: 'assistant', content: '', cites: [], openCiteIds: [], streaming: true, trace: startTrace },
    ])

    const patchLast = (patch: Partial<ChatMsg>) => {
      setMessages(prev => {
        if (prev.length === 0) return prev
        const next = [...prev]
        const last = next[next.length - 1]
        if (last && last.role === 'assistant') next[next.length - 1] = { ...last, ...patch }
        return next
      })
    }

    const bumpTrace = (
      event: Parameters<typeof streamTraceReducer>[1],
      payload?: Parameters<typeof streamTraceReducer>[2],
    ) => {
      setActiveTrace(prev => {
        const next = streamTraceReducer(prev, event, payload)
        patchLast({ trace: next })
        return next
      })
    }

    try {
      await streamAsk(sid!, q, topK, 'strict_rag', {
        onAnswer: (delta) => {
          appendDelta(delta)
          if (!answerStarted) {
            answerStarted = true
            bumpTrace('answer')
          }
        },
        onCitations: (cs: Citation[]) => {
          const refs: CiteRef[] = cs.map((c, i) => ({
            id: `C${i + 1}`,
            chunkIndex: c.chunk_index,
            score: c.score,
            content: c.content,
            source: c.source,
            videoTitle: c.video_title,
            finalRank: c.final_rank,
            color: colorFor(c.task_id),
          }))
          patchLast({ cites: refs })
          const sources = [...new Set(cs.map(c => c.video_title).filter(Boolean))] as string[]
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
      patchLast({ streaming: false, error: e instanceof ApiError ? e.message : '流式请求失败' })
    } finally {
      setStreaming(false)
      abortRef.current = null
    }
  }, [session?.id, streaming, topK, appendDelta, colorFor, loadSessions, kbId])

  const stop = () => { abortRef.current?.abort(); setStreaming(false) }

  const clearSession = async () => {
    if (!session) return
    if (!window.confirm('确认清空当前会话的所有消息？此操作不可撤销。')) return
    try {
      await api.deleteSession(session.id)
      router.push('/kb')
    } catch { /* ignore */ }
  }

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

  return (
    <>
      <ChatShell
        scrollRef={scrollRef}
        tracePanel={
          <AgentTracePanel
            steps={activeTrace}
            streaming={streaming}
            source="inferred"
            emptyHint="知识库跨视频检索。"
          />
        }
        header={
          <ChatHeader
            backHref="/kb"
            backLabel="知识库"
            kicker="跨视频问答"
            title={kb?.name || '加载中…'}
            actions={!isDemo ? (
              <button
                onClick={() => setShowManage(true)}
                className="h-8 px-3 rounded-lg border border-ink-0/10 text-[11px] flex items-center gap-1.5 ui-btn-lift hover:bg-paper-1"
              >
                <Settings2 className="w-3.5 h-3.5" />管理成员
              </button>
            ) : undefined}
          />
        }
        sidebar={
          <ChatSidebar>
            <div className="space-y-6">
              <SidebarSection
                title="会话"
                action={
                  <button onClick={newSession} className="text-sienna-700 hover:text-sienna-600 flex items-center gap-0.5 text-[11px]">
                    <Plus className="w-3 h-3" />新建
                  </button>
                }
              >
                <ul className="space-y-0.5">
                  {sessions.map(s => (
                    <li key={s.id}>
                      <button
                        onClick={() => switchSession(s.id)}
                        title={fmtSession(s.created_at)}
                        className={`w-full flex items-center gap-2 px-2 py-1.5 rounded-lg text-left text-[13px] ui-row-hover ${
                          session?.id === s.id ? 'bg-sienna-500/8 text-sienna-800 font-medium' : 'text-ink-3'
                        }`}
                      >
                        <MessageCircle className="w-3 h-3 shrink-0" />
                        <span className="truncate">{s.title || '新会话'}</span>
                      </button>
                    </li>
                  ))}
                  {sessions.length === 0 && <li className="text-[12px] text-ink-4 px-2 py-1">还没有会话</li>}
                </ul>
              </SidebarSection>

              <SidebarSection title={`成员 · ${kb?.videos?.length ?? 0}`}>
                <ul className="space-y-1.5">
                  {(kb?.videos || []).map((v, i) => (
                    <li key={v.task_id} className="flex items-center gap-2 py-0.5">
                      <span className="src-dot" style={{ background: DOT_COLORS[i % DOT_COLORS.length] }} />
                      <span className="text-[12px] truncate flex-1">{v.title}</span>
                    </li>
                  ))}
                  {(kb?.videos?.length ?? 0) === 0 && <li className="text-[12px] text-ink-4">暂无成员</li>}
                </ul>
                {!isDemo && (
                  <button
                    onClick={() => setShowManage(true)}
                    className="mt-2 w-full h-7 rounded-lg border border-dashed border-ink-0/15 text-[11px] text-ink-4 hover:border-sienna-500/40 hover:text-sienna-700 flex items-center justify-center gap-1 ui-btn-lift"
                  >
                    <Plus className="w-3 h-3" />添加视频
                  </button>
                )}
              </SidebarSection>
            </div>
          </ChatSidebar>
        }
        footer={
          <ChatFooter
            footerAction={
              session ? (
                <button
                  onClick={clearSession}
                  className="hover:text-rust flex items-center gap-1 ui-btn-lift"
                >
                  <Trash2 className="w-3 h-3" />清空会话
                </button>
              ) : undefined
            }
          >
            <ChatInput
              onSend={send}
              onStop={stop}
              streaming={streaming}
              placeholder="就知识库里的视频提问…"
            />
          </ChatFooter>
        }
      >
        <>
          {messages.length === 0 && (
            <p className="text-[13px] text-ink-4 ui-fade-in">
              会在知识库全部视频里检索，引用会标出来源。
            </p>
          )}

          {messages.map((m, i) => (
            <ChatMessageRow
              key={i}
              msg={m}
              idx={i}
              onToggleCite={toggleCite}
              modeLabel="跨视频问答"
            />
          ))}
        </>
      </ChatShell>

      {showManage && kb && (
        <KBModal
          mode="manage"
          kb={kb}
          onClose={async () => { setShowManage(false); try { setKb(await api.getKB(kbId)) } catch {} }}
          onChanged={async () => { try { setKb(await api.getKB(kbId)) } catch {} }}
        />
      )}
    </>
  )
}
