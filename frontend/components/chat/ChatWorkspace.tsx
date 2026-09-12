'use client'

import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useRouter } from 'next/navigation'
import type { CiteRef } from '@/components/Citation'
import { formatTimeRange, hasReplayRange } from '@/components/Citation'
import { EvidenceDrawer } from '@/components/chat/EvidenceDrawer'
import { MarkdownAnswer } from '@/components/chat/MarkdownAnswer'
import { useConversationSession } from '@/components/chat/useConversationSession'
import type { ChatTraceStep } from '@/components/chat/traceTypes'
import { ThinkingProcess } from '@/components/chat/ThinkingProcess'
import type { ChatMsg } from '@/components/chat/chatUtils'
import { ModalityTag } from '@/components/ui/ModalityTag'
import { VideoPlayer, type VideoPlayerHandle } from '@/components/player/VideoPlayer'
import { useToast } from '@/components/Toast'
import { Icon } from '@/components/ui/Icon'
import { BrandMark } from '@/components/ui/BrandMark'
import { DrawerVeil } from '@/components/ui/Modal'
import { api } from '@/lib/api'
import { KnowledgeSources, KnowledgeEvidence } from '@/components/knowledge/KnowledgeSources'
import { RunDetails, SessionMemoryControl } from '@/components/knowledge/RunDetails'
import { replayLink } from '@/lib/knowledge'
import knowledgeStyles from '@/components/knowledge/KnowledgeWorkspace.module.css'
import type { KnowledgeBase } from '@/lib/types'
import { fmtRelTime } from '@/lib/format'
import type { Citation, ChatScopeType, VideoChatMode } from '@/lib/types'

// Shared Chat / Agent workspace. Historical mode labels are display-only.
// Agent steps come from live tool events; Chat progress remains inferred.

const TOP_K = 4

type ChatUIMode = VideoChatMode
type AgentUIMode = 'agent' | 'research' | 'evidence_funnel'

const MODE_LABEL: Record<AgentUIMode, string> = {
  agent: 'Agent',
  research: '深入研究',
  evidence_funnel: '证据漏斗',
}

const MODE_NOTE: Record<ChatUIMode, string> = {
  chat: '结合视频内容自然问答、解释与总结',
  agent: '按问题调用文本和视觉工具,逐步分析后回答',
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
}

// 自定义引用映射:在默认字段之上补 evidence_id / source_mapping_status,供证据抽屉展示。
function mapCitations(citations: Citation[]): CiteRef[] {
  return citations.map((citation, index) => ({
    id: citation.citation_id || `C${index + 1}`,
    taskId: citation.task_id,
    chunkIndex: citation.chunk_index,
    score: citation.score,
    content: citation.content,
    anchorQuote: citation.anchor_quote || citation.content,
    displayContext: citation.display_context || citation.content,
    modality: citation.modality,
    startMS: citation.start_ms,
    endMS: citation.end_ms,
    timeRangeStatus: citation.time_range_status,
    contextStartMS: citation.context_start_ms,
    contextEndMS: citation.context_end_ms,
    displayContextTruncated: citation.display_context_truncated,
    sourceRefs: citation.source_refs,
    source: citation.source,
    videoTitle: citation.video_title,
    finalRank: citation.final_rank,
    evidenceId: citation.evidence_id,
    sourceMappingURL: citation.source_mapping_status,
  }))
}

function clipText(text: string | undefined, max: number): string {
  const value = (text || '').trim()
  if (!value) return ''
  return value.length > max ? `${value.slice(0, max)}…` : value
}

function formatDuration(ms: number): string {
  return ms >= 1000 ? `${(ms / 1000).toFixed(1)}s` : `${Math.round(ms)}ms`
}

export function ChatWorkspace({ knowledgeBase, scopeType, targetId, scopeName, playbackUrl, refreshPlaybackUrl, suggestions }: ChatWorkspaceProps) {
  const isVideo = scopeType === 'video'
  const router = useRouter()
  const toast = useToast()
  const playerRef = useRef<VideoPlayerHandle>(null)
  const scrollRef = useRef<HTMLDivElement>(null)
  const followOutputRef = useRef(true)
  const inputRef = useRef<HTMLTextAreaElement | null>(null)
  const startedAtRef = useRef(0)

  const [input, setInput] = useState('')
  const [mode, setMode] = useState<ChatUIMode>('chat')
  const [drawerCite, setDrawerCite] = useState<{ cite: CiteRef; cites: CiteRef[] } | null>(null)
  const [railTab, setRailTab] = useState<'run' | 'ev'>('run')
  const [railOpen, setRailOpen] = useState(false)
  const [elapsed, setElapsed] = useState<string | null>(null)
  const [askTall, setAskTall] = useState(false)

  const [historyOpen, setHistoryOpen] = useState(false)
  const historyRef = useRef<HTMLDivElement>(null)

  const {
    session, sessions, messages, ragTrace, agentTrace, streaming, send, stop, newSession, switchSession, loadSessions,
  } = useConversationSession({
    scopeType,
    targetId,
    basePath: isVideo ? `/chat/v/${targetId}` : `/chat/kb/${targetId}`,
    mode,
    topK: TOP_K,
    mapCitations,
    onBeforeSend: () => {
      followOutputRef.current = true
      startedAtRef.current = performance.now()
      setElapsed(null)
      setRailTab('run')
    },
  })

  const lastMessage = messages.length > 0 ? messages[messages.length - 1] : null
  const lastAssistant = lastMessage && lastMessage.role === 'assistant' ? lastMessage : null


  useEffect(() => {
    const el = scrollRef.current
    if (el && followOutputRef.current && messages.length > 0) el.scrollTop = el.scrollHeight
  }, [messages])

  useEffect(() => {
    if (!streaming && startedAtRef.current > 0) {
      setElapsed(((performance.now() - startedAtRef.current) / 1000).toFixed(1))
      startedAtRef.current = 0
    }
  }, [streaming])

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
    if (streaming) return
    setInput('')
    void send(q)
  }, [input, streaming, send, toast])

  const openEvidence = useCallback((cite: CiteRef, cites: CiteRef[]) => {
    setDrawerCite({ cite, cites })
  }, [])

  const jumpToCitation = useCallback((cite: CiteRef) => {
    if (isVideo) {
      playerRef.current?.seek(cite.startMS || 0, true, cite.id)
      if (window.matchMedia('(max-width: 1080px)').matches) setRailOpen(true)
    } else if (cite.taskId) {
      // 知识库范围没有统一的迷你播放器:跳到该片段所属视频的工作台
      router.push(replayLink(cite.taskId, cite.startMS, cite.timeRangeStatus))
    }
  }, [isVideo, router])

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

  const agentBlocked = !streaming && !!lastAssistant?.agentRun && !!lastAssistant.degraded

  const statusLine = (() => {
    if (streaming) {
      const generating = lastMessage?.role === 'assistant' && lastMessage.content.length > 0

      return (
        <>
          <span className="pulse" />
          <span>{generating ? '正在生成回答…' : mode === 'agent' ? '正在分析视频…' : '检索中,稍等…'}</span>
          <button className="meta-link stop" onClick={stop}>停止</button>
        </>
      )
    }
    if (lastMessage?.role === 'assistant' && lastMessage.error) {
      return (
        <span style={{ color: 'var(--bad)', display: 'inline-flex', alignItems: 'center', gap: 6 }}>
          <Icon name="alert" size="sm" />{lastMessage.error}
        </span>
      )
    }
    if (agentBlocked) {
      return (
        <span style={{ color: 'var(--warn)', display: 'inline-flex', alignItems: 'center', gap: 6 }}>
          <Icon name="shield" size="sm" />本轮仅提供有限信息
        </span>
      )
    }
    if (elapsed) {
      return (
        <span style={{ color: 'var(--ok)', display: 'inline-flex', alignItems: 'center', gap: 6 }}>
          <Icon name="check" size="sm" />已完成 · {elapsed}s
        </span>
      )
    }
    return null
  })()


  return (
    <div className="chat-wrap">
      <div className="chat-col">
        {knowledgeBase && <KnowledgeSources kb={knowledgeBase} hitIds={new Set([...(lastAssistant?.cites || []).map(c=>c.taskId || 0), ...agentTrace.steps.flatMap(s=>(s.hitRows || []).map(h=>h.task_id || 0))])} />}
        <div className="chat-scroll" ref={scrollRef} onScroll={event => {
          const el = event.currentTarget
          followOutputRef.current = el.scrollHeight - el.scrollTop - el.clientHeight < 100
        }}>
          <div className="chat-inner">
            {messages.length === 0 ? (
              <div className="chat-empty">
                <div className="hello">
                  <BrandMark size={40} />
                  <h2>{isVideo ? '问这段视频' : `问「${scopeName}」`}</h2>
                </div>
                {!isVideo && <><p className={knowledgeStyles.intro}>把分散的视频连成可追溯的知识。比较观点、追踪主题，让每一个发现都有出处。</p><div className={knowledgeStyles.prompts}>{suggestions.map(text=><button className={knowledgeStyles.prompt} key={text} onClick={()=>{setInput(text);setMode('agent');inputRef.current?.focus()}}>{text}<span>↗</span></button>)}</div></>}
              </div>
            ) : (
              messages.map((msg, i) => msg.role === 'user'
                ? (
                  <div key={i} className="msg msg-user">
                    <div className="bubble">{msg.content}</div>
                  </div>
                )
                : (
                  <AgentMessageView
                    key={i}
                    msg={msg}
                    sessionId={session?.id}
                    fallbackTitle={scopeName}
                    onOpenEvidence={openEvidence}
                    canJump={citationJumpable}
                    onJump={jumpToCitation}
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
                <Icon name="bolt" size="sm" />Chat
              </button>
              <button className={`mode-pill${mode === 'agent' ? ' on' : ''}`} disabled={streaming} onClick={() => setMode('agent')}><Icon name="target" size="sm" />{isVideo ? 'Agent' : '跨视频研究'}</button>
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
                      onClick={() => { void switchSession(item.id); setHistoryOpen(false) }}
                    >
                      <span className="q">{item.title || '未命名会话'}</span>
                      <span className="when">{fmtRelTime(item.updated_at)}</span>
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
              <button
                type="button"
                className="meta-link chat-rail-toggle"
                onClick={() => setRailOpen(true)}
              >
                <Icon name="list" size="sm" />过程
              </button>
            </div>
            </div>
            <p className="mode-note">{MODE_NOTE[mode]}</p>
            <div className={`ask-bar${askTall ? ' tall' : ''}`} style={{ marginTop: 0 }}>
              <textarea
                ref={el => { inputRef.current = el }}
                rows={1}
                value={input}
                onChange={e => setInput(e.target.value)}
                onKeyDown={e => { if (e.key === 'Enter' && !e.shiftKey) { e.preventDefault(); submit() } }}
                placeholder={isVideo ? '问这段视频…' : '向知识库提问…'}
              />
              <button className="ask-send" disabled={streaming} onClick={() => submit()} aria-label="发送">
                <Icon name="send" />
              </button>
            </div>
            <div className="chat-status">{statusLine}</div>
            {isVideo && <div className="suggest-row" style={{ marginTop: 2 }}>
              {suggestions.map(s => (
                <button key={s} className="suggest" disabled={streaming} onClick={() => submit(s)}>{s}</button>
              ))}
            </div>}
          </div>
        </div>
      </div>

      {railOpen && <div className="chat-rail-veil"><DrawerVeil onClose={() => setRailOpen(false)} /></div>}

      <aside className={`rail-panel${railOpen ? ' open' : ''}`}>
        <div className="rail-mobile-head">
          <button type="button" className="btn btn-ic btn-ghost" onClick={() => setRailOpen(false)} aria-label="关闭">
            <Icon name="x" />
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
                  <div className="rail-empty" style={{ paddingTop: 44 }}>
                    <Icon name="target" size="lg" />
                    <p style={{ marginTop: 10 }}>
                      {agentRail.live ? '等待运行事件…' : '这次运行没有留下执行步骤(可能失败或已停止)。'}
                    </p>
                  </div>
                )}
              </>
            ) : (
              <>
                <div className="run-meta">
                  <span className="chip chip-mute mono">chat</span>
                  <span className="chip chip-mute">{lastAssistant?.traceSource === 'server' ? '实时' : '历史摘要'}</span>
                </div>
                <p style={{ fontSize: 12, color: 'var(--tx-4)', marginBottom: 10 }}>{lastAssistant?.traceSource === 'server' ? '来自服务端的实际执行进度' : '发送问题后查看执行过程'}</p>
                {ragTrace.length > 0 ? (
                  <div className="steps">
                    {ragTrace.map(step => <TraceStepView key={step.id} step={step} />)}
                  </div>
                ) : (
                  <div className="rail-empty" style={{ paddingTop: 44 }}>
                    <Icon name="target" size="lg" />
                    <p style={{ marginTop: 10 }}>提问后会显示检索过程</p>
                  </div>
                )}
              </>
            )}
        </div>
      </aside>

      {drawerCite && (
        <EvidenceDrawer
          cite={drawerCite.cite}
          fallbackTitle={scopeName}
          canJump={citationJumpable(drawerCite.cite)}
          jumpDisabledHint={isVideo && !playbackUrl ? '当前视频没有可用播放源' : undefined}
          onJump={cite => { jumpToCitation(cite); setDrawerCite(null) }}
          onClose={() => setDrawerCite(null)}
        />
      )}


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
  msg, sessionId, fallbackTitle, onOpenEvidence, canJump, onJump,
}: {
  sessionId?: number
  msg: ChatMsg
  fallbackTitle: string
  onOpenEvidence: (cite: CiteRef, cites: CiteRef[]) => void
  canJump: (cite: CiteRef) => boolean
  onJump: (cite: CiteRef) => void
}) {
  const toast = useToast()
  const cites = msg.cites || []
  const isAgentRun = !!msg.agentRun
  const agentMode = (isAgentRun ? (msg.agentMode as AgentUIMode | undefined) ?? 'agent' : undefined)
  const waitingServer = !!msg.streaming && !!agentMode && agentMode !== 'agent' && msg.content.length === 0

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
        <span style={{ color: 'var(--tx-4)' }}>{agentMode ? MODE_LABEL[agentMode] : 'Chat'}</span>
      </div>
      <ThinkingProcess message={msg} />
      {sessionId && msg.agentRunId && !msg.streaming && <RunDetails sessionId={sessionId} runId={msg.agentRunId} live={false} />}
      <div className="answer">
        <MarkdownAnswer content={msg.content} onCite={openCite} />
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
            <span className="chip chip-warn" title="生成阶段异常,回答由片段与摘要直拼,未经过完整模型生成">
              <Icon name="alert" size="sm" />降级回答
            </span>
          )}
        </div>
      )}
      {msg.error && (
        <div style={{ marginTop: 8, fontSize: 12, color: 'var(--bad)', display: 'flex', alignItems: 'center', gap: 6 }}>
          <Icon name="alert" size="sm" /><span>{msg.error}</span>
        </div>
      )}
      {cites.length > 0 && (
        <div className="cite-list">
          {cites.map((cite, i) => (
            <div key={`${cite.id}-${i}`} className="cite-card" style={{ animationDelay: `${Math.min(i * 60, 300)}ms` }} onClick={() => onOpenEvidence(cite, cites)}>
              <span className="cno">{cite.id}</span>
              <div className="cbody">
                <div className="chead">
                  <span className="cvideo">{cite.videoTitle || fallbackTitle}</span>
                  {hasReplayRange(cite) && <span className="ctime mono">{formatTimeRange(cite.startMS, cite.endMS)}</span>}
                  <ModalityTag modality={cite.modality} />
                  {cite.timeRangeStatus && cite.timeRangeStatus !== 'exact' && (
                    <span className="chip chip-mute" style={{ height: 20, fontSize: 10, padding: '0 6px' }}>{cite.timeRangeStatus === 'unknown' ? '时间未知' : '粗粒度时间'}</span>
                  )}
                </div>
                <div className="cquote">{cite.anchorQuote || cite.content}</div>
              </div>
              <button
                className="btn btn-sm cjump"
                onClick={e => {
                  e.stopPropagation()
                  if (canJump(cite)) onJump(cite)
                  else onOpenEvidence(cite, cites)
                }}
              >
                <Icon name="play" size="sm" />{canJump(cite) ? '回放' : '查看'}
              </button>
            </div>
          ))}
        </div>
      )}
      <div className="answer-meta">
        {isAgentRun ? (
          <>

            <span className="chip chip-mute mono">{agentMode || 'agent'}</span>
          </>
        ) : (
          <span className="chip chip-mute mono">chat</span>
        )}
        <button className="meta-link" onClick={copyAnswer}>
          <Icon name="file" size="sm" />复制回答
        </button>
      </div>
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
        {showHits ? (
          <div className="hits-card">
            <div className="hits-q">
              query <b>{step.query || '—'}</b>
              {typeof step.hits === 'number' && <> · {step.hits} 条命中</>}
            </div>
            {step.hitRows!.map((row, i) => (
              <div key={i} className="hit-row">
                <span className="hs mono">{typeof row.score === 'number' ? row.score.toFixed(2) : '—'}</span>
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
              {durationMs && <span className="tool-ms">{durationMs}ms</span>}
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
