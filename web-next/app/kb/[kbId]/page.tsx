'use client'

import { useState, useEffect, useRef, useCallback } from 'react'
import { useParams, useRouter } from 'next/navigation'
import Link from 'next/link'
import { ArrowLeft, BookOpen, Settings2, Plus, Trash2, Database, MessageCircle } from 'lucide-react'
import ThemeSwitch from '@/components/ThemeSwitch'
import ChatInput from '@/components/ChatInput'
import KBModal from '@/components/KBModal'
import { CiteRef, CitationCards, renderAnswerWithCites, citesFromSnapshot } from '@/components/Citation'
import { api, streamAsk, ApiError } from '@/lib/api'
import { useRole } from '@/lib/useRole'
import type { KnowledgeBase, Citation, ChatSession, ChatMessage } from '@/lib/types'

// /kb/:kbId — 跨视频问答。KB scope 自带严格 RAG 语义。
// 左 sticky 栏：知识库成员视频列表(色点) + 引用按来源分组 + 跨卷检索元信息。

interface Msg {
  role: 'user' | 'assistant'
  content: string
  cites?: CiteRef[]
  openCiteIds?: string[]
  streaming?: boolean
  degraded?: boolean
  error?: string
}

// 成员视频色点调色板（mockup 用固定色，按成员序号取）
const DOT_COLORS = ['#9A4A1A', '#2A6B5E', '#3F62C2', '#6B7A2A', '#8C3A4A', '#B8842E', '#5C2A0D', '#1D3468']

export default function KBChatPage() {
  const params = useParams<{ kbId: string }>()
  const kbId = Number(params.kbId)
  const router = useRouter()

  const [kb, setKb] = useState<KnowledgeBase | null>(null)
  const [session, setSession] = useState<ChatSession | null>(null)
  const [sessions, setSessions] = useState<ChatSession[]>([])
  const [messages, setMessages] = useState<Msg[]>([])
  const [streaming, setStreaming] = useState(false)
  const [topK, setTopK] = useState(8) // 跨视频默认 topK 大些
  const [showManage, setShowManage] = useState(false)
  const [searchInfo, setSearchInfo] = useState<{ hits?: string; cross?: number }>({})
  const abortRef = useRef<AbortController | null>(null)
  const scrollRef = useRef<HTMLDivElement>(null)
  // 演示账号只读：隐藏管理成员/添加视频写入口，仅保留查看与问答。
  const { isDemo } = useRole()

  // 拉取本知识库的全部会话（会话列表/切换用）
  const loadSessions = useCallback(async () => {
    try { setSessions(await api.listSessions({ knowledge_base_id: kbId })) } catch { /* ignore */ }
  }, [kbId])

  // 成员 → 色点映射。用 ref 读最新 kb，函数保持稳定，供 init/切换/流式复用。
  const kbRef = useRef(kb)
  useEffect(() => { kbRef.current = kb }, [kb])
  const colorFor = useCallback((taskId: number) => {
    const members = kbRef.current?.videos || []
    const idx = members.findIndex(v => v.task_id === taskId)
    return DOT_COLORS[idx >= 0 ? idx % DOT_COLORS.length : 0]
  }, [])

  // 初始化：拉 KB 详情 + 复用 URL 里的历史会话（无 ?session= 时不创建，
  // 等到首次发送消息时才懒创建——避免"新建会话但没问答"产生一堆空会话）。
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

  // 切换会话：加载该会话历史并写回 URL（可刷新还原）
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

  // 新建会话：只重置本地状态，不创建后端会话，首次发送时才真正创建
  const newSession = () => {
    setSession(null)
    setMessages([])
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
    // 首次发送时若无会话，先懒创建（避免空会话堆积）
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
    setMessages(prev => [
      ...prev,
      { role: 'user', content: q },
      { role: 'assistant', content: '', cites: [], openCiteIds: [], streaming: true },
    ])

    const patchLast = (patch: Partial<Msg>) => {
      setMessages(prev => {
        if (prev.length === 0) return prev
        const next = [...prev]
        const last = next[next.length - 1]
        if (last && last.role === 'assistant') next[next.length - 1] = { ...last, ...patch }
        return next
      })
    }

    try {
      await streamAsk(sid!, q, topK, 'strict_rag', {
        onAnswer: (delta) => appendDelta(delta),
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
          // 跨卷统计
          const taskIds = new Set(cs.map(c => c.task_id))
          setSearchInfo({ hits: `${cs.length}`, cross: taskIds.size })
        },
        onDone: (d) => patchLast({ ...(d.answer ? { content: d.answer } : {}), streaming: false, degraded: d.degraded }),
        onError: (e) => patchLast({ streaming: false, error: e.message }),
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
    <div className="hidden md:flex flex-col h-screen border-t border-ink-0/15">
      <header className="shrink-0 bg-paper-0 border-b border-ink-0/15">
        <div className="flex items-center px-6 h-14 gap-4">
          <Link href="/kb" className="flex items-center gap-2 text-ink-3 hover:text-ink-0" title="返回知识库列表">
            <ArrowLeft className="w-4 h-4" /><span className="font-sans text-[11px]">返回知识库</span>
          </Link>
          <div className="h-5 w-px bg-ink-0/15" />
          <div className="min-w-0 flex-1">
            <div className="font-sans text-[10px] text-ink-4">知识库 №KB-{pad(kbId)} · 跨视频严格 RAG</div>
            <div className="font-sans text-[16px] font-medium tight text-ink-0 truncate -mt-0.5">{kb?.name || '加载中…'}</div>
          </div>
          {!isDemo && (
            <button onClick={() => setShowManage(true)} className="btn-line h-8 px-3 font-sans text-[11px] flex items-center gap-1.5"><Settings2 className="w-3.5 h-3.5" />管理成员</button>
          )}
          <ThemeSwitch />
        </div>
      </header>

      <main ref={scrollRef} className="flex-1 overflow-y-auto scroll-thin">
        <div className="max-w-[1100px] mx-auto px-8 py-7 flex gap-10">
          {/* 左 sticky：成员视频 + 引用汇总 + 检索元信息 */}
          <aside className="w-[220px] shrink-0 hidden lg:block">
            <div className="sticky top-7 space-y-6">
              {/* 会话列表：多会话保留 + 切换 + 新建 */}
              <div>
                <div className="font-sans text-[10px] text-ink-4 mb-2 flex items-center justify-between">
                  <span>会话</span>
                  <button onClick={newSession} className="text-sienna-700 hover:text-sienna-900 flex items-center gap-0.5" title="新建会话"><Plus className="w-3 h-3" />新建</button>
                </div>
                <ul className="space-y-1">
                  {sessions.map(s => (
                    <li key={s.id}>
                      <button
                        onClick={() => switchSession(s.id)}
                        className={`w-full flex items-center gap-2 px-2 py-1.5 rounded-md text-left text-[12px] ${session?.id === s.id ? 'bg-sienna-500/10 text-sienna-700 font-medium' : 'text-ink-2 hover:bg-ink-2/10'}`}
                      >
                        <MessageCircle className="w-3 h-3 shrink-0" />
                        <span className="truncate flex-1">会话 {s.id}</span>
                        <span className="font-mono text-[10px] text-ink-4 shrink-0">{fmtSession(s.created_at)}</span>
                      </button>
                    </li>
                  ))}
                  {sessions.length === 0 && <li className="font-sans text-[11px] text-ink-4">暂无会话</li>}
                </ul>
              </div>
              <div className="h-px bg-ink-0/15" />
              <div>
                <div className="font-sans text-[10px] text-ink-4 mb-2">知识库</div>
                <dl className="font-mono text-[11px] text-ink-3 space-y-1.5 leading-relaxed">
                  <div className="flex justify-between"><dt className="text-ink-4">视频</dt><dd className="text-ink-2">{kb?.member_count ?? '—'}</dd></div>
                  <div className="flex justify-between"><dt className="text-ink-4">建立</dt><dd className="text-ink-2">{kb ? fmt(kb.created_at) : '—'}</dd></div>
                  {kb?.embedding_model && <div className="flex justify-between"><dt className="text-ink-4">模型</dt><dd className="text-ink-2 truncate max-w-[120px]">{kb.embedding_model}</dd></div>}
                </dl>
              </div>
              <div className="h-px bg-ink-0/15" />
              {/* 成员视频列表 */}
              <div>
                <div className="font-sans text-[10px] text-ink-4 mb-2 flex items-center justify-between">成员视频 <span className="text-ink-3">{kb?.videos?.length ?? 0}</span></div>
                <ul className="space-y-1.5">
                  {(kb?.videos || []).map((v, i) => (
                    <li key={v.task_id} className="flex items-center gap-2 py-1 hover:bg-ink-0/[.03] cursor-pointer">
                      <span className="src-dot" style={{ background: DOT_COLORS[i % DOT_COLORS.length] }} />
                      <span className="font-sans text-[12px] text-ink-1 truncate flex-1">{v.title}</span>
                      <span className={`font-mono text-[10px] ${v.retrievable ? 'text-moss' : 'text-ink-4'}`}>{v.retrievable ? '✓' : '—'}</span>
                    </li>
                  ))}
                  {(kb?.videos?.length ?? 0) === 0 && (
                    <li className="font-sans text-[11px] text-ink-4">暂无成员</li>
                  )}
                </ul>
                {!isDemo && (
                  <button onClick={() => setShowManage(true)} className="mt-2 w-full h-7 border border-dashed border-ink-0/30 font-sans text-[10px] text-ink-4 hover:border-sienna-500 hover:text-sienna-700 flex items-center justify-center gap-1"><Plus className="w-3 h-3" />添加视频</button>
                )}
              </div>
              <div className="h-px bg-ink-0/15" />
              {/* 检索元信息 */}
              <div>
                <div className="font-sans text-[10px] text-ink-4 mb-2">检索信息</div>
                <dl className="font-mono text-[11px] space-y-1.5 leading-relaxed">
                  <div className="flex justify-between"><dt className="text-ink-4">命中</dt><dd className="text-ink-2">{searchInfo.hits ? `${searchInfo.hits} 条` : '—'}</dd></div>
                  <div className="flex justify-between"><dt className="text-ink-4">跨卷</dt><dd className="text-ink-2">{searchInfo.cross ? `${searchInfo.cross} 卷` : '—'}</dd></div>
                  <div className="flex justify-between"><dt className="text-ink-4">TopK</dt><dd className="text-ink-2">{topK}</dd></div>
                </dl>
              </div>
            </div>
          </aside>

          {/* 中：消息流 */}
          <div className="flex-1 min-w-0 max-w-[820px] space-y-7" aria-live="polite">
            <div className="pb-2">
              <div className="font-mono text-[10px] text-ink-4">Session · {session ? new Date(session.created_at).toLocaleString('zh-CN') : ''}</div>
              <h1 className="font-sans text-[28px] leading-tight font-medium tight text-ink-0 mt-1.5">跨视频问答<span className="text-sienna-500">.</span></h1>
              <p className="font-sans italic text-[14px] text-ink-3 mt-1">本会话检索知识库全部视频，引用标注来源视频。</p>
            </div>

            {messages.map((m, i) => (
              <MessageRow key={i} msg={m} idx={i} onToggleCite={toggleCite} topK={topK} />
            ))}
          </div>
        </div>
      </main>

      <footer className="shrink-0 bg-paper-0 border-t border-ink-0/15">
        <div className="max-w-[1100px] mx-auto px-8 py-3.5 flex gap-10">
          <div className="w-[220px] shrink-0 hidden lg:block" />
          <div className="flex-1 max-w-[820px]">
            <ChatInput
              onSend={send}
              onStop={stop}
              streaming={streaming}
              placeholder="就知识库全部视频提问…"
              topK={topK}
              onTopKChange={setTopK}
            />
            <div className="flex items-center justify-between mt-2 font-mono text-[10px]">
              <span className="text-ink-4">跨 {kb?.member_count ?? 0} 个视频检索 · 引用标注来源 · 无时间码</span>
              <button onClick={() => session && api.deleteSession(session.id).then(() => router.push('/kb'))} className="text-ink-4 hover:text-rust flex items-center gap-1"><Trash2 className="w-3 h-3" />清空会话</button>
            </div>
          </div>
        </div>
      </footer>

      {showManage && kb && (
        <KBModal
          mode="manage"
          kb={kb}
          onClose={async () => { setShowManage(false); try { setKb(await api.getKB(kbId)) } catch {} }}
          onChanged={async () => { try { setKb(await api.getKB(kbId)) } catch {} }}
        />
      )}
    </div>
  )
}

function MessageRow({ msg, idx, onToggleCite, topK }: {
  msg: Msg
  idx: number
  onToggleCite: (msgIdx: number, id: string) => void
  topK: number
}) {
  if (msg.role === 'user') {
    return (
      <div className="flex justify-end">
        <div className="bubble-user font-sans text-[14px] leading-[1.7] px-4 py-2.5 max-w-[80%]">{msg.content}</div>
      </div>
    )
  }
  const citeIds = (msg.cites || []).map(c => c.id)
  const toggle = (id: string) => onToggleCite(idx, id)
  return (
    <div className="space-y-1">
      <div className="font-mono text-[10px] text-ink-4 flex items-center gap-2">
        <BookOpen className="w-3 h-3" />VidLens · 跨视频严格 RAG · top_k {topK}
        {msg.streaming && <span className="text-sienna-700 flex items-center gap-1"><span className="w-1.5 h-1.5 rounded-full bg-sienna-500 live" />生成中</span>}
        {msg.degraded && <span className="text-ink-4">（降级）</span>}
      </div>
      {msg.error ? (
        <div className="border border-rust/40 bg-rust/5 px-4 py-2.5 font-sans text-[13px] text-rust">{msg.error}</div>
      ) : (
        <div className={`font-sans text-[15.5px] font-medium leading-[1.8] text-ink-0 ${msg.streaming ? 'caret' : ''}`}>
          {renderAnswerWithCites(msg.content, citeIds, toggle, msg.openCiteIds || [])}
        </div>
      )}
      {msg.cites && msg.cites.length > 0 && <CitationCards refs={msg.cites} openIds={msg.openCiteIds || []} />}
    </div>
  )
}

function pad(n: number) { return n < 10 ? `0${n}` : `${n}` }
function fmt(iso: string) {
  const d = new Date(iso)
  return `${pad(d.getMonth() + 1)}-${pad(d.getDate())}`
}

// 把后端历史消息转成页面 Msg：assistant 消息从 retrieval_snapshot 恢复引用片段。
function parseMessages(msgs: ChatMessage[], colorFor: (taskId: number) => string): Msg[] {
  return msgs.map(m => ({
    role: m.role as 'user' | 'assistant',
    content: m.content,
    openCiteIds: [],
    ...(m.role === 'assistant' ? { cites: citesFromSnapshot(m.retrieval_snapshot, colorFor) } : {}),
  }))
}

// 会话时间：MM-DD HH:mm
function fmtSession(iso: string) {
  const d = new Date(iso)
  return `${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`
}
