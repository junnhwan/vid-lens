'use client'

import { useState, useEffect, useRef, useCallback } from 'react'
import { useParams, useRouter } from 'next/navigation'
import { Settings2, Plus, Trash2, MessageCircle } from 'lucide-react'
import ChatInput from '@/components/ChatInput'
import ChatShell, { ChatHeader, ChatSidebar, ChatFooter, SidebarSection } from '@/components/chat/ChatShell'
import ChatMessageRow from '@/components/chat/ChatMessageRow'
import AgentTracePanel from '@/components/chat/AgentTracePanel'
import { parseMessages, fmtSession, fmtShortDate, type ChatMsg } from '@/components/chat/chatUtils'
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
  const [topK, setTopK] = useState(8)
  const [showManage, setShowManage] = useState(false)
  const [searchInfo, setSearchInfo] = useState<{ hits?: string; cross?: number }>({})
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
    setSearchInfo({})
    const url = new URLSearchParams(location.search)
    url.set('session', String(sid))
    history.replaceState(null, '', `/kb/${kbId}?${url.toString()}`)
    api.getMessages(sid).then(msgs => setMessages(parseMessages(msgs, colorFor))).catch(() => {})
  }

  const newSession = () => {
    setSession(null)
    setMessages([])
    setActiveTrace([])
    setSearchInfo({})
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
          const taskIds = new Set(cs.map(c => c.task_id))
          setSearchInfo({ hits: `${cs.length}`, cross: taskIds.size })
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
            emptyHint="知识库跨视频 strict_rag 流式问答。"
          />
        }
        header={
          <ChatHeader
            backHref="/kb"
            backLabel="返回知识库"
            kicker={`知识库 KB-${pad(kbId)} · 跨视频严格 RAG`}
            title={kb?.name || '加载中…'}
            actions={!isDemo ? (
              <button
                onClick={() => setShowManage(true)}
                className="h-8 px-3 rounded-lg border border-stone-200 text-[11px] flex items-center gap-1.5 ui-btn-lift hover:bg-stone-50"
              >
                <Settings2 className="w-3.5 h-3.5" />管理成员
              </button>
            ) : undefined}
          />
        }
        sidebar={
          <ChatSidebar>
            <div className="space-y-5">
              <SidebarSection
                title="会话"
                action={
                  <button onClick={newSession} className="text-amber-700 hover:text-amber-900 flex items-center gap-0.5 text-[10px]">
                    <Plus className="w-3 h-3" />新建
                  </button>
                }
              >
                <ul className="space-y-1">
                  {sessions.map(s => (
                    <li key={s.id}>
                      <button
                        onClick={() => switchSession(s.id)}
                        className={`w-full flex items-center gap-2 px-2 py-1.5 rounded-lg text-left text-[12px] ui-row-hover ${
                          session?.id === s.id ? 'bg-amber-50 text-amber-900 font-medium' : 'text-stone-600'
                        }`}
                      >
                        <MessageCircle className="w-3 h-3 shrink-0" />
                        <span className="truncate flex-1">{s.title || `会话 ${s.id}`}</span>
                        <span className="font-mono text-[10px] text-stone-400 shrink-0">{fmtSession(s.created_at)}</span>
                      </button>
                    </li>
                  ))}
                  {sessions.length === 0 && <li className="text-[11px] text-stone-400">暂无会话</li>}
                </ul>
              </SidebarSection>

              <div className="h-px bg-stone-200" />

              <SidebarSection title="知识库">
                <dl className="text-[11px] space-y-1.5">
                  <div className="flex justify-between"><dt className="text-stone-400">视频</dt><dd>{kb?.member_count ?? '—'}</dd></div>
                  <div className="flex justify-between"><dt className="text-stone-400">建立</dt><dd>{kb ? fmtShortDate(kb.created_at) : '—'}</dd></div>
                  {kb?.embedding_model && (
                    <div className="flex justify-between">
                      <dt className="text-stone-400">模型</dt>
                      <dd className="truncate max-w-[100px]">{kb.embedding_model}</dd>
                    </div>
                  )}
                </dl>
              </SidebarSection>

              <div className="h-px bg-stone-200" />

              <SidebarSection title={`成员视频 · ${kb?.videos?.length ?? 0}`}>
                <ul className="space-y-1.5">
                  {(kb?.videos || []).map((v, i) => (
                    <li key={v.task_id} className="flex items-center gap-2 py-1">
                      <span className="src-dot" style={{ background: DOT_COLORS[i % DOT_COLORS.length] }} />
                      <span className="text-[12px] truncate flex-1">{v.title}</span>
                      <span className={`text-[10px] font-mono ${v.retrievable ? 'text-emerald-700' : 'text-stone-400'}`}>
                        {v.retrievable ? '✓' : '—'}
                      </span>
                    </li>
                  ))}
                  {(kb?.videos?.length ?? 0) === 0 && <li className="text-[11px] text-stone-400">暂无成员</li>}
                </ul>
                {!isDemo && (
                  <button
                    onClick={() => setShowManage(true)}
                    className="mt-2 w-full h-7 rounded-lg border border-dashed border-stone-300 text-[10px] text-stone-400 hover:border-amber-400 hover:text-amber-700 flex items-center justify-center gap-1 ui-btn-lift"
                  >
                    <Plus className="w-3 h-3" />添加视频
                  </button>
                )}
              </SidebarSection>

              <div className="h-px bg-stone-200" />

              <SidebarSection title="检索信息">
                <dl className="text-[11px] space-y-1.5">
                  <div className="flex justify-between"><dt className="text-stone-400">命中</dt><dd>{searchInfo.hits ? `${searchInfo.hits} 条` : '—'}</dd></div>
                  <div className="flex justify-between"><dt className="text-stone-400">跨卷</dt><dd>{searchInfo.cross ? `${searchInfo.cross} 卷` : '—'}</dd></div>
                  <div className="flex justify-between"><dt className="text-stone-400">TopK</dt><dd>{topK}</dd></div>
                </dl>
              </SidebarSection>
            </div>
          </ChatSidebar>
        }
        footer={
          <ChatFooter
            hint={`跨 ${kb?.member_count ?? 0} 个视频检索 · 引用标注来源 · 无时间码`}
            footerAction={
              <button
                onClick={() => session && api.deleteSession(session.id).then(() => router.push('/kb'))}
                className="hover:text-red-600 flex items-center gap-1 ui-btn-lift"
              >
                <Trash2 className="w-3 h-3" />清空会话
              </button>
            }
          >
            <ChatInput
              onSend={send}
              onStop={stop}
              streaming={streaming}
              placeholder="就知识库全部视频提问…"
              topK={topK}
              onTopKChange={setTopK}
            />
          </ChatFooter>
        }
      >
        <>
          <div className="pb-2 ui-fade-in">
            <div className="text-[10px] text-stone-400 font-mono">
              Session · {session ? (session.title || `会话 ${session.id}`) : '新会话'}
              {session ? ` · ${new Date(session.created_at).toLocaleString('zh-CN')}` : ''}
            </div>
            <h1 className="text-[24px] font-semibold text-stone-900 mt-1.5 ui-serif">跨视频问答</h1>
            <p className="text-[13px] text-stone-500 mt-1">本会话检索知识库全部视频，引用标注来源视频。</p>
          </div>

          {messages.map((m, i) => (
            <ChatMessageRow
              key={i}
              msg={m}
              idx={i}
              onToggleCite={toggleCite}
              modeLabel="跨视频严格 RAG"
              topK={topK}
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

function pad(n: number) { return n < 10 ? `0${n}` : `${n}` }
