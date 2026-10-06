import { presentAnswerCitations } from '@/lib/citationPresentation'
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useRouter } from '@/lib/router'
import type { CiteRef } from '@/components/Citation'
import { citeFromAPI, citationTimeLabel, hasReplayRange } from '@/components/Citation'
import { EvidenceDrawer } from '@/components/chat/EvidenceDrawer'
import { groupCitationSources } from '@/lib/citationGroups'
import { AnswerFeedback } from '@/components/chat/AnswerFeedback'
import { MarkdownAnswer } from '@/components/chat/MarkdownAnswer'
import { useConversationSession } from '@/components/chat/useConversationSession'
import type { ChatTraceStep } from '@/components/chat/traceTypes'
import { ThinkingProcess } from '@/components/chat/ThinkingProcess'
import type { ChatMsg } from '@/components/chat/chatUtils'
import { ModalityTag } from '@/components/ui/ModalityTag'
import { VideoPlayer, type VideoPlayerHandle } from '@/components/player/VideoPlayer'
import { useToast } from '@/components/Toast'
import { Icon } from '@/components/ui/Icon'
import { QuestionSuggestionsLoading } from '@/components/chat/QuestionSuggestionsLoading'
import { EmptyState } from '@/components/ui/AsyncState'
import { BrandMark } from '@/components/ui/BrandMark'
import { DrawerVeil } from '@/components/ui/Modal'
import { Modal } from '@/components/ui/Modal'
import { api, ApiError } from '@/lib/api'
import { KnowledgeSources, KnowledgeEvidence } from '@/components/knowledge/KnowledgeSources'
import { RunDetails, SessionMemoryControl } from '@/components/knowledge/RunDetails'
import { replayLink } from '@/lib/knowledge'
import knowledgeStyles from '@/components/knowledge/KnowledgeWorkspace.module.css'
import type { KnowledgeBase } from '@/lib/types'
import { fmtRelTime, formatClock, fmtScore, fmtTimeOfDay } from '@/lib/format'
import { formatDuration } from '@/lib/duration'
import type { Citation, ChatScopeType, VideoChatMode, VideoQuestionResult } from '@/lib/types'
import type { StudyBlock, Artifact, ArtifactDetail, AnswerPreview } from '@/lib/artifacts/schema'
import { artifactApi, artifactError } from '@/lib/artifacts/api'
import { FollowUpQuestions } from './FollowUpQuestions'
import './ChatWorkspace.css'
import { useShell } from '@/components/shell/AppShell'
import { useAIAvailability } from '@/components/settings/useAIAvailability'
import { useVideoAIPreflight } from '@/components/settings/VideoAIPreflight'

// Shared Chat / Agent workspace. Historical mode labels are display-only.
// Agent steps come from live tool events; new Chat answers retain server-safe progress.

const TOP_K = 4

type ChatUIMode = VideoChatMode
type AgentUIMode = 'agent' | 'research' | 'evidence_funnel'

const MODE_LABEL: Record<AgentUIMode, string> = {
  agent: '深入分析',
  research: '深入研究',
  evidence_funnel: '证据漏斗',
}

const MODE_NOTE: Record<ChatUIMode, string> = {
  chat: '结合视频内容自然问答、解释与总结',
  agent: '按问题检索文本证据，逐步分析后回答',
}



interface ChatWorkspaceProps {
  knowledgeBase?: KnowledgeBase
  scopeType: ChatScopeType
  targetId: number
  scopeName: string
  /** 单视频范围的播放源签名 URL;为空时右栏不放迷你播放器 */
  playbackUrl: string | null
  /** 签名 URL 过期时重取(约 5 分钟有效期),返回新 URL 或 null */
  refreshPlaybackUrl?: () => Promise<string | null>
  suggestions: string[]
  videoQuestions?: VideoQuestionResult | null
  questionsLoading?: boolean
  studyBlock?: StudyBlock | null
  studyError?: string
  returnToStudy?: string
  videoVisualMode?: string
  videoRetrievable?: boolean
  videoHasTranscript?: boolean
  aiPreflightAccepted?: boolean
}

// 自定义引用映射:在默认字段之上补 evidence_id / source_mapping_status,供证据抽屉展示。
function mapCitations(citations: Citation[]): CiteRef[] {
  return citations.map(citeFromAPI)
}

function clipText(text: string | undefined, max: number): string {
  const value = (text || '').trim()
  if (!value) return ''
  return value.length > max ? `${value.slice(0, max)}…` : value
}

export function ChatWorkspace({ knowledgeBase, scopeType, targetId, scopeName, playbackUrl, refreshPlaybackUrl, suggestions, videoQuestions, questionsLoading, studyBlock, studyError, returnToStudy, videoVisualMode, videoRetrievable, videoHasTranscript, aiPreflightAccepted = false }: ChatWorkspaceProps) {
  const isVideo = scopeType === 'video'
  const videoRelated = isVideo || scopeType === 'video_library'
  const router = useRouter()
  const toast = useToast()
  const { user } = useShell()
  const readOnly = user?.role === 'DEMO'
  const ai = useAIAvailability(readOnly)
  const videoPreflight = useVideoAIPreflight()
  const requestVideoAI = videoPreflight.request
  const [preflightAccepted, setPreflightAccepted] = useState(aiPreflightAccepted)
  const playerRef = useRef<VideoPlayerHandle>(null)
  const scrollRef = useRef<HTMLDivElement>(null)
  const followOutputRef = useRef(true)
  const inputRef = useRef<HTMLTextAreaElement | null>(null)
  const questionRefs = useRef<Record<number, HTMLDivElement | null>>({})
  const autoAsked = useRef(false)
  const workspaceRef = useRef<HTMLDivElement>(null)
  const [workspaceWidth, setWorkspaceWidth] = useState(Infinity)
  const railOverlay = workspaceWidth < 1140
  const questionsOverlay = workspaceWidth < 820
  useEffect(() => {
    const element = workspaceRef.current
    if (!element || typeof ResizeObserver === 'undefined') return
    const observer = new ResizeObserver(entries => setWorkspaceWidth(entries[0]?.contentRect.width || element.clientWidth))
    observer.observe(element)
    return () => observer.disconnect()
  }, [])

  const [input, setInput] = useState('')
  useEffect(() => { if (aiPreflightAccepted) setPreflightAccepted(true) }, [aiPreflightAccepted])
  const [mode, setMode] = useState<ChatUIMode>('chat')
  const [drawerCite, setDrawerCite] = useState<{ cite: CiteRef; cites: CiteRef[] } | null>(null)
  const [drawerOpen, setDrawerOpen] = useState(false)
  const [activeEvidence, setActiveEvidence] = useState<{ cite: CiteRef; messageKey: string } | null>(null)
  const [railTab, setRailTab] = useState<'run' | 'ev'>('run')
  const [panelsInstant, setPanelsInstant] = useState(false)
  const [railOpen, setRailOpen] = useState(() => {
    try { if (!window.matchMedia?.('(min-width: 1301px)').matches) return false; const saved = localStorage.getItem('vidlens-chat-rail'); return saved == null || saved === 'open' } catch { return false }
  })
  const [activeQuestion, setActiveQuestion] = useState(0)
  const [questionsOpen, setQuestionsOpen] = useState(false)
  const [askTall, setAskTall] = useState(false)
  const [importMessage, setImportMessage] = useState<ChatMsg | null>(null)
  const [importArtifacts, setImportArtifacts] = useState<Artifact[]>([])
  const [importDetail, setImportDetail] = useState<ArtifactDetail | null>(null)
  const [importBlock, setImportBlock] = useState('')
  const [importPreview, setImportPreview] = useState<AnswerPreview | null>(null)
  const [importPersonal, setImportPersonal] = useState(false)
  const [importKey, setImportKey] = useState('')
  const [importError, setImportError] = useState('')
  const [importConflict, setImportConflict] = useState(false)
  const [importBusy, setImportBusy] = useState(false)

  const [historyOpen, setHistoryOpen] = useState(false)
  const historyRef = useRef<HTMLDivElement>(null)

  const {
    session, sessions, messages, ragTrace, agentTrace, streaming, sending, sessionReady, historyLoading, historyError, retryHistory, send, stop, newSession, switchSession, loadSessions,
  } = useConversationSession({
    scopeType,
    targetId,
    basePath: isVideo ? `/chat/v/${targetId}` : scopeType === 'video_library' ? '/chat/library' : `/chat/kb/${targetId}`,
    mode,
    topK: TOP_K,
    canSend: !readOnly && ai.ready,
    onBlocked: () => toast.info(readOnly ? '演示模式可查看已有会话' : ai.reason),
    mapCitations,
    onBeforeSend: () => {
      followOutputRef.current = true
      setRailTab('run')
    },
  })

  const displayMessages = useMemo(() => messages.map(msg => msg.role === 'assistant'
    ? { ...msg, ...presentAnswerCitations(msg.content, msg.cites || []) }
    : msg), [messages])
  const lastMessage = displayMessages.length > 0 ? displayMessages[displayMessages.length - 1] : null
  const lastAssistant = lastMessage && lastMessage.role === 'assistant' ? lastMessage : null
  const toggleRail = (event: { detail: number }) => { setPanelsInstant(event.detail === 0); if (railOverlay) setQuestionsOpen(false); setRailOpen(open => {
    try { localStorage.setItem('vidlens-chat-rail', open ? 'closed' : 'open') } catch { /* Private browsing can deny storage. */ }
    return !open
  }) }
  const contextKey = useRef('')
  useEffect(() => {
    if (!studyBlock || !returnToStudy || contextKey.current === returnToStudy) return
    contextKey.current = returnToStudy
    const excerpt = Array.from(studyBlock.content)
    const limited = excerpt.length > 700
    setInput(`请核对已保存笔记「${studyBlock.title.slice(0, 80)}」中的这段内容。人工笔记尚待核对，不能直接视为原视频事实：\n${excerpt.slice(0, 700).join('')}${limited ? '\n（段落较长，当前只带入前 700 字；请缩小要核对的范围。）' : ''}\n\n请依据当前视频的证据回答：`)
  }, [studyBlock,returnToStudy])
  async function startImport(msg: ChatMsg) {
    setImportMessage(msg); setImportError(''); setImportConflict(false); setImportPreview(null); setImportDetail(null); setImportBlock(''); setImportKey(crypto.randomUUID()); setImportPersonal(false)
    setImportBusy(true)
    try {
      const first=await artifactApi.list(1,targetId)
      const all=[...first.list]
      for (let page=2; all.length<first.total; page++) { const next=await artifactApi.list(page,targetId); if (!next.list.length) break; all.push(...next.list) }
      const usable=all.filter(item=>!!item.current_version_id)
      setImportArtifacts(usable)
      if (usable.length) {
        const detail=await artifactApi.get(usable[0].id)
        setImportDetail(detail); setImportBlock(detail.version?.body.blocks[0]?.block_id ?? '')
      }
    } catch (error) { setImportError(artifactError(error)) }
    finally { setImportBusy(false) }
  }
  async function selectImportArtifact(id: string) {
    setImportBusy(true); setImportError(''); setImportConflict(false); setImportPreview(null); setImportPersonal(false); setImportKey(crypto.randomUUID())
    try { const detail=await artifactApi.get(id); setImportDetail(detail); setImportBlock(detail.version?.body.blocks[0]?.block_id ?? '') }
    catch (error) { setImportError(artifactError(error)) }
    finally { setImportBusy(false) }
  }
  async function previewImport() {
    if (!importMessage?.messageId || !importDetail?.version || !importBlock) return
    setImportBusy(true); setImportError(''); setImportConflict(false); setImportPreview(null); setImportPersonal(false)
    try { setImportPreview(await artifactApi.answerPreview(importDetail.id,importMessage.messageId,importBlock,importDetail.head_version)) }
    catch (error) { setImportError(artifactError(error)); setImportConflict(error instanceof ApiError && error.status === 409) }
    finally { setImportBusy(false) }
  }
  async function reloadImportTarget() {
    if (!importDetail || importBusy) return
    setImportBusy(true)
    try {
      const next = await artifactApi.get(importDetail.id)
      setImportDetail(next)
      setImportBlock(current => next.version?.body.blocks.some(block => block.block_id === current) ? current : next.version?.body.blocks[0]?.block_id ?? '')
      setImportPreview(null); setImportPersonal(false); setImportConflict(false); setImportKey(crypto.randomUUID())
      setImportError('请重新预览新版本的插入位置和引用。')
    } catch (error) { setImportError(artifactError(error)) }
    finally { setImportBusy(false) }
  }
  async function confirmImport() {
    if (!importMessage?.messageId || !importDetail?.version || !importPreview) return
    setImportBusy(true); setImportError('')
    try {
      const result=await artifactApi.importAnswer(importDetail.id,importMessage.messageId,importBlock,importDetail.head_version,importKey,importPersonal)
      const existing = new Set(importDetail.version.body.blocks.map(block => block.block_id))
      const inserted=result.version?.body.blocks.find(block => !existing.has(block.block_id))?.block_id
      setImportMessage(null)
      router.push(`/artifacts/${encodeURIComponent(result.id)}${inserted?`?block=${encodeURIComponent(inserted)}`:''}`)
    } catch (error) { setImportError(artifactError(error)); setImportConflict(error instanceof ApiError && error.status===409) }
    finally { setImportBusy(false) }
  }


  useEffect(() => {
    const el = scrollRef.current
    if (el && followOutputRef.current && messages.length > 0) el.scrollTop = el.scrollHeight
  }, [messages])

  const questions = useMemo(() => messages.map((message, index) => ({ message, index })).filter(item => item.message.role === 'user'), [messages])
  useEffect(() => {
    const root = scrollRef.current
    if (!root || !questions.length) { setActiveQuestion(0); return }
    const update = () => {
      const top = root.getBoundingClientRect().top + 90
      let current = questions[0].index
      for (const item of questions) {
        const node = questionRefs.current[item.index]
        if (node && node.getBoundingClientRect().top <= top) current = item.index
      }
      setActiveQuestion(current)
    }
    update()
    root.addEventListener('scroll', update, { passive: true })
    return () => root.removeEventListener('scroll', update)
  }, [questions, session?.id])

  const syncAsk = (el: HTMLTextAreaElement | null) => {
    if (!el) return
    el.style.height = 'auto'
    const h = Math.min(160, Math.max(36, el.scrollHeight))
    el.style.height = `${h}px`
    const next = h > 44
    setAskTall(v => (v === next ? v : next))
  }

  useEffect(() => {
    syncAsk(inputRef.current)
  }, [input])

  useEffect(() => {
    if (!historyOpen) return
    const onDown = (e: MouseEvent) => {
      if (historyRef.current && !historyRef.current.contains(e.target as Node)) setHistoryOpen(false)
    }
    document.addEventListener('mousedown', onDown)
    return () => document.removeEventListener('mousedown', onDown)
  }, [historyOpen])

  const removeHistorySession = async (sessionId: number) => {
    if (!window.confirm('删除这个会话?删除后聊天记录不可恢复。')) return
    try {
      await api.deleteSession(sessionId)
      if (session?.id === sessionId) newSession()
      await loadSessions()
      toast.success('会话已删除')
    } catch (e) {
      toast.error(e instanceof Error ? e.message : '删除失败')
    }
  }

  const submit = useCallback((text?: string) => {
    const q = (text ?? input).trim()
    if (!q) { toast.info('先输入一个问题'); return }
    if (Array.from(q).length > 1000) { toast.error('问题超过 1000 字，请缩小段落范围后再提问。'); return }
    if (sending || streaming || historyLoading || historyError || !sessionReady) return
    if (readOnly) { toast.info('演示模式可查看已有会话'); return }
    if (videoRelated && (!preflightAccepted || !ai.ready)) {
      requestVideoAI(isVideo ? '单视频问答' : '视频库问答', () => {
        setPreflightAccepted(true)
        setInput('')
        void send(q)
      })
      return
    }
    if (!ai.ready) { toast.info(ai.reason); return }
    setInput('')
    void send(q)
  }, [input, sending, streaming, historyLoading, historyError, sessionReady, send, toast, readOnly, ai.ready, ai.reason, videoRelated, preflightAccepted, requestVideoAI, isVideo])

  useEffect(() => {
    if (!sessionReady || !ai.ready || readOnly || autoAsked.current || !isVideo) return
    const url = new URL(window.location.href)
    const question = url.searchParams.get('ask')?.trim()
    if (!question) return
    autoAsked.current = true
    url.searchParams.delete('ask')
    window.history.replaceState(null, '', `${url.pathname}${url.search}`)
    submit(question)
  }, [sessionReady, isVideo, submit, ai.ready, readOnly])

  const openEvidence = useCallback((cite: CiteRef, cites: CiteRef[]) => {
    setDrawerCite({ cite, cites })
    const index = displayMessages.findIndex(message => message.cites === cites)
    setActiveEvidence(index < 0 ? null : { cite, messageKey: `${session?.id ?? 'new'}-${displayMessages[index].messageId ?? index}` })
    setDrawerOpen(true)
  }, [displayMessages, session?.id])

  const jumpToCitation = useCallback((cite: CiteRef) => {
    if (isVideo) {
      playerRef.current?.seek(cite.startMS ?? 0, true, cite.id)
      setRailOpen(true)
      if (railOverlay) setQuestionsOpen(false)
    } else if (cite.taskId) {
      // 知识库范围没有统一的迷你播放器:跳到该片段所属视频的工作台
      router.push(replayLink(cite.taskId, cite.startMS, cite.timeRangeStatus))
    }
  }, [isVideo, router, railOverlay])

  const citationJumpable = useCallback((cite: CiteRef) =>
    (isVideo ? !!playbackUrl : !!cite.taskId) && hasReplayRange(cite)
  , [isVideo, playbackUrl])

  // 右栏执行过程:Agent 运行用真实 trace(进行中或刚结束),否则历史 Agent 消息用快照 trace。
  // 历史消息即使没有步骤(运行失败/被停止)也保留其模式,避免误显示成 strict 推断面板。
  const agentRail = useMemo(() => {
    if (agentTrace.runId != null || agentTrace.steps.length > 0) {
      return { steps: agentTrace.steps, runId: agentTrace.runId, mode: agentTrace.mode ?? undefined, live: streaming && !agentTrace.finished }
    }
    if (lastAssistant?.agentRun) {
      return { steps: lastAssistant.trace ?? [], runId: lastAssistant.agentRunId ?? null, mode: lastAssistant.agentMode, live: streaming }
    }
    return null
  }, [agentTrace, streaming, lastAssistant])

  return (
    <div ref={workspaceRef} className={`chat-wrap${questionsOpen ? ' questions-expanded' : ''}${railOpen ? ' rail-expanded' : ''}${railOverlay ? ' context-overlay' : ''}${questionsOverlay ? ' questions-overlay' : ''}${panelsInstant ? ' panels-instant' : ''}`}>
      <nav id="chat-question-nav" className={`question-nav${questionsOpen ? ' open' : ''}`} aria-label="历史问题导航">
        <div className="question-nav-head"><b>本次问题</b><span>{questions.length}</span><button type="button" className="question-nav-close" onClick={() => setQuestionsOpen(false)} aria-label="收起问题目录"><Icon name="chev-l" size="sm" /></button></div>
        {questions.length ? questions.map(({ message, index }, position) => <button key={`${session?.id ?? 'new'}-${message.messageId ?? index}`} type="button" className={activeQuestion === index ? 'active' : ''} aria-current={activeQuestion === index ? 'location' : undefined} onClick={() => {
          questionRefs.current[index]?.scrollIntoView({ behavior: window.matchMedia('(prefers-reduced-motion: reduce)').matches ? 'auto' : 'smooth', block: 'start' })
          setActiveQuestion(index)
          if (questionsOverlay) setQuestionsOpen(false)
        }}><span>{position + 1}</span><span>{clipText(message.content, 70)}</span></button>) : <p>提问后，这里会列出问题。</p>}
      </nav>
      <div className="chat-col">
        <div className="chat-scope-bar">
          <button type="button" className={`chat-panel-button${questionsOpen ? ' selected' : ''}`} onClick={event => { setPanelsInstant(event.detail === 0); if (questionsOverlay) setRailOpen(false); setQuestionsOpen(v => !v) }} aria-expanded={questionsOpen} aria-controls="chat-question-nav" aria-label="问题目录"><Icon name="list" size="sm" /><span>问题</span><small>{questions.length}</small></button>
          <div className="chat-scope-name"><b>{scopeType === 'video' ? '单视频问答' : scopeType === 'video_library' ? '视频库问答' : '知识库问答'}</b><span>{scopeName}</span></div>
          <button type="button" className={`chat-panel-button${railOpen ? ' selected' : ''}`} onClick={toggleRail} aria-expanded={railOpen} aria-controls="chat-context-rail" aria-label="视频与执行过程"><Icon name={isVideo ? 'video' : 'target'} size="sm" /><span>{isVideo ? '视频与过程' : '执行过程'}</span><Icon name={railOpen ? 'chev-r' : 'chev-l'} size="sm" /></button>
        </div>
        {returnToStudy && <div className="artifact-notice">{studyError ? `笔记段落读取失败：${studyError}` : studyBlock ? `正在讨论已保存段落「${studyBlock.title}」；人工笔记需依据原视频核对。` : '正在读取已保存段落…'}<button className="btn btn-sm" onClick={() => router.push(returnToStudy)}>返回笔记位置</button></div>}
        {knowledgeBase && <KnowledgeSources kb={knowledgeBase} hitIds={new Set([...(lastAssistant?.cites || []).map(c=>c.taskId || 0), ...agentTrace.steps.flatMap(s=>(s.hitRows || []).map(h=>h.task_id || 0))])} />}
        {historyLoading && <div className="artifact-notice" role="status">正在读取历史消息…</div>}
        {historyError && <div className="artifact-notice danger" role="alert">{historyError}<button type="button" className="btn btn-sm" onClick={() => void retryHistory()}>重试读取</button></div>}
        <div className="chat-scroll" ref={scrollRef} onScroll={event => {
          const el = event.currentTarget
          followOutputRef.current = el.scrollHeight - el.scrollTop - el.clientHeight < 100
        }}>
          <div className="chat-inner">
            {messages.length === 0 ? (
              <div className="chat-empty">
                <div className="hello">
                  <BrandMark size={40} />
                  <h2>{isVideo ? '问这段视频' : scopeType === 'video_library' ? '问整个视频库' : `问「${scopeName}」`}</h2>
                </div>
                {!isVideo && <><p className={knowledgeStyles.intro}>{scopeType === 'video_library' ? '仅检索你的视频库中已用当前向量模型建好索引的视频。' : '仅检索当前知识库的成员视频。'}</p><div className={knowledgeStyles.prompts}>{suggestions.map(text=><button className={knowledgeStyles.prompt} key={text} onClick={()=>{setInput(text);if(scopeType !== 'video_library')setMode('agent');inputRef.current?.focus()}}>{text}<span>↗</span></button>)}</div></>}
                {isVideo && <div className="video-question-intro" aria-busy={questionsLoading}>{questionsLoading ? <QuestionSuggestionsLoading /> : <><p>{videoQuestions?.message || '从视频内容开始，试着问一个问题。'}</p>{videoQuestions?.questions.map(item => <button key={item.question} className="suggest-card" type="button" onClick={() => submit(item.question)} disabled={sending || streaming || historyLoading || !!historyError || !sessionReady}><Icon name="message" size="sm" /><span>{item.question}<small>{item.source}{item.time_ms != null ? ` · ${formatClock(item.time_ms)}` : ''}</small></span></button>)}</>}</div>}
              </div>
            ) : (
              displayMessages.map((msg, i) => msg.role === 'user'
                ? (
                  <div key={`${session?.id ?? 'new'}-${msg.messageId ?? i}`} className="msg msg-user" ref={node => { questionRefs.current[i] = node }}>
                    <div className="bubble">{msg.content}</div>
                  </div>
                )
                : (
                  <AgentMessageView
                    key={`${session?.id ?? 'new'}-${msg.messageId ?? i}`}
                    msg={msg}
                    feedbackReadOnly={readOnly}
                    sessionId={session?.id}
                    fallbackTitle={scopeName}
                    fallbackTaskId={isVideo ? targetId : undefined}
                    showVideoTitle={!isVideo}
                    followUpSessionId={msg === lastAssistant && !streaming ? session?.id : undefined}
                    onFollowUp={submit}
                    onOpenEvidence={openEvidence}
                    activeCitationId={activeEvidence && activeEvidence.messageKey === `${session?.id ?? 'new'}-${msg.messageId ?? i}` ? activeEvidence.cite.id : undefined}
                    canJump={citationJumpable}
                    onJump={jumpToCitation}
                    onStop={stop}
                    onImport={isVideo ? () => void startImport(msg) : undefined}
                  />
                )
              )
            )}
          </div>
        </div>

        <div className="composer">
          <div className="composer-inner">
            <div className="composer-toolbar">
            <div className="mode-row">
              <button
                className={`mode-pill${mode === 'chat' ? ' on' : ''}`}
                disabled={streaming}
                onClick={() => setMode('chat')}
              >
                <Icon name="bolt" size="sm" />快速问答
              </button>
              <button className={`mode-pill${mode === 'agent' ? ' on' : ''}`} disabled={streaming} onClick={() => setMode('agent')}><Icon name="target" size="sm" />{isVideo ? '深入分析' : '跨视频研究'}</button>
            </div>
            <div className="composer-tools" ref={historyRef}>
              <button
                type="button"
                className="btn btn-ic btn-ghost"
                aria-label="历史会话"
                title="历史会话"
                disabled={streaming}
                onClick={() => {
                  if (!historyOpen) void loadSessions()
                  setHistoryOpen(v => !v)
                }}
              >
                <Icon name="message" />
              </button>
              {historyOpen && (
                <div className="session-pop" role="listbox" aria-label="历史会话">
                  <div className="session-pop-head">这个范围的会话</div>
                  {sessions.length === 0 ? (
                    <div className="session-pop-empty">还没有历史会话</div>
                  ) : sessions.map(item => (
                    <div
                      key={item.id}
                      className={`session-pop-row${session?.id === item.id ? ' on' : ''}`}
                    >
                      <button type="button" className="session-select" onClick={() => { void switchSession(item.id); setHistoryOpen(false) }}>
                      <span className="q">{item.title || '未命名会话'}</span>
                      <span className="when">{fmtRelTime(item.updated_at)}</span>
                      </button>
                      <button
                        className="session-del"
                        title="删除会话"
                        aria-label="删除会话"
                        onClick={e => { e.stopPropagation(); void removeHistorySession(item.id) }}
                      >
                        <Icon name="trash" size="sm" />
                      </button>
                    </div>
                  ))}
                </div>
              )}
              <button
                type="button"
                className="btn btn-ic btn-ghost"
                aria-label="新会话"
                title="新会话"
                onClick={() => { newSession(); setHistoryOpen(false) }}
                disabled={streaming}
              >
                <Icon name="plus" />
              </button>
            </div>
            </div>
            <p className="mode-note" title="模式和默认 AI 配置从下一轮起生效，历史回答保留当轮配置。">{scopeType === 'video_library' ? '范围：当前向量模型可检索的视频' : scopeType === 'knowledge_base' ? `范围：${scopeName}` : mode === 'agent' && videoVisualMode && videoVisualMode !== 'off' ? '按问题调用文本与画面工具，逐步分析后回答' : MODE_NOTE[mode]}</p>
            {isVideo && videoRetrievable === false && <p className="mode-note">{videoHasTranscript ? '检索未就绪；快速问答可使用摘要或转写，暂不提供检索引用。' : '尚无当前模型可检索的内容，请先在视频详情处理内容并建立索引。'}</p>}
            {!readOnly && !ai.ready && <div className="chat-ai-notice" role="status"><span>{ai.reason}</span>{ai.error ? <button type="button" className="btn btn-sm" onClick={() => void ai.refetch()}>重试</button> : <a className="btn btn-sm" href="/settings" target="_blank" rel="noopener noreferrer">配置 AI</a>}</div>}
            <div className={`ask-bar${askTall ? ' tall' : ''}`} style={{ marginTop: 0 }}>
              <textarea
                ref={el => { inputRef.current = el }}
                rows={1}
                value={input}
                maxLength={1000}
                aria-label="输入问题"
                onChange={e => setInput(e.target.value)}
                onKeyDown={e => { if (e.key === 'Enter' && !e.shiftKey) { e.preventDefault(); submit() } }}
                placeholder={isVideo ? '问这段视频…' : scopeType === 'video_library' ? '向视频库提问…' : '向知识库提问…'}
              />
              <button className="ask-send" disabled={sending || streaming || historyLoading || !!historyError || !sessionReady || readOnly} onClick={() => submit()} aria-label="发送">
                <Icon name="send" />
              </button>
            </div>
          </div>
        </div>
      </div>

      {((railOpen && railOverlay) || (questionsOpen && questionsOverlay)) && <div className="chat-rail-veil"><DrawerVeil onClose={() => { setRailOpen(false); setQuestionsOpen(false) }} /></div>}

      <aside id="chat-context-rail" className={`rail-panel${railOpen ? ' open' : ''}`} aria-label="视频与执行过程">
        <div className="rail-mobile-head"><b>{isVideo ? '视频与执行过程' : '执行过程'}</b>
          <button type="button" className="btn btn-ic btn-ghost" onClick={event => { setPanelsInstant(event.detail === 0); setRailOpen(false) }} aria-label="收起视频与执行过程">
            <Icon name="chev-r" />
          </button>
        </div>
        {isVideo && playbackUrl && (
          <div className="mini-player-block">
            <VideoPlayer ref={playerRef} src={playbackUrl} title={scopeName} compact className="mini-player" onNeedRefresh={refreshPlaybackUrl} />
            <button
              type="button"
              className="mini-workbench"
              aria-label="打开视频工作台"
              title="视频工作台"
              onClick={() => router.push(`/video/${targetId}`)}
            >
              <Icon name="video" size="sm" />
            </button>
          </div>
        )}
        <div className="rail-tabs">
          <button className={`rail-tab${railTab === 'run' ? ' on' : ''}`} onClick={() => setRailTab('run')}>
            执行过程
          </button>
          {!isVideo && <button className={`rail-tab${railTab === 'ev' ? ' on' : ''}`} onClick={()=>setRailTab('ev')}>来源证据 · {lastAssistant?.cites?.length || 0}</button>}

        </div>
        <div className="rail-body">
          {session && <SessionMemoryControl key={session.id} sessionId={session.id} disabled={streaming} />}
          {!isVideo && railTab === 'ev' ? <KnowledgeEvidence cites={lastAssistant?.cites || []} onOpen={openEvidence} /> : agentRail ? (
              <>
                <RunHeader mode={(agentRail.mode as AgentUIMode) || 'agent'} runId={agentRail.runId} />
                {session && agentRail.runId && <RunDetails key={agentRail.runId} sessionId={session.id} runId={agentRail.runId} live={agentRail.live} />}
                <p style={{ fontSize: 12, color: 'var(--tx-4)', marginBottom: 10 }}>
                  {agentRail.mode === 'research' ? '受限研究循环' : agentRail.mode === 'evidence_funnel' ? '固定漏斗' : '自主工具调用'}
                </p>
                {agentRail.steps.length > 0 ? (
                  <div className="steps" style={agentRail.mode === 'evidence_funnel' ? { marginTop: 12 } : undefined}>
                    {agentRail.steps.map(step => <AgentTraceStepView key={step.id} step={step} />)}
                  </div>
                ) : (
                  <EmptyState
                    variant="bare"
                    icon="target"
                    desc={agentRail.live ? '等待运行事件…' : '这次运行没有留下执行步骤(可能失败或已停止)。'}
                  />
                )}
              </>
            ) : (
              <>
                <div className="run-meta">
                  <span className="chip chip-mute mono">chat</span>
                  <span className="chip chip-mute">{streaming ? '实时' : lastAssistant?.traceSource === 'server' ? '已保存记录' : '历史摘要'}</span>
                </div>
                <p style={{ fontSize: 12, color: 'var(--tx-4)', marginBottom: 10 }}>{lastAssistant?.traceSource === 'server' ? '服务端记录的阶段与调用摘要；模型内部推理未保存' : '发送问题后查看执行过程'}</p>
                {(ragTrace.length > 0 ? ragTrace : lastAssistant?.trace ?? []).length > 0 ? (
                  <div className="steps">
                    {(ragTrace.length > 0 ? ragTrace : lastAssistant?.trace ?? []).map(step => <TraceStepView key={step.id} step={step} />)}
                  </div>
                ) : (
                  <EmptyState variant="bare" icon="target" desc="提问后会显示检索过程" />
                )}
              </>
            )}
        </div>
      </aside>

      {drawerCite && (
        <EvidenceDrawer
          open={drawerOpen}
          onExited={() => setDrawerCite(null)}
          cite={drawerCite.cite}
          cites={drawerCite.cites}
          onSelect={cite => { setDrawerCite(current => current ? { ...current, cite } : null); setActiveEvidence(current => current ? { ...current, cite } : null) }}
          fallbackTaskId={isVideo ? targetId : undefined}
          fallbackTitle={scopeName}
          canJump={citationJumpable(drawerCite.cite)}
          jumpDisabledHint={isVideo && !playbackUrl ? '当前视频没有可用播放源' : undefined}
          onJump={cite => { jumpToCitation(cite); setDrawerOpen(false) }}
          onClose={() => setDrawerOpen(false)}
        />
      )}

      {importMessage && <Modal title="把回答收进笔记" width={720} onClose={() => setImportMessage(null)} footer={<><button className="btn" onClick={() => setImportMessage(null)}>取消</button>{importPreview && <button className="btn btn-primary" disabled={importBusy || importPreview.version_id!==importDetail?.version?.id || (importPreview.unmapped.length>0 && !importPersonal)} onClick={() => void confirmImport()}>{importBusy ? '保存中…' : '确认生成新版本'}</button>}</>}>
        <p>只收录当前视频已完整保存的回答。新块标记为人工整理、待核对，插在所选段落之后。</p>
        {importBusy && <p role="status">正在核对回答和目标快照…</p>}
        {importError && <div className="artifact-notice danger" role="alert">{importError}{importPreview && <span> 预览仍保留。</span>}{importConflict && importDetail && <button className="btn btn-sm" disabled={importBusy} onClick={() => void reloadImportTarget()}>读取新版本</button>}</div>}
        {!importArtifacts.length && !importBusy && <p>当前视频没有可写的笔记版本。请先在视频页生成笔记。</p>}
        {importArtifacts.length>0 && <label className="artifact-field">目标笔记<select disabled={importBusy} value={importDetail?.id ?? ''} onChange={e => void selectImportArtifact(e.target.value)}>{importArtifacts.map(item=><option key={item.id} value={item.id}>{item.title}</option>)}</select></label>}
        {importDetail?.version && <label className="artifact-field">插在段落之后<select disabled={importBusy} value={importBlock} onChange={e => { setImportBlock(e.target.value); setImportPreview(null); setImportPersonal(false); setImportKey(crypto.randomUUID()) }}>{importDetail.version!.body.blocks.map(block=><option key={block.block_id} value={block.block_id}>{block.title}</option>)}</select></label>}
        {importDetail?.version && <button className="btn btn-sm" disabled={importBusy || !importBlock} onClick={() => void previewImport()}>预览正文与引用</button>}
        {importPreview && <><div className="artifact-notice"><b>将收录的正文</b><p style={{ whiteSpace:'pre-wrap', maxHeight:240, overflow:'auto' }}>{importPreview.content}</p></div><p>映射到目标快照：{importPreview.mapped.length} 条依据；无法映射或缺失：{importPreview.unmapped.length ? importPreview.unmapped.join('、') : '无'}。</p>{importPreview.unmapped.length>0 && <label><input type="checkbox" checked={importPersonal} onChange={e=>{setImportPersonal(e.target.checked);setImportKey(crypto.randomUUID())}} /> 我确认把整段作为无来源个人补充保存，已有聊天引用也不作为来源</label>}</>}
      </Modal>}
      {videoPreflight.dialog}

    </div>
  )
}

// ---- 消息渲染 ----

/** 右栏运行头部:三种 Agent 路径的模式 chip + 预算标注 + run_id */
function RunHeader({ mode, runId }: { mode: AgentUIMode; runId: string | null }) {
  return (
    <div className="run-meta">
      <span className="chip chip-acc">
        <Icon name={mode === 'research' ? 'zoom-scan' : mode === 'evidence_funnel' ? 'filter' : 'target'} size="sm" />
        {MODE_LABEL[mode]}
      </span>
      {mode === 'research' && <span className="chip chip-mute mono">MaxSteps 8 · MaxReplans 2</span>}
      {mode === 'evidence_funnel' && <span className="chip chip-mute">固定八步</span>}
      {runId && <span className="rid mono" title={runId}>{runId.slice(0, 8)}</span>}
    </div>
  )
}

function AgentMessageView({
  msg, sessionId, feedbackReadOnly, fallbackTitle, fallbackTaskId, showVideoTitle, followUpSessionId, onFollowUp, onOpenEvidence, canJump, onJump, onStop, onImport, activeCitationId,
}: {
  sessionId?: number
  feedbackReadOnly: boolean
  msg: ChatMsg
  fallbackTitle: string
  fallbackTaskId?: number
  showVideoTitle: boolean
  followUpSessionId?: number
  onFollowUp: (question: string) => void
  onOpenEvidence: (cite: CiteRef, cites: CiteRef[]) => void
  canJump: (cite: CiteRef) => boolean
  onJump: (cite: CiteRef) => void
  onStop: () => void
  onImport?: () => void
  activeCitationId?: string
}) {
  const toast = useToast()
  const cites = msg.cites || []
  const citationSources = groupCitationSources(cites, fallbackTaskId)
  const isAgentRun = !!msg.agentRun
  const agentMode = (isAgentRun ? (msg.agentMode as AgentUIMode | undefined) ?? 'agent' : undefined)
  const waitingServer = !!msg.streaming && !!agentMode && agentMode !== 'agent' && msg.content.length === 0
  const [clockNow, setClockNow] = useState(() => Date.now())
  useEffect(() => {
    if (!msg.streaming) return
    const timer = window.setInterval(() => setClockNow(Date.now()), 1000)
    return () => window.clearInterval(timer)
  }, [msg.streaming])

  const openCite = (no: number) => {
    const hit = cites.find(c => c.id === `C${no}`)
    if (hit) onOpenEvidence(hit, cites)
  }

  const copyAnswer = () => {
    if (navigator.clipboard) {
      navigator.clipboard.writeText(msg.content).then(
        () => toast.success('回答已复制'),
        () => toast.error('复制失败'),
      )
    }
  }

  return (
    <div className="msg msg-agent">
      <div className="who">
        <span className="agent-mark">
          <Icon name={agentMode === 'research' ? 'zoom-scan' : agentMode === 'evidence_funnel' ? 'filter' : isAgentRun ? 'target' : 'bolt'} />
        </span>
        映知
        <span style={{ color: 'var(--tx-4)' }}>{agentMode ? MODE_LABEL[agentMode] : '快速问答'}</span>
      </div>
      <ThinkingProcess message={msg} />
      <div className="answer">
        <MarkdownAnswer content={msg.content} onCite={openCite} activeCite={activeCitationId ? Number(activeCitationId.slice(1)) : undefined} />
        {waitingServer && (
          <span className="chip chip-mute">服务端执行中…</span>
        )}
        {msg.streaming && !waitingServer && <span className="stream-cursor" />}
      </div>
      {!msg.streaming && !msg.content && !msg.error && (
        <div style={{ marginTop: 8 }}>
          <span className="chip chip-mute">已停止,本轮没有生成回答</span>
        </div>
      )}
      {msg.degraded && (
        <div style={{ marginTop: 8 }}>
          {isAgentRun ? (
            <span className="chip chip-warn" title="本轮未完成完整分析，请留意回答中的限制说明">
              <Icon name="shield" size="sm" />有限结果
            </span>
          ) : (
            <span className="chip chip-warn" title={msg.degradationReason === 'retrieval_unavailable' ? '检索未完成；本轮使用摘要或转写生成回答，没有检索引用' : msg.degradationReason === 'retrieval_and_generation_unavailable' ? '检索与生成均未完成；本轮只提供有限上下文' : '生成阶段异常,回答由片段与摘要直拼,未经过完整模型生成'}>
              <Icon name="alert" size="sm" />{msg.degradationReason === 'retrieval_unavailable' ? '检索降级 · 无引用' : '降级回答'}
            </span>
          )}
          {msg.degradationReason === 'retrieval_unavailable' && (
            <div style={{ marginTop: 6, fontSize: 12, color: 'var(--tx-3)' }}>已用摘要或转写生成回答，未找到检索引用。</div>
          )}
          {msg.degradationReason?.startsWith('retrieval_') && msg.diagnosticId && (
            <div style={{ marginTop: 4, fontSize: 11, color: 'var(--tx-4)' }}>诊断编号：<code>{msg.diagnosticId}</code></div>
          )}
        </div>
      )}
      {msg.error && (
        <div style={{ marginTop: 8, fontSize: 12, color: 'var(--bad)', display: 'flex', alignItems: 'center', gap: 6 }}>
          <Icon name="alert" size="sm" /><span>{msg.error}</span>
        </div>
      )}
      {!msg.streaming && msg.content && !msg.error && cites.length === 0 && <p style={{ fontSize: 12, color: 'var(--tx-3)' }}>本轮回答没有有效原文引用，视频事实仍需核对。</p>}
      {cites.length > 0 && (
        <div className="cite-list">
          <p className="cite-source-count">{citationSources.length} 个来源片段 · {cites.length} 句引用</p>
          {citationSources.map(group => {
            const source = group.citations[0]
            return <div key={group.key} className="cite-card cite-source-card" role="group" aria-label={`来源片段 ${citationTimeLabel(source)}`}>
              <div className="cite-source-head chead">
                {showVideoTitle && <span className="cvideo">{source.videoTitle || fallbackTitle}</span>}
                <span className="ctime mono">{citationTimeLabel(source)}</span>
                <ModalityTag modality={source.modality} />
                {source.timeRangeStatus === 'coarse' && <span className="chip chip-mute">句子时间未提供</span>}
                <button className="btn btn-sm cjump" onClick={() => canJump(source) ? onJump(source) : onOpenEvidence(source, cites)}><Icon name="play" size="sm" />{canJump(source) ? '回放' : '查看'}</button>
              </div>
              <div className="cite-source-quotes">{group.citations.map(cite => <button key={cite.id} type="button" className={`cite-card-open${activeCitationId === cite.id ? ' selected' : ''}`} aria-pressed={activeCitationId === cite.id} aria-label={`查看证据 ${cite.id}`} onClick={() => onOpenEvidence(cite, cites)}>
                <span className="cno">{cite.id}</span><div className="cbody">
                  {cite.supportStatus === 'unsupported' && <span className="chip chip-warn">结论支持不足</span>}
                  {(cite.supportStatus === 'review_invalid' || cite.supportStatus === 'review_unavailable') && <span className="chip chip-mute">语义复核未完成</span>}
                  <div className="cquote">{clipText(cite.modality === 'visual_caption' || cite.modality === 'visual_ocr' ? cite.displayContext || cite.content : cite.anchorQuote || cite.content, 220)}</div>
                </div>
              </button>)}</div>
            </div>
          })}
        </div>
      )}
      <div className="answer-meta">
        <button className="meta-link" onClick={copyAnswer}>
          <Icon name="file" size="sm" />复制回答
        </button>
        {onImport && msg.messageId && !msg.streaming && !msg.error && !msg.cancelled && !!msg.content && <button className="meta-link" onClick={onImport}><Icon name="plus" size="sm" />收进笔记</button>}
      </div>
      <details className="answer-technical"><summary>技术详情</summary><p>{agentMode ? MODE_LABEL[agentMode] : '快速问答'} · {msg.modelName ? `模型：${msg.modelName}` : '模型未记录'} · {msg.profileId ? `配置 #${msg.profileId}` : '配置未记录'}</p>{sessionId && msg.agentRunId && !msg.streaming && <RunDetails sessionId={sessionId} runId={msg.agentRunId} live={false} />}</details>
      <div className="answer-completion" aria-live="polite">{msg.streaming ? <><span className="answer-live-dot" />{msg.transientStatus || (msg.content ? '正在生成回答…' : isAgentRun ? '正在分析视频…' : '正在检索…')}{msg.processStartedAt !== undefined && ` · 已等待 ${formatDuration(Math.max(0, clockNow - msg.processStartedAt))}`}<button type="button" onClick={onStop}>停止</button></> : <>{msg.error ? '本轮未完成' : msg.cancelled ? '已停止' : '已完成'}{!msg.error && !msg.cancelled && ` · ${msg.executionDurationMs !== undefined ? `处理耗时 ${formatDuration(msg.executionDurationMs)}` : '处理耗时未知'}`}{msg.createdAt ? ` · ${fmtTimeOfDay(msg.createdAt)}` : ''}</>}</div>
      {!feedbackReadOnly && sessionId && msg.messageId && msg.messageId > 0 && !msg.streaming && <AnswerFeedback sessionId={sessionId} messageId={msg.messageId} />}
      {followUpSessionId && msg.messageId && !!msg.content && !msg.error && !msg.cancelled && !msg.streaming && <FollowUpQuestions sessionId={followUpSessionId} messageId={msg.messageId} onAsk={onFollowUp} />}
    </div>
  )
}

function TraceStepView({ step }: { step: ChatTraceStep }) {
  const cls = step.status === 'running' ? 'running' : step.status === 'error' ? 'error' : 'done'
  return (
    <div className={`step ${cls}`}>
      <div className="step-dot" />
      <div className="step-head">
        <span className="step-label">{step.label}</span>
        {typeof step.hits === 'number' && (
          <span className="step-dur mono">{step.hits} 条</span>
        )}
      </div>
      {(step.detail || step.error) && (
        <div className="step-body">
          <span style={{ fontSize: 11.5, color: step.error ? 'var(--bad)' : 'var(--tx-3)' }}>
            {step.detail || step.error}
          </span>
        </div>
      )}
      {(step.tool || step.toolInput || step.toolOutput || step.kind === 'prepare') && <details className="step-body"><summary>{step.kind === 'prepare' ? '上下文准备详情' : step.kind === 'retrieve' ? '检索详情' : '步骤详情'}</summary>{step.tool && <p>实际调用：{step.tool}</p>}{step.toolInput && <p>输入摘要：{step.toolInput}</p>}{step.toolOutput && <p>结果摘要：{step.toolOutput}</p>}</details>}
    </div>
  )
}

/** Agent 真实步骤:命中卡 / 工具卡按原型样式;SSE 不带时间码,命中行无时间列 */
function AgentTraceStepView({ step }: { step: ChatTraceStep }) {
  const cls = step.status === 'running' ? 'running' : step.status === 'error' ? 'error' : 'done'
  const durationMs = typeof step.durationMs === 'number' && step.durationMs > 0 ? step.durationMs : undefined
  const showHits = (step.hitRows?.length ?? 0) > 0
  return (
    <div className={`step ${cls}`}>
      <div className="step-dot" />
      <div className="step-head">
        <span className="step-label">{step.label}</span>
        {durationMs && <span className="step-dur mono">{formatDuration(durationMs)}</span>}
      </div>
      <div className="step-body">
        {(step.kind === 'plan' || step.tool) && <details><summary>{step.kind === 'plan' ? '模型计划摘要' : '工具调用详情'}</summary>{step.tool && <p>{step.kind === 'plan' ? '计划下一步' : '实际调用'}：{step.tool}</p>}{step.toolInput && <p>输入摘要：{step.toolInput}</p>}{step.toolOutput && <p>结果摘要：{step.toolOutput}</p>}{step.error && <p>失败：{step.error}</p>}</details>}
        {showHits ? (
          <div className="hits-card">
            <div className="hits-q">
              query <b>{step.query || '—'}</b>
              {typeof step.hits === 'number' && <> · {step.hits} 条命中</>}
            </div>
            {step.hitRows!.map((row, i) => (
              <div key={i} className="hit-row">
                <span className="hs mono">{fmtScore(row.score)}</span>
                <span className="hv">
                  {row.video_title || '本视频'}{row.chunk_index != null ? ` · 片段 #${row.chunk_index}` : ''}
                </span>
              </div>
            ))}
          </div>
        ) : step.kind === 'plan' && step.detail ? (
          <span style={{ fontSize: 12, color: 'var(--tx-3)' }}>{step.detail}</span>
        ) : step.tool ? (
          <div className="tool-card">
            <div className="tool-head">
              <span className="tool-name mono">{step.tool}</span>
              {durationMs && <span className="tool-ms">{formatDuration(durationMs)}</span>}
            </div>
            {(step.toolOutput || step.toolInput) && (
              <div className="tool-out">{clipText(step.toolOutput || step.toolInput, 200)}</div>
            )}
          </div>
        ) : (step.detail || typeof step.hits === 'number') ? (
          <span style={{ fontSize: 11.5, color: step.error ? 'var(--bad)' : 'var(--tx-3)' }}>
            {step.detail || `命中 ${step.hits} 条`}
          </span>
        ) : null}
        {step.error && (
          <span style={{ fontSize: 11.5, color: 'var(--bad)' }}>{step.error}</span>
        )}
      </div>
    </div>
  )
}
