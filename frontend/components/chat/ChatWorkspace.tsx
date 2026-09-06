'use client'

import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useRouter } from 'next/navigation'
import type { CiteRef } from '@/components/Citation'
import { formatTimeRange, hasReplayRange } from '@/components/Citation'
import { parseAnswerTokens, type AnswerToken } from '@/components/chat/answerTokens'
import { EvidenceDrawer } from '@/components/chat/EvidenceDrawer'
import { useConversationSession } from '@/components/chat/useConversationSession'
import type { ChatTraceStep } from '@/components/chat/traceTypes'
import type { ChatMsg } from '@/components/chat/chatUtils'
import { ModalityTag } from '@/components/ui/ModalityTag'
import { VideoPlayer, type VideoPlayerHandle } from '@/components/player/VideoPlayer'
import { useToast } from '@/components/Toast'
import { Icon } from '@/components/ui/Icon'
import type { Citation, ChatScopeType } from '@/lib/types'

// 聊天工作区:中央会话流 + 右栏(迷你播放器 / 执行过程 / 证据账本)+ 模式胶囊行。
// 单视频(/chat/v/:id)与知识库(/chat/kb/:id)共用;本阶段接入 strict 快速问答
// (streamAsk,SSE 只有 answer/citations/done)。Agent/研究/漏斗按后端能力以禁用态呈现:
// 知识库范围后端直接拒绝,单视频 Agent 流式在下一阶段接入,研究/漏斗当前为非流式接口。

const TOP_K = 4

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

export function ChatWorkspace({ scopeType, targetId, scopeName, playbackUrl, refreshPlaybackUrl, suggestions }: ChatWorkspaceProps) {
  const isVideo = scopeType === 'video'
  const router = useRouter()
  const toast = useToast()
  const playerRef = useRef<VideoPlayerHandle>(null)
  const scrollRef = useRef<HTMLDivElement>(null)
  const inputRef = useRef<HTMLTextAreaElement | null>(null)
  const startedAtRef = useRef(0)

  const [input, setInput] = useState('')
  const [drawerCite, setDrawerCite] = useState<{ cite: CiteRef; cites: CiteRef[] } | null>(null)
  const [railTab, setRailTab] = useState<'run' | 'ev'>('run')
  const [elapsed, setElapsed] = useState<string | null>(null)

  const {
    messages, ragTrace, streaming, send, stop, newSession,
  } = useConversationSession({
    scopeType,
    targetId,
    basePath: isVideo ? `/chat/v/${targetId}` : `/chat/kb/${targetId}`,
    mode: 'strict_rag',
    topK: TOP_K,
    mapCitations,
    onBeforeSend: () => {
      startedAtRef.current = performance.now()
      setElapsed(null)
      setRailTab('run')
    },
  })

  const lastMessage = messages.length > 0 ? messages[messages.length - 1] : null

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

  const statusLine = (() => {
    if (streaming) {
      const generating = lastMessage?.role === 'assistant' && lastMessage.content.length > 0
      return (
        <>
          <span className="pulse" />
          <span>{generating ? '正在生成回答…' : '检索中,稍等…'}</span>
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
        <div className="chat-scroll" ref={scrollRef}>
          <div className="chat-inner">
            {messages.length === 0 ? (
              <div className="chat-empty">
                <div className="hello">
                  <div className="brand-mark" />
                  <h2>{isVideo ? '问这段视频' : `问「${scopeName}」`}</h2>
                </div>
                <p>每个回答都带可回放的时间点引用。点下面的问题,或直接输入。</p>
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
                  />
                )
              )
            )}
          </div>
        </div>

        <div className="composer">
          <div className="composer-inner">
            <div className="mode-row">
              <button className="mode-pill on" disabled={streaming}>
                <Icon name="bolt" size="sm" />快速问答
              </button>
              {isVideo ? (
                <>
                  <button className="mode-pill" disabled title="Agent 检证将在下一阶段接入:检索后生成,回答经独立证据核验">
                    <Icon name="target" size="sm" />Agent 检证
                  </button>
                  <button className="mode-pill" disabled title="研究模式当前为非流式接口,尚未接入前端">
                    <Icon name="zoom-scan" size="sm" />深入研究
                    <span className="chip chip-mute" style={{ height: 18, fontSize: 10, padding: '0 6px' }}>实验</span>
                  </button>
                  <button className="mode-pill" disabled title="证据漏斗当前为非流式接口,尚未接入前端">
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
                </>
              )}
              <span className="mode-note">一次检索,直接给出带引用的回答</span>
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
                placeholder={isVideo ? '问这段视频…回答会标注口述还是画面' : '向整个知识库提问…回答会注明每个片段来自哪场视频'}
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
            证据账本<span className="badge">0</span>
          </button>
        </div>
        <div className="rail-body">
          {railTab === 'run' ? (
            <>
              <div className="run-meta">
                <span className="chip chip-mute mono">strict_rag</span>
                <span className="chip chip-warn">推断</span>
              </div>
              <p style={{ fontSize: 11, color: 'var(--tx-4)', marginBottom: 10 }}>
                实际流事件只有 answer / citations / done,以下检索过程由前端推断展示。
              </p>
              {ragTrace.length > 0 ? (
                <div className="steps">
                  {ragTrace.map(step => <TraceStepView key={step.id} step={step} />)}
                </div>
              ) : (
                <div className="rail-empty" style={{ paddingTop: 44 }}>
                  <Icon name="target" size="lg" />
                  <p style={{ marginTop: 10 }}>发起一次提问后,<br />检索与生成状态会出现在这里。</p>
                </div>
              )}
            </>
          ) : (
            <div className="rail-empty" style={{ paddingTop: 44 }}>
              <Icon name="shield-check" size="lg" />
              <p style={{ marginTop: 10 }}>
                快速问答不写证据账本。<br />完成一次 Agent 检证后,每条事实的支撑情况会列在这里。
              </p>
            </div>
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

function AgentMessageView({
  msg, fallbackTitle, onOpenEvidence, canJump, onJump,
}: {
  msg: ChatMsg
  fallbackTitle: string
  onOpenEvidence: (cite: CiteRef, cites: CiteRef[]) => void
  canJump: (cite: CiteRef) => boolean
  onJump: (cite: CiteRef) => void
}) {
  const toast = useToast()
  const tokens = useMemo(() => parseAnswerTokens(msg.content), [msg.content])
  const cites = msg.cites || []

  const openCite = (no: number) => {
    const hit = cites.find(c => c.id === `C${no}`)
    if (hit) onOpenEvidence(hit, cites)
  }

  const paragraphs = useMemo(() => splitParagraphs(tokens), [tokens])

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
        <span className="agent-mark"><Icon name="bolt" /></span>
        映知
        <span style={{ color: 'var(--tx-4)' }}>快速问答</span>
      </div>
      <div className="answer">
        {paragraphs.map((para, pi) => (
          <p key={pi}>
            {para.map((tk, ti) => typeof tk === 'string'
              ? <span key={ti}>{tk}</span>
              : (
                <button key={ti} className="cite" title="查看证据详情" onClick={() => openCite(tk.cite)}>
                  C{tk.cite}
                </button>
              ))}
          </p>
        ))}
        {msg.streaming && <span className="stream-cursor" />}
      </div>
      {msg.degraded && (
        <div style={{ marginTop: 8 }}>
          <span className="chip chip-warn" title="生成阶段异常,回答由片段与摘要直拼,未经过完整模型生成">
            <Icon name="alert" size="sm" />降级回答
          </span>
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
        <span className="chip chip-mute mono">strict_rag</span>
        <button className="meta-link" onClick={copyAnswer}>
          <Icon name="file" size="sm" />复制回答
        </button>
      </div>
    </div>
  )
}

function splitParagraphs(tokens: AnswerToken[]): AnswerToken[][] {
  const paras: AnswerToken[][] = [[]]
  for (const tk of tokens) {
    if (typeof tk === 'string') {
      tk.split('\n').forEach((seg, i) => {
        if (i > 0) paras.push([])
        if (seg) paras[paras.length - 1].push(seg)
      })
    } else {
      paras[paras.length - 1].push(tk)
    }
  }
  return paras
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

