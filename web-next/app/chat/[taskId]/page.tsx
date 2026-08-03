'use client'

import { useState, useEffect, useRef, useCallback, Suspense } from 'react'
import { useParams, useRouter } from 'next/navigation'
import Link from 'next/link'
import { ArrowLeft, Crosshair, BookOpen, Database, AlertTriangle, Copy, RefreshCw, Trash2, MessageCircle, Plus } from 'lucide-react'
import ThemeSwitch from '@/components/ThemeSwitch'
import UserMenu from '@/components/UserMenu'
import ChatInput from '@/components/ChatInput'
import { CiteRef, CitationCards, renderAnswerWithCites, citesFromSnapshot } from '@/components/Citation'
import { useToast } from '@/components/Toast'
import { api, streamAsk, ApiError } from '@/lib/api'
import type { VideoTask, ChatSession, ChatMessage, Citation, ChatMode } from '@/lib/types'

// /chat/:taskId — 单视频问答。
// 820 限宽消息流 + 左 sticky 元信息栏。SSE: answer 增量 / citations / done / error。
// mode: video_assistant(默认宽松) / strict_rag(强制 RAG，无索引 fail closed)。

interface Msg {
  role: 'user' | 'assistant'
  content: string
  cites?: CiteRef[]
  openCiteIds?: string[]
  streaming?: boolean
  degraded?: boolean
  error?: string
}

export default function ChatPage() {
  return (
    <Suspense fallback={<div className="flex-1" />}>
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
  const [mode, setMode] = useState<ChatMode>('strict_rag') // mockup 默认严格 RAG
  const [topK, setTopK] = useState(4)
  const [messages, setMessages] = useState<Msg[]>([])
  const [streaming, setStreaming] = useState(false)
  const [failClosed, setFailClosed] = useState(false)
  const [sessionReady, setSessionReady] = useState(false)
  const abortRef = useRef<AbortController | null>(null)
  const scrollRef = useRef<HTMLDivElement>(null)
  const toast = useToast()

  // 拉取本视频的全部会话（会话列表/切换用）
  const loadSessions = useCallback(async () => {
    try { setSessions(await api.listSessions({ task_id: taskId })) } catch { /* ignore */ }
  }, [taskId])

  // 初始化：拉 task + RAG 状态 + 复用 URL 里的历史会话（无 ?session= 时不创建，
  // 等到首次发送消息时才懒创建——避免"新建会话但没问答"产生一堆空会话）。
  useEffect(() => {
    api.getTask(taskId).then(setTask).catch(() => {})
    api.getRagIndex(taskId).then(r => setRagStatus({ indexed: r.indexed, chunks: r.chunks })).catch(() => {})

    const init = async () => {
      const url = new URLSearchParams(location.search)
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

  // 切换会话：加载该会话历史并写回 URL（可刷新还原）
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

  // 新建会话：只重置本地状态，不创建后端会话，首次发送时才真正创建
  const newSession = () => {
    setSession(null)
    setMessages([])
    setFailClosed(false)
    const url = new URLSearchParams(location.search)
    url.delete('session')
    history.replaceState(null, '', `/chat/${taskId}?${url.toString()}`)
  }

  // 自动滚到底
  useEffect(() => {
    const el = scrollRef.current
    if (el) el.scrollTop = el.scrollHeight
  }, [messages, streaming])

  // strict_rag 无索引 → fail closed
  useEffect(() => {
    setFailClosed(mode === 'strict_rag' && ragStatus != null && !ragStatus.indexed)
  }, [mode, ragStatus])

  const triggerIndex = async () => {
    try {
      const r = await api.triggerRagIndex(taskId)
      setRagStatus({ indexed: r.indexed, chunks: r.chunks })
      setFailClosed(false)
    } catch { /* ignore */ }
  }

  // 累加 delta 到最后一条 AI 消息
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

  // 发送：SSE 流式。首次发送时若尚无会话，先懒创建（避免空会话堆积）。
  const send = useCallback(async (q: string) => {
    if (streaming) return
    if (mode === 'strict_rag' && ragStatus && !ragStatus.indexed) { setFailClosed(true); return }

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
      await streamAsk(sid!, q, topK, mode, {
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
          }))
          patchLast({ cites: refs })
        },
        onDone: (d) => patchLast({ ...(d.answer ? { content: d.answer } : {}), streaming: false, degraded: d.degraded }),
        onError: (e) => patchLast({ streaming: false, error: e.message }),
      }, ctrl.signal)
    } catch (e) {
      // 用户主动 Stop 触发的 abort：静默收尾，不显示错误
      if (e instanceof DOMException && e.name === 'AbortError') {
        patchLast({ streaming: false })
      } else {
        patchLast({ streaming: false, error: e instanceof ApiError ? e.message : '流式请求失败' })
      }
    } finally {
      setStreaming(false)
      abortRef.current = null
    }
  }, [session?.id, streaming, mode, ragStatus, topK, appendDelta, loadSessions, taskId])

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

  // 复制某条 AI 回答
  const copyAnswer = async (content: string) => {
    if (!content) return
    try {
      await navigator.clipboard.writeText(content)
      toast.success('已复制回答')
    } catch {
      toast.error('复制失败')
    }
  }

  // 重试某条：找到它对应的上一条用户提问重发
  const retryQuestion = (msgIdx: number) => {
    // 找到这条 assistant 之前最近的 user 消息
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

  return (
    <div className="hidden md:flex flex-col h-screen border-t border-ink-0/15">
      {/* Masthead */}
      <header className="shrink-0 bg-paper-0 border-b border-ink-0/15">
        <div className="flex items-center px-6 h-14 gap-4">
          <Link href="/" className="flex items-center gap-2 text-ink-3 hover:text-ink-0" title="返回视频库">
            <ArrowLeft className="w-4 h-4" /><span className="font-sans text-[11px]">返回视频库</span>
          </Link>
          <div className="h-5 w-px bg-ink-0/15" />
          <div className="min-w-0 flex-1">
            <div className="font-sans text-[10px] text-ink-4">任务 #{taskId} · 问答</div>
            <div className="font-sans text-[16px] font-medium tight text-ink-0 truncate -mt-0.5">{task?.title || task?.filename || '加载中…'}</div>
          </div>
          <div className="flex items-center border border-ink-0/20">
            <button onClick={() => setMode('video_assistant')} className={`px-2.5 h-7 font-sans text-[10px] ${mode === 'video_assistant' ? 'bg-ink-0 text-paper-0' : 'text-ink-3 hover:text-ink-0'}`}>普通</button>
            <button onClick={() => setMode('strict_rag')} className={`px-2.5 h-7 font-sans text-[10px] flex items-center gap-1.5 ${mode === 'strict_rag' ? 'bg-ink-0 text-paper-0' : 'text-ink-3 hover:text-ink-0'}`}>
              <Crosshair className="w-3 h-3" />严格 RAG
            </button>
          </div>
          <ThemeSwitch />
          <UserMenu />
        </div>
      </header>

      {/* 主体 */}
      <main ref={scrollRef} className="flex-1 overflow-y-auto scroll-thin">
        <div className="max-w-[1100px] mx-auto px-8 py-7 flex gap-10">
          {/* 左 sticky */}
          <aside className="w-[200px] shrink-0 hidden lg:block">
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
                        <span className="truncate flex-1">{s.title || `会话 ${s.id}`}</span>
                        <span className="font-mono text-[10px] text-ink-4 shrink-0">{fmtSession(s.created_at)}</span>
                      </button>
                    </li>
                  ))}
                  {sessions.length === 0 && <li className="font-sans text-[11px] text-ink-4">暂无会话</li>}
                </ul>
              </div>
              <div className="h-px bg-ink-0/15" />
              <div>
                <div className="font-sans text-[10px] text-ink-4 mb-2">本视频</div>
                <dl className="font-mono text-[11px] text-ink-3 space-y-1.5 leading-relaxed">
                  <div className="flex justify-between"><dt className="text-ink-4">№</dt><dd className="text-ink-2">{taskId}</dd></div>
                  <div className="flex justify-between"><dt className="text-ink-4">索引</dt><dd className={ragStatus?.indexed ? 'text-moss' : 'text-ink-4'}>{ragStatus?.indexed ? `${ragStatus.chunks} chunks` : '未索引'}</dd></div>
                  {task?.transcription && <div className="flex justify-between"><dt className="text-ink-4">转写</dt><dd className="text-ink-2">{task.transcription.words} 词</dd></div>}
                </dl>
              </div>
              <div className="h-px bg-ink-0/15" />
              <div>
                <div className="font-sans text-[10px] text-ink-4 mb-1">当前模式</div>
                <div className="font-mono text-[11px] text-ink-2">{mode === 'strict_rag' ? '严格 RAG' : '普通问答'} · top_k {topK}</div>
                {mode === 'strict_rag' && ragStatus && !ragStatus.indexed && (
                  <div className="font-sans text-[10px] text-rust mt-1">无索引 → fail closed</div>
                )}
              </div>
            </div>
          </aside>

          {/* 中：消息流 */}
          <div className="flex-1 min-w-0 max-w-[820px] space-y-7" aria-live="polite">
            <div className="text-center pb-2">
              <div className="font-mono text-[10px] text-ink-4">Session · {session ? (session.title || `会话 ${session.id}`) : '新会话'}{session ? ` · ${new Date(session.created_at).toLocaleString('zh-CN')}` : ''}</div>
              <p className="font-sans italic text-[14px] text-ink-3 mt-1.5">基于本卷转写内容的问答。引用以 [C1] 标注，点击展开原文片段。</p>
            </div>

            {!sessionReady ? (
              <div className="space-y-2">
                <div className="sk h-4 w-1/3" /><div className="sk h-4 w-2/3" />
              </div>
            ) : (
              messages.map((m, i) => (
                <MessageRow key={i} msg={m} idx={i} onToggleCite={toggleCite} mode={mode} topK={topK} onCopy={copyAnswer} onRetry={retryQuestion} />
              ))
            )}

            {/* fail-closed 提示（独立显示在底部） */}
            {failClosed && (
              <div className="border border-rust/40 bg-rust/5 px-4 py-3">
                <div className="flex items-start gap-2.5">
                  <AlertTriangle className="w-4 h-4 text-rust mt-0.5" />
                  <div>
                    <div className="font-sans text-[14px] font-medium text-rust">该视频尚未建立索引</div>
                    <p className="font-sans text-[13px] text-ink-2 mt-1">strict_rag 模式强制走检索，无索引时无法回答。建立索引后即可基于转写内容做引用问答。</p>
                    <button onClick={triggerIndex} className="btn-line h-7 px-2.5 font-sans text-[10px] mt-2.5 inline-flex items-center gap-1"><Database className="w-3 h-3" />触发 RAG 索引</button>
                  </div>
                </div>
              </div>
            )}
          </div>
        </div>
      </main>

      {/* 底部输入 */}
      <footer className="shrink-0 bg-paper-0 border-t border-ink-0/15">
        <div className="max-w-[680px] mx-auto px-8 py-3.5">
          <ChatInput
            onSend={send}
            onStop={stop}
            streaming={streaming}
            placeholder="就转写内容提问…"
            topK={topK}
            onTopKChange={setTopK}
          />
          <div className="flex items-center justify-between mt-2 font-mono text-[10px]">
            <span className="text-ink-4">基于本卷转写 · 引用可追溯 · 无时间码</span>
            <button onClick={clearSession} className="text-ink-4 hover:text-rust flex items-center gap-1"><Trash2 className="w-3 h-3" />清空会话</button>
          </div>
        </div>
      </footer>
    </div>
  )
}

// 单条消息
function MessageRow({ msg, idx, onToggleCite, mode, topK, onCopy, onRetry }: {
  msg: Msg
  idx: number
  onToggleCite: (msgIdx: number, id: string) => void
  mode: ChatMode
  topK: number
  onCopy: (content: string) => void
  onRetry: (msgIdx: number) => void
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
      <div className="font-sans text-[10px] text-ink-4 flex items-center gap-2">
        <BookOpen className="w-3 h-3" />VidLens · {mode === 'strict_rag' ? '严格 RAG' : '问答'} · top_k {topK}
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
      {!msg.streaming && !msg.error && msg.content && (
        <div className="flex items-center gap-3 pt-1 font-mono text-[10px] text-ink-4">
          <button onClick={() => onCopy(msg.content)} className="hover:text-ink-0 flex items-center gap-1"><Copy className="w-3 h-3" />复制</button>
          <button onClick={() => onRetry(idx)} className="hover:text-ink-0 flex items-center gap-1"><RefreshCw className="w-3 h-3" />重试</button>
        </div>
      )}
      {msg.cites && msg.cites.length > 0 && (
        <CitationCards refs={msg.cites} openIds={msg.openCiteIds || []} />
      )}
    </div>
  )
}

// 把后端历史消息转成页面 Msg：assistant 消息从 retrieval_snapshot 恢复引用片段。
function parseMessages(msgs: ChatMessage[]): Msg[] {
  return msgs.map(m => ({
    role: m.role as 'user' | 'assistant',
    content: m.content,
    openCiteIds: [],
    ...(m.role === 'assistant' ? { cites: citesFromSnapshot(m.retrieval_snapshot) } : {}),
  }))
}

// 会话时间：MM-DD HH:mm
function fmtSession(iso: string) {
  const d = new Date(iso)
  return `${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`
}

function pad(n: number) { return n < 10 ? `0${n}` : `${n}` }
