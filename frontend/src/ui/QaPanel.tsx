import { useEffect, useLayoutEffect, useRef, useState } from "react"
import type { ActivityStep, Citation, Frame, TurnView } from "../contract/types"
import { answerKicker, describeTurn, stepPresentation } from "../app/labels"
import { clipText, formatClock } from "../contract/text"
import { BlockView } from "./Blocks"
import { FrameThumb } from "./FrameThumb"
import { ReferenceCard } from "./ReferenceCard"
import { prefersReduced } from "./motion"

export function QaPanel({
  open,
  turns,
  stepsFor,
  frames,
  draft,
  frameChip,
  passageChip,
  suggesting,
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
}: {
  open: boolean
  turns: TurnView[]
  stepsFor: (turn: TurnView) => ActivityStep[]
  frames: Map<string, Frame>
  draft: string
  frameChip: { id: string; label: string } | null
  passageChip: { quote: string } | null
  suggesting: string
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
  onSuggest: () => void
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
        const animation = node?.animate(
          [
            { opacity: 0, transform: "translateY(8px)" },
            { opacity: 1, transform: "none" },
          ],
          { duration: 200, easing: "cubic-bezier(0.23, 1, 0.32, 1)" },
        )
        if (animation) animation.onfinish = () => animation.cancel()
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
        <button type="button" className="icon-btn" aria-label="收起问答" onClick={onClose}>
          <svg viewBox="0 0 16 16" aria-hidden="true">
            <path d="M4 4l8 8M12 4l-8 8" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
          </svg>
        </button>
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
            <button type="button" className="suggest" onClick={onSuggest}>
              {suggesting}
            </button>
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
              <h3 className="turn-q">{turn.turn.question}</h3>
              <div className="turn-chips">
                {frame ? (
                  <span className="chip static">
                    <span>
                      {formatClock(frame.time_ms)} · {frame.caption || "画面"}
                    </span>
                  </span>
                ) : null}
                {turn.turn.context.passage ? (
                  <span className="chip static">
                    <span>{clipText(turn.turn.context.passage.quote, 18)}</span>
                  </span>
                ) : null}
              </div>
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
                    <p className="turn-title">{described.title}</p>
                    <div className="turn-steps">
                      {steps.map((step) => {
                        const view = stepPresentation(step, turn.turn.status)
                        return (
                          <div className="step" role="listitem" key={step.id} data-status={view.status}>
                            <span className="mark" aria-hidden="true" />
                            <div>
                              <div className="step-label">
                                {step.title}
                                <span className="sr-only">
                                  {view.status === "done" ? "已完成" : view.status === "active" ? "进行中" : "已停止"}
                                </span>
                              </div>
                              {view.outcome && view.status !== "active" ? <div className="step-outcome">{view.outcome}</div> : null}
                              {step.frames?.length ? (
                                <div className="mini-row">
                                  {step.frames.map((item, index) => (
                                    <button
                                      type="button"
                                      className="mini-shot"
                                      key={item.id}
                                      onClick={(event) =>
                                        onOpenFrame(
                                          step.frames,
                                          index,
                                          event.currentTarget.querySelector(".shot-host") || event.currentTarget,
                                        )
                                      }
                                    >
                                      <span className="shot-host">
                                        <FrameThumb frame={item} variant="thumbnail" onBroken={onBroken} />
                                      </span>
                                      <span className="mini-time">{formatClock(item.time_ms)}</span>
                                    </button>
                                  ))}
                                </div>
                              ) : null}
                            </div>
                          </div>
                        )
                      })}
                    </div>
                  </div>
                </div>
              </div>
              {turn.turn.status === "completed" && answer ? (
                <div className="turn-answer">
                  {answerKicker(answer.kind) ? <p className="answer-kicker">{answerKicker(answer.kind)}</p> : null}
                  {(answer.blocks ?? []).map((block) => (
                    <BlockView
                      key={block.id}
                      block={block}
                      sectionId={block.id}
                      citations={answer.citations ?? {}}
                      frames={frames}
                      onOpenFrame={onOpenFrame}
                      onAskFrame={() => undefined}
                      onCite={onCite}
                      onBroken={onBroken}
                    />
                  ))}
                  {(answer.limitations ?? []).map((item) => (
                    <p className="history-note" key={item.message}>
                      {item.message}
                    </p>
                  ))}
                  {Object.values(answer.citations || {})
                    .filter((c) => c.kind === "external_reference")
                    .map((c) => (
                      <ReferenceCard key={c.id} citation={c} requestId={turn.turn.request_id} />
                    ))}
                </div>
              ) : null}
              {turn.turn.status === "canceled" ? (
                <div className="turn-answer">
                  <p className="turn-stopped">没有继续生成回答。</p>
                </div>
              ) : null}
              {turn.turn.status === "failed" ? (
                <div className="turn-answer">
                  <p className="turn-stopped">{turn.failure?.message || "没能完成回答。"}</p>
                </div>
              ) : null}
              {turn.actions.can_retry ? (
                <button type="button" className="disclosure" onClick={() => onRetry(turn)}>
                  重新回答
                </button>
              ) : null}
            </article>
          )
        })}
      </div>
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
          有新回答，回到底部 ↓
        </button>
      ) : null}
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
              <span>{clipText(passageChip.quote, 18)}</span>
              <button type="button" aria-label="移除选中段落" onClick={onRemovePassage}>
                ×
              </button>
            </span>
          ) : null}
        </div>
        {error ? <p className="form-error">{error}</p> : null}
        {modelUnavailable ? <p className="doc-note">本次解读的原模型暂不可用。文章和下载仍可使用，问题草稿已保留。</p> : null}
        <div className="external-option">
          <label>
            <input
              type="checkbox"
              checked={externalAllowed}
              disabled={!externalAvailable || busy || !!active || modelUnavailable}
              onChange={(e) => onExternal(e.target.checked)}
            />
            补充网络资料
          </label>
          <span>{externalAvailable ? (searchAvailable ? "已知网页优先 · 免费额度内搜索" : "可读取已知公开网页") : "站长尚未启用"}</span>
        </div>
        {externalAllowed ? (
          <label className="reference-input">
            提供网页链接（可选，每行一条，最多 3 条）
            <textarea rows={2} value={referenceLinks} onChange={(e) => onReferenceLinks(e.target.value)} placeholder="https://…" />
          </label>
        ) : null}
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
