'use client'

import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useRouter } from 'next/navigation'
import type { CiteRef } from '@/components/Citation'
import { formatTimeRange, hasReplayRange } from '@/components/Citation'
import { EvidenceDrawer } from '@/components/chat/EvidenceDrawer'
import { FunnelTrack } from '@/components/chat/FunnelTrack'
import { LedgerClaims, LedgerDrawer, latestClaimsByRoot } from '@/components/chat/EvidenceLedger'
import { MarkdownAnswer } from '@/components/chat/MarkdownAnswer'
import { useConversationSession } from '@/components/chat/useConversationSession'
import type { ChatTraceStep } from '@/components/chat/traceTypes'
import type { ChatMsg } from '@/components/chat/chatUtils'
import { ModalityTag } from '@/components/ui/ModalityTag'
import { VideoPlayer, type VideoPlayerHandle } from '@/components/player/VideoPlayer'
import { useToast } from '@/components/Toast'
import { Icon } from '@/components/ui/Icon'
import { BrandMark } from '@/components/ui/BrandMark'
import { api } from '@/lib/api'
import type { Citation, ChatScopeType, EvidenceLedgerView, VideoChatMode } from '@/lib/types'

// 聊天工作区:中央会话流 + 右栏(迷你播放器 / 执行过程 / 证据账本)+ 模式胶囊行。
// 单视频(/chat/v/:id)与知识库(/chat/kb/:id)共用。
// - strict 快速问答:streamAsk,SSE 只有 answer/citations/done,右栏执行过程为前端推断。
// - Agent 检证(单视频):streamAgent + agentTraceReducer 渲染真实步骤时间轴
//   (direct_qa 实际步骤 = search_transcript → build_cited_answer);run 结束按 done
//   事件的 run_id 拉取证据账本,核验未通过时显示阻断发布警示。知识库范围后端直接拒绝,保持禁用。
// - 深入研究 / 证据漏斗(单视频,实验):非流式 api.askAgent(mode=research|evidence_funnel)。
//   等待期只显示诚实状态(不做假 SSE);结果到达后在右栏一次性回放执行轨迹——
//   研究模式为 Planner 循环步骤(MaxSteps 8 / MaxReplans 2,含 investigate_visual 工具卡),
//   漏斗模式为固定八步轨道;账本/引用与 agent 模式同一套组件。

const TOP_K = 4

type ChatUIMode = Extract<VideoChatMode, 'strict_rag' | 'agent' | 'research' | 'evidence_funnel'>
type AgentUIMode = 'agent' | 'research' | 'evidence_funnel'

const MODE_LABEL: Record<AgentUIMode, string> = {
  agent: 'Agent 检证',
  research: '深入研究',
  evidence_funnel: '证据漏斗',
}

const MODE_NOTE: Record<ChatUIMode, string> = {
  strict_rag: '一次检索,直接给出带引用的回答',
  agent: '检索后生成回答,答案经独立证据核验',
  research: '受限 Planner 循环,可做查询时像素核验',
  evidence_funnel: '固定八步漏斗,逐步收窄证据范围',
}

interface LedgerState {
  loading: boolean
  view?: EvidenceLedgerView
  error?: string
}

interface ChatWorkspaceProps {
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

export function ChatWorkspace({ scopeType, targetId, scopeName, playbackUrl, refreshPlaybackUrl, suggestions }: ChatWorkspaceProps) {
  const isVideo = scopeType === 'video'
  const router = useRouter()
  const toast = useToast()
  const playerRef = useRef<VideoPlayerHandle>(null)
  const scrollRef = useRef<HTMLDivElement>(null)
  const inputRef = useRef<HTMLTextAreaElement | null>(null)
  const startedAtRef = useRef(0)

  const [input, setInput] = useState('')
  const [mode, setMode] = useState<ChatUIMode>('strict_rag')
  const [drawerCite, setDrawerCite] = useState<{ cite: CiteRef; cites: CiteRef[] } | null>(null)
  const [ledgerDrawerRun, setLedgerDrawerRun] = useState<string | null>(null)
  const [railTab, setRailTab] = useState<'run' | 'ev'>('run')
  const [elapsed, setElapsed] = useState<string | null>(null)
  const [ledgerByRun, setLedgerByRun] = useState<Record<string, LedgerState>>({})

  const {
    messages, ragTrace, agentTrace, streaming, send, stop, newSession,
  } = useConversationSession({
    scopeType,
    targetId,
    basePath: isVideo ? `/chat/v/${targetId}` : `/chat/kb/${targetId}`,
    mode,
    topK: TOP_K,
    mapCitations,
    onBeforeSend: () => {
      startedAtRef.current = performance.now()
      setElapsed(null)
      setRailTab('run')
    },
  })

  const lastMessage = messages.length > 0 ? messages[messages.length - 1] : null
  const lastAssistant = lastMessage && lastMessage.role === 'assistant' ? lastMessage : null

  // 最近一条 Agent 运行消息:账本 tab 与自动拉取都跟着它走
  const lastAgentMsg = useMemo(() => {
    for (let i = messages.length - 1; i >= 0; i--) {
      const msg = messages[i]
      if (msg.role === 'assistant' && msg.agentRunId) return msg
    }
    return null
  }, [messages])
  const lastAgentRunId = lastAgentMsg?.agentRunId ?? null

  const fetchLedger = useCallback(async (runId: string) => {
    setLedgerByRun(prev => ({ ...prev, [runId]: { ...(prev[runId] || {}), loading: true, error: undefined } }))
    try {
      const view = await api.getEvidenceLedger(runId)
      setLedgerByRun(prev => ({ ...prev, [runId]: { loading: false, view: view ?? undefined } }))
    } catch (e) {
      setLedgerByRun(prev => ({
        ...prev,
        [runId]: { loading: false, error: e instanceof Error ? e.message : '证据账本加载失败' },
      }))
    }
  }, [])

  // run 结束(done 事件把 run_id 写到消息上)自动拉取账本;出错时等用户手动重试
  useEffect(() => {
    if (!lastAgentRunId || streaming) return
    const state = ledgerByRun[lastAgentRunId]
    if (state?.loading || state?.view || state?.error) return
    void fetchLedger(lastAgentRunId)
  }, [lastAgentRunId, streaming, ledgerByRun, fetchLedger])

  const openLedgerDrawer = useCallback((runId: string) => {
    setLedgerDrawerRun(runId)
    if (!ledgerByRun[runId]) void fetchLedger(runId)
  }, [ledgerByRun, fetchLedger])

  const citesForRun = useCallback((runId: string) =>
    messages.find(m => m.role === 'assistant' && m.agentRunId === runId)?.cites || []
  , [messages])

  const modeForRun = useCallback((runId: string) =>
    messages.find(m => m.role === 'assistant' && m.agentRunId === runId)?.agentMode
  , [messages])

  useEffect(() => {
    const el = scrollRef.current
    if (el) el.scrollTop = el.scrollHeight
  }, [messages])

  useEffect(() => {
    if (!streaming && startedAtRef.current > 0) {
      setElapsed(((performance.now() - startedAtRef.current) / 1000).toFixed(1))
      startedAtRef.current = 0
    }
  }, [streaming])

  const autoGrow = (el: HTMLTextAreaElement | null) => {
    if (!el) return
    el.style.height = 'auto'
    el.style.height = `${Math.min(120, el.scrollHeight)}px`
  }

  const submit = useCallback((text?: string) => {
    const q = (text ?? input).trim()
    if (!q) { toast.info('先输入一个问题'); return }
    if (streaming) return
    setInput('')
    if (inputRef.current) inputRef.current.style.height = 'auto'
    void send(q)
  }, [input, streaming, send, toast])

  const openEvidence = useCallback((cite: CiteRef, cites: CiteRef[]) => {
    setDrawerCite({ cite, cites })
  }, [])

  // 账本抽屉里点开证据详情时先收起账本抽屉,避免两个抽屉叠放
  const openEvidenceFromLedger = useCallback((cite: CiteRef, cites: CiteRef[]) => {
    setLedgerDrawerRun(null)
    setDrawerCite({ cite, cites })
  }, [])

  const jumpToCitation = useCallback((cite: CiteRef) => {
    if (isVideo) {
      playerRef.current?.seek(cite.startMS || 0, true)
    } else if (cite.taskId) {
      // 知识库范围没有统一的迷你播放器:跳到该片段所属视频的工作台
      router.push(`/video/${cite.taskId}`)
    }
  }, [isVideo, router])

  const citationJumpable = useCallback((cite: CiteRef) =>
    (isVideo ? !!playbackUrl : !!cite.taskId) && hasReplayRange(cite)
  , [isVideo, playbackUrl])

  // 右栏执行过程:Agent 运行用真实 trace(进行中或刚结束),否则历史 Agent 消息用快照 trace。
  // 历史消息即使没有步骤(运行失败/被停止)也保留其模式,避免误显示成 strict 推断面板。
  const agentRail = useMemo(() => {
    if (agentTrace.runId != null || agentTrace.steps.length > 0) {
      return { steps: agentTrace.steps, runId: agentTrace.runId, mode: agentTrace.mode ?? undefined, live: true }
    }
    if (lastAssistant?.agentRun) {
      return { steps: lastAssistant.trace ?? [], runId: lastAssistant.agentRunId ?? null, mode: lastAssistant.agentMode, live: streaming }
    }
    return null
  }, [agentTrace, streaming, lastAssistant])

  // 非流式研究/漏斗的等待期:没有任何事件流,只保留诚实状态,不模拟逐步进度
  const pendingExperimental = (streaming && agentTrace.steps.length === 0
    && (agentTrace.mode === 'research' || agentTrace.mode === 'evidence_funnel'))
    ? agentTrace.mode as 'research' | 'evidence_funnel'
    : null

  const agentBlocked = !streaming && !!lastAssistant?.agentRun && !!lastAssistant.degraded

  const statusLine = (() => {
    if (streaming) {
      const generating = lastMessage?.role === 'assistant' && lastMessage.content.length > 0
      if ((mode === 'research' || mode === 'evidence_funnel') && !generating) {
        return (
          <>
            <span className="pulse" />
            <span>
              {mode === 'research'
                ? '深入研究运行中…(非流式接口,完成后一次性回放轨迹)'
                : '证据漏斗运行中…(非流式接口,完成后一次性回放轨迹)'}
            </span>
            <button className="meta-link stop" onClick={stop}>停止</button>
          </>
        )
      }
      return (
        <>
          <span className="pulse" />
          <span>{generating ? '正在生成回答…' : mode === 'agent' ? '正在检索与核验…' : '检索中,稍等…'}</span>
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
          <Icon name="shield" size="sm" />核验未通过,回答被阻断发布
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

  const ledgerTabBody = (() => {
    if (!lastAgentRunId) {
      return (
        <div className="rail-empty" style={{ paddingTop: 44 }}>
          <Icon name="shield-check" size="lg" />
          <p style={{ marginTop: 10 }}>
            快速问答不写证据账本。<br />完成一次 Agent 检证、深入研究或证据漏斗后,<br />每条事实的支撑情况会列在这里。
          </p>
        </div>
      )
    }
    const state = ledgerByRun[lastAgentRunId]
    if (!state || state.loading) {
      return (
        <div className="rail-empty" style={{ paddingTop: 44 }}>
          <Icon name="shield-check" size="lg" />
          <p style={{ marginTop: 10 }}>正在加载证据账本…</p>
        </div>
      )
    }
    if (state.error) {
      return (
        <div className="rail-empty" style={{ paddingTop: 44 }}>
          <Icon name="alert" size="lg" />
          <p style={{ marginTop: 10 }}>{state.error}</p>
          <button className="btn btn-sm" style={{ marginTop: 10 }} onClick={() => void fetchLedger(lastAgentRunId)}>
            <Icon name="refresh" size="sm" />重试
          </button>
        </div>
      )
    }
    if (!state.view) {
      return (
        <div className="rail-empty" style={{ paddingTop: 44 }}>
          <p>这次运行没有留下证据账本。</p>
        </div>
      )
    }
    return (
      <>
        <div className="run-meta">
          <span className="chip chip-mute mono">{lastAgentMsg?.agentMode || 'agent'}</span>
          <span className="chip chip-mute">
            {latestClaimsByRoot(state.view.claims).length} 条 claim · {(state.view.evidence ?? []).length} 条证据
          </span>
        </div>
        <LedgerClaims
          view={state.view}
          cites={lastAgentMsg?.cites || []}
          onOpenEvidence={openEvidence}
          onCorrected={() => void fetchLedger(lastAgentRunId)}
        />
      </>
    )
  })()

  return (
    <div className="chat-wrap">
      <div className="chat-col">
        <div className="chat-scroll" ref={scrollRef}>
          <div className="chat-inner">
            {messages.length === 0 ? (
              <div className="chat-empty">
                <div className="hello">
                  <BrandMark size={40} />
                  <h2>{isVideo ? '问这段视频' : `问「${scopeName}」`}</h2>
                </div>
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
                    fallbackTitle={scopeName}
                    onOpenEvidence={openEvidence}
                    canJump={citationJumpable}
                    onJump={jumpToCitation}
                    onOpenLedger={msg.agentRunId ? () => openLedgerDrawer(msg.agentRunId as string) : undefined}
                    claimsCount={msg.agentRunId && ledgerByRun[msg.agentRunId]?.view
                      ? latestClaimsByRoot(ledgerByRun[msg.agentRunId]!.view!.claims).length
                      : undefined}
                  />
                )
              )
            )}
          </div>
        </div>

        <div className="composer">
          <div className="composer-inner">
            <div className="mode-row">
              <button
                className={`mode-pill${mode === 'strict_rag' ? ' on' : ''}`}
                disabled={streaming}
                onClick={() => setMode('strict_rag')}
              >
                <Icon name="bolt" size="sm" />快速问答
              </button>
              {isVideo ? (
                <>
                  <button
                    className={`mode-pill${mode === 'agent' ? ' on' : ''}`}
                    disabled={streaming}
                    onClick={() => setMode('agent')}
                  >
                    <Icon name="target" size="sm" />Agent 检证
                  </button>
                  <button
                    className={`mode-pill${mode === 'research' ? ' on' : ''}`}
                    disabled={streaming}
                    onClick={() => setMode('research')}
                  >
                    <Icon name="zoom-scan" size="sm" />深入研究
                    <span className="chip chip-mute" style={{ height: 18, fontSize: 10, padding: '0 6px' }}>实验</span>
                  </button>
                  <button
                    className={`mode-pill${mode === 'evidence_funnel' ? ' on' : ''}`}
                    disabled={streaming}
                    onClick={() => setMode('evidence_funnel')}
                  >
                    <Icon name="filter" size="sm" />证据漏斗
                    <span className="chip chip-mute" style={{ height: 18, fontSize: 10, padding: '0 6px' }}>实验</span>
                  </button>
                </>
              ) : (
                <>
                  <button className="mode-pill" disabled title="知识库范围的 Agent 后端会直接拒绝,当前仅支持快速问答">
                    <Icon name="target" size="sm" />Agent 检证
                  </button>
                  <button className="mode-pill" disabled title="研究模式仅支持单视频会话">
                    <Icon name="zoom-scan" size="sm" />深入研究
                  </button>
                  <button className="mode-pill" disabled title="证据漏斗仅支持单视频会话">
                    <Icon name="filter" size="sm" />证据漏斗
                  </button>
                </>
              )}
              <span className="mode-note">{MODE_NOTE[mode]}</span>
              <button className="meta-link" style={{ marginLeft: 8 }} onClick={() => newSession()} disabled={streaming}>
                新会话
              </button>
            </div>
            <div className="ask-bar" style={{ marginTop: 0 }}>
              <textarea
                ref={el => { inputRef.current = el; autoGrow(el) }}
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
            <div className="suggest-row" style={{ marginTop: 2 }}>
              {suggestions.map(s => (
                <button key={s} className="suggest" disabled={streaming} onClick={() => submit(s)}>{s}</button>
              ))}
            </div>
          </div>
        </div>
      </div>

      <aside className="rail-panel">
        {isVideo && playbackUrl && (
          <div style={{ margin: '14px 14px 0' }}>
            <VideoPlayer ref={playerRef} src={playbackUrl} title={scopeName} compact className="mini-player" onNeedRefresh={refreshPlaybackUrl} />
          </div>
        )}
        <div className="rail-tabs">
          <button className={`rail-tab${railTab === 'run' ? ' on' : ''}`} onClick={() => setRailTab('run')}>
            执行过程
          </button>
          <button className={`rail-tab${railTab === 'ev' ? ' on' : ''}`} onClick={() => setRailTab('ev')}>
            证据账本
            <span className="badge">
              {lastAgentRunId && ledgerByRun[lastAgentRunId]?.view
                ? latestClaimsByRoot(ledgerByRun[lastAgentRunId]!.view!.claims).length
                : 0}
            </span>
          </button>
        </div>
        <div className="rail-body">
          {railTab === 'run' ? (
            pendingExperimental ? (
              <>
                <RunHeader mode={pendingExperimental} runId={null} />
                <p style={{ fontSize: 12, color: 'var(--tx-4)', marginBottom: 10 }}>运行中…</p>
                {pendingExperimental === 'evidence_funnel' && <FunnelTrack steps={[]} />}
                <div className="rail-empty" style={{ paddingTop: 34 }}>
                  <span className="pulse" style={{ marginBottom: 10 }} />
                  <p style={{ marginTop: 10 }}>
                    {pendingExperimental === 'research' ? '深入研究运行中…' : '证据漏斗运行中…'}
                    <br />完成后在这里一次性回放执行轨迹。
                  </p>
                </div>
              </>
            ) : agentRail ? (
              <>
                <RunHeader mode={(agentRail.mode as AgentUIMode) || 'agent'} runId={agentRail.runId} />
                <p style={{ fontSize: 12, color: 'var(--tx-4)', marginBottom: 10 }}>
                  {agentRail.mode === 'research' ? '受限研究循环' : agentRail.mode === 'evidence_funnel' ? '固定漏斗' : '检索后核验发布'}
                </p>
                {agentRail.mode === 'evidence_funnel' && <FunnelTrack steps={agentRail.steps} />}
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
                  <span className="chip chip-mute mono">strict_rag</span>
                  <span className="chip chip-warn">推断</span>
                </div>
                <p style={{ fontSize: 12, color: 'var(--tx-4)', marginBottom: 10 }}>检索过程由前端推断</p>
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
            )
          ) : ledgerTabBody}
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

      {ledgerDrawerRun && (
        <LedgerDrawer
          runId={ledgerDrawerRun}
          view={ledgerByRun[ledgerDrawerRun]?.view}
          loading={ledgerByRun[ledgerDrawerRun]?.loading}
          error={ledgerByRun[ledgerDrawerRun]?.error}
          cites={citesForRun(ledgerDrawerRun)}
          modeLabel={modeForRun(ledgerDrawerRun) || 'agent'}
          onRetry={runId => void fetchLedger(runId)}
          onOpenEvidence={openEvidenceFromLedger}
          onClose={() => setLedgerDrawerRun(null)}
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
  msg, fallbackTitle, onOpenEvidence, canJump, onJump, onOpenLedger, claimsCount,
}: {
  msg: ChatMsg
  fallbackTitle: string
  onOpenEvidence: (cite: CiteRef, cites: CiteRef[]) => void
  canJump: (cite: CiteRef) => boolean
  onJump: (cite: CiteRef) => void
  onOpenLedger?: () => void
  claimsCount?: number
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
        <span style={{ color: 'var(--tx-4)' }}>{agentMode ? MODE_LABEL[agentMode] : '快速问答'}</span>
      </div>
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
            <span className="chip chip-warn" title="独立核验未通过,回答已被替换为阻断文案,引用仅供核对">
              <Icon name="shield" size="sm" />核验未通过,阻断发布
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
                    <span className="chip chip-mute" style={{ height: 20, fontSize: 10, padding: '0 6px' }}>粗粒度时间</span>
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
            {onOpenLedger && (
              <button className="meta-link acc" onClick={onOpenLedger}>
                <Icon name="shield-check" size="sm" />证据账本{typeof claimsCount === 'number' ? ` · ${claimsCount} 条 claim` : ''}
              </button>
            )}
            <span className="chip chip-mute mono">{agentMode || 'agent'}</span>
          </>
        ) : (
          <span className="chip chip-mute mono">strict_rag</span>
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
