import { useEffect, useLayoutEffect, useRef, useState } from "react"
import type { ActivityStep, Citation, Frame, TurnView } from "../contract/types"
import { describeTurn } from "../app/labels"
import { clipText, contextQuote, formatClock } from "../contract/text"
import { AnswerBody } from "./AnswerBody"
import { ActivityList } from "./ActivityList"
import { ReferenceCard } from "./ReferenceCard"
import { prefersReduced } from "./motion"

export function QaPanel({
  open,
  historyState,
  turns,
  stepsFor,
  frames,
  draft,
  frameChip,
  passageChip,
  prompts,
  busy,
  error,
  expanded,
  focusToken,
  onDraft,
  onSend,
  onStop,
  onRetry,
  onClose,
  onToggle,
  onSuggest,
  onRemoveFrame,
  onRemovePassage,
  onOpenFrame,
  onCite,
  onBroken,
  externalAllowed,
  externalAvailable,
  searchAvailable,
  referenceLinks,
  onExternal,
  onReferenceLinks,
  modelUnavailable,
  unavailableNote = "原模型暂不可用，问题草稿已保留。文章和下载仍可使用。",
  staged = false,
  onStage,
  onPair,
  onOpenArticle,
}: {
  open: boolean
  historyState?: Record<string, string>
  turns: TurnView[]
  stepsFor: (turn: TurnView) => ActivityStep[]
  frames: Map<string, Frame>
  draft: string
  frameChip: { id: string; label: string } | null
  passageChip: { quote: string } | null
  prompts: string[]
  busy: boolean
  error: string
  expanded: Record<string, boolean | undefined>
  focusToken: number
  onDraft: (value: string) => void
  onSend: () => void
  onStop: () => void
  onRetry: (turn: TurnView) => void
  onClose: () => void
  onToggle: (id: string, open: boolean) => void
  onSuggest: (prompt: string) => void
  onRemoveFrame: () => void
  onRemovePassage: () => void
  onOpenFrame: (frames: Frame[], index: number, origin: HTMLElement) => void
  onCite: (citation: Citation) => void
  onBroken: (frame: Frame) => void
  externalAllowed: boolean
  externalAvailable: boolean
  searchAvailable: boolean
  referenceLinks: string
  onExternal: (v: boolean) => void
  onReferenceLinks: (v: string) => void
  modelUnavailable: boolean
  unavailableNote?: string
  staged?: boolean
  onStage?: () => void
  onPair?: () => void
  onOpenArticle?: () => void
}) {
  const sectionRef = useRef<HTMLElement>(null)
  const fieldRef = useRef<HTMLTextAreaElement>(null)
  const logRef = useRef<HTMLDivElement>(null)
  const nearBottom = useRef(true)
  const [newAnswer, setNewAnswer] = useState(false)
  const previousStatus = useRef(new Map<string, string>())
  const active = turns.find((turn) => !["completed", "failed", "canceled"].includes(turn.turn.status))

  useEffect(() => {
    const section = sectionRef.current
    if (section) section.inert = !open
  }, [open])

  useEffect(() => {
    const field = fieldRef.current
    if (!field) return
    field.style.height = "auto"
    field.style.height = `${Math.min(field.scrollHeight, 120)}px`
  }, [draft])

  useEffect(() => {
    if (!open || !focusToken) return
    fieldRef.current?.focus({ preventScroll: true })
  }, [open, focusToken])

  useLayoutEffect(() => {
    for (const turn of turns) {
      const before = previousStatus.current.get(turn.turn.id)
      if (before && before !== "completed" && turn.turn.status === "completed" && !prefersReduced()) {
        const node = Array.from(sectionRef.current?.querySelectorAll<HTMLElement>(".turn") || [])
          .find((el) => el.dataset.turnId === turn.turn.id)
          ?.querySelector<HTMLElement>(".turn-answer")
        node?.setAttribute("data-arrive", "true")
      }
      previousStatus.current.set(turn.turn.id, turn.turn.status)
    }
  }, [turns])

  useLayoutEffect(() => {
    const log = logRef.current
    if (!log) return
    if (nearBottom.current) {
      log.scrollTop = log.scrollHeight
      setNewAnswer(false)
    } else if (turns.length) setNewAnswer(true)
  }, [turns, open])

  return (
    <section id="qa" className="qa" aria-labelledby="qa-title" aria-hidden={!open} ref={sectionRef}>
      <header className="qa-head">
        <div>
          <h2 id="qa-title">问这条视频</h2>
          <p>只解释这条视频和文章，不会改写正文。</p>
        </div>
        <div className="qa-actions">
          {onStage && onPair ? (
            <div className="qa-view" role="group" aria-label="阅读布局">
              <button type="button" aria-pressed={!staged} onClick={onPair}>并排</button>
              <button type="button" aria-pressed={!!staged} onClick={onStage}>问答为主</button>
            </div>
          ) : null}
          {staged ? (
            <button type="button" id="qa-open-article" className="qa-paper" onClick={onOpenArticle}>
              图文
            </button>
          ) : null}
          <button type="button" className="icon-btn" aria-label="收起问答" onClick={onClose}>
            <svg viewBox="0 0 16 16" aria-hidden="true">
              <path d="M4 4l8 8M12 4l-8 8" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
            </svg>
          </button>
        </div>
      </header>
      <div
        className="qa-log"
        ref={logRef}
        onScroll={() => {
          const log = logRef.current
          if (log) {
            nearBottom.current = log.scrollHeight - log.scrollTop - log.clientHeight < 80
            if (nearBottom.current) setNewAnswer(false)
          }
        }}
      >
        {!turns.length ? (
          <div className="qa-empty">
            <p>可以问文章里的内容，也可以从某张图问起。</p>
            <div className="qa-prompts">
              {prompts.map((prompt) => (
                <button type="button" key={prompt} onClick={() => onSuggest(prompt)}>
                  {prompt}
                </button>
              ))}
            </div>
          </div>
        ) : null}
        {turns.map((turn) => {
          const steps = stepsFor(turn)
          const current = steps.find((step) => step.id === turn.activity.current_step_id) ?? null
          const described = describeTurn(turn, current)
          const isOpen = expanded[turn.turn.id] ?? described.open
          const answer = turn.turn.answer
          const frame = turn.turn.context.frame_id ? frames.get(turn.turn.context.frame_id) : undefined
          return (
            <article className="turn" data-turn-id={turn.turn.id} key={turn.turn.id}>
              <div className="question-message" aria-label="你的提问">
              <div className="message-byline question-byline"><span className="user-monogram" aria-hidden="true">你</span><span>你的提问</span></div>
              <h3 className="turn-q">{turn.turn.question}</h3>
              <div className="turn-chips">
                {frame ? (
                  <span className="chip static">
                    <span>
                      {formatClock(frame.time_ms)} · {frame.caption || "画面"}
                    </span>
                  </span>
                ) : null}
                {contextQuote(turn.turn.context) ? (
                  <span className="chip static">
                    <span title={contextQuote(turn.turn.context)}>{clipText(contextQuote(turn.turn.context), 18)}</span>
                  </span>
                ) : null}
              </div>
              </div>
              <div className="assistant-message" aria-label="VidLens 的回答">
              <div className="message-byline assistant-byline"><svg viewBox="0 0 24 24" aria-hidden="true"><circle cx="11" cy="10" r="6"/><path d="m15.5 14.5 5 5M4 21h10"/></svg><strong>VidLens</strong><span>视频问答</span></div>
              <div className="turn-activity">
                <button
                  type="button"
                  className="turn-summary"
                  hidden={!["completed", "failed", "canceled"].includes(turn.turn.status)}
                  aria-expanded={isOpen}
                  onClick={() => onToggle(turn.turn.id, !isOpen)}
                >
                  {described.summary || described.title}
                </button>
                <div className="disclose" data-open={isOpen ? "true" : "false"}>
                  <div className="disclose-inner">
                    {described.title && !steps.length ? <p className="turn-title">{described.title}</p> : null}
                    <ActivityList steps={steps} resourceStatus={turn.turn.status} detailed open onOpenFrame={onOpenFrame} onBroken={onBroken} />
                    {historyState?.[turn.turn.id] ? <p className="history-note" role="status">{historyState[turn.turn.id]}</p> : null}
                  </div>
                </div>
              </div>
              {turn.turn.status === "completed" && answer ? (
                <>
                  <AnswerBody answer={answer} frames={frames} onOpenFrame={onOpenFrame} onAskFrame={() => undefined} onCite={onCite} onBroken={onBroken} />
                  {Object.values(answer.citations || {})
                    .filter((c) => c.kind === "external_reference")
                    .map((c) => (
                      <ReferenceCard key={c.id} citation={c} requestId={turn.turn.request_id} />
                    ))}
                </>
              ) : null}
              {!answer && !["completed", "failed", "canceled"].includes(turn.turn.status) && !steps.length ? (
                <p className="quiet-note">{described.title || "正在组织回答"}</p>
              ) : null}
              {turn.turn.status === "canceled" ? (
                <div className="turn-answer">
                  <p className="turn-stopped">没有继续生成回答。</p>
                </div>
              ) : null}
              {turn.turn.status === "failed" ? (
                <div className="turn-answer">
                  <p className="form-error">{turn.failure?.message || "没能完成回答。"}</p>
                  {turn.failure?.diagnostic_id ? <small className="meta">错误编号：{turn.failure.diagnostic_id}</small> : null}
                </div>
              ) : null}
              {turn.actions.can_retry ? (
                <button type="button" className="disclosure" onClick={() => onRetry(turn)}>
                  重新回答
                </button>
              ) : null}
              </div>
            </article>
          )
        })}
        {newAnswer ? (
          <button
            type="button"
            className="new-answer"
            onClick={() => {
              if (logRef.current) logRef.current.scrollTop = logRef.current.scrollHeight
              nearBottom.current = true
              setNewAnswer(false)
            }}
          >
            新回答 ↓
          </button>
        ) : null}
      </div>
      <form
        className="composer"
        onSubmit={(event) => {
          event.preventDefault()
          onSend()
        }}
      >
        <div className="chips">
          {frameChip ? (
            <span className="chip">
              <span>{frameChip.label}</span>
              <button type="button" aria-label="移除这张图" onClick={onRemoveFrame}>
                ×
              </button>
            </span>
          ) : null}
          {passageChip ? (
            <span className="chip">
              <span title={passageChip.quote}>{clipText(passageChip.quote, 18)}</span>
              <button type="button" aria-label="移除选中内容" onClick={onRemovePassage}>
                ×
              </button>
            </span>
          ) : null}
        </div>
        {error ? <p className="form-error">{error}</p> : null}
        {modelUnavailable ? <p className="composer-reason">{unavailableNote}</p> : null}
        <div className="external-option">
          <label className="ink-check">
            <input
              type="checkbox"
              checked={externalAllowed}
              disabled={!externalAvailable || busy || !!active || modelUnavailable}
              onChange={(e) => onExternal(e.target.checked)}
            />
            <span aria-hidden="true" />
            补充网络资料
          </label>
          <span>{externalAvailable ? (searchAvailable ? "已知网页优先 · 免费额度内搜索" : "可读取已知公开网页") : "站长尚未启用"}</span>
        </div>
        <div className="fold" data-open={externalAllowed ? "true" : "false"}>
          <div className="fold-clip">
            <label className="reference-input">
              提供网页链接（可选，每行一条，最多 3 条）
              <textarea
                rows={2}
                value={referenceLinks}
                tabIndex={externalAllowed ? 0 : -1}
                aria-hidden={externalAllowed ? undefined : true}
                onChange={(e) => onReferenceLinks(e.target.value)}
                placeholder="https://…"
              />
            </label>
          </div>
        </div>
        <div className="composer-field">
          <label className="sr-only" htmlFor="composer">
            问题
          </label>
          <textarea
            id="composer"
            ref={fieldRef}
            rows={2}
            placeholder="就这条视频提问"
            value={draft}
            onChange={(event) => onDraft(event.target.value)}
            onKeyDown={(event) => {
              if (event.key === "Enter" && !event.shiftKey && !event.nativeEvent.isComposing) {
                event.preventDefault()
                if (!busy && draft.trim() && !active && !modelUnavailable) onSend()
              }
            }}
          />
          <button
            type="submit"
            className="send"
            aria-label="发送"
            title={modelUnavailable ? "原模型暂不可用" : "发送"}
            disabled={busy || !draft.trim() || !!active || modelUnavailable}
            hidden={!!active}
          >
            <svg viewBox="0 0 16 16" aria-hidden="true">
              <path
                d="M8 12.5V3.5M4.5 6.5L8 3l3.5 3.5"
                fill="none"
                stroke="currentColor"
                strokeWidth="1.5"
                strokeLinecap="round"
                strokeLinejoin="round"
              />
            </svg>
          </button>
          <button type="button" className="send stop-send" hidden={!active?.actions.can_cancel} onClick={onStop}>
            停止
          </button>
        </div>
      </form>
    </section>
  )
}
