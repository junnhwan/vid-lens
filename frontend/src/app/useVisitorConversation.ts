import { useEffect, useMemo, useRef, useState } from "react"
import { ApiError, isApiError } from "../api/error"
import { ResourceStream, type Reaction } from "../api/live"
import {
  canReplay,
  forgetWrite,
  newIdempotencyKey,
  pendingWrite,
  rememberWrite
} from "../api/writes"
import { advanceActivity, asSteps, mergeActivitySnapshot, mergeSteps } from "../contract/activity"
import { clockFrom, decideSnapshot, decideUpsert, isTerminalStatus, type Clock } from "../contract/stream"
import { codePointLength, formatClock, normalizeQuote, passageContext, type PassageChoice } from "../contract/text"
import type {
  ActivityStep,
  Conversation,
  Frame,
  StreamPosition,
  TurnView
} from "../contract/types"
import { starterPrompts } from "./labels"
import type { VisitorContext } from "./visitorContext"
import { ObsoleteRequest, isTurnView, normalizeStep } from "./visitorContract"
export function useVisitorConversation(context: VisitorContext, frameMap: Map<string, Frame>, loadHistory: (turnId?: string) => Promise<void>, absorb: (frames: Frame[]) => void) {
  const workspace = context.workspace
  const { lost: markLost, announce: setLive, notice: setNotice, report } = context.messages
  const store = context.store
  function ownsWorkspace(gen: number, id: string | undefined) { return workspace.owns(gen, id) }
  function ownsRequest(gen: number, id: string) { return workspace.ownsRequest(gen, id, context.request()?.request.id) }

  const article = context.article(), session = context.session()
  const [conversation, setConversation] = useState<Conversation | null>(null)
  const [turns, setTurns] = useState<TurnView[]>([])
  const [turnSteps, setTurnSteps] = useState<Record<string, ActivityStep[]>>({})
  const turnStepsRef = useRef<Record<string, ActivityStep[]>>({})
  turnStepsRef.current = turnSteps
  const [qaOpen, setQaOpen] = useState(false)
  const [draft, setDraft] = useState("")
  const [externalAllowed, setExternalAllowed] = useState(false)
  const [referenceLinks, setReferenceLinks] = useState("")
  const [frameChip, setFrameChip] = useState<{
    id: string
    label: string
  } | null>(null)
  const [passageChip, setPassageChip] = useState<PassageChoice | null>(null)
  const [qaError, setQaError] = useState("")
  const [qaBusy, setQaBusy] = useState(false)
  const [expanded, setExpanded] = useState<Record<string, boolean | undefined>>({})
  const [focusToken, setFocusToken] = useState(0)
  const conversationRef = useRef<Conversation | null>(null)
  const turnsRef = useRef<TurnView[]>([])
  const turnClock = useRef<Clock | null>(null)
  conversationRef.current = conversation
  turnsRef.current = turns
  function rememberConversation(next: Conversation) {
    conversationRef.current = next
    setConversation(next)
  }
  function installTurn(view: TurnView, replace: boolean) {
    setTurns((prev) => {
      const index = prev.findIndex((item) => item.turn.id === view.turn.id)
      const next = index < 0 ? [...prev, view] : prev.map((item, itemIndex) => (itemIndex === index ? view : item))
      next.sort((a, b) => a.turn.turn_no - b.turn.turn_no)
      turnsRef.current = next
      return next
    })
    setTurnSteps((prev) => ({
      ...prev,
      [view.turn.id]: mergeActivitySnapshot(prev[view.turn.id] ?? [], asSteps(view.activity.recent_steps).map(normalizeStep), { replace, sameResource: !!prev[view.turn.id], expired: view.activity.history_status === "expired" }),
    }))
    absorb([...(view.turn.answer?.frames ?? []), ...asSteps(view.activity.recent_steps).flatMap((step) => step.frames ?? [])])
    const terminal = isTerminalStatus(view.turn.status)
    setConversation((prev) => {
      if (!prev) return prev
      const next = {
        ...prev,
        active_turn_id: terminal ? (prev.active_turn_id === view.turn.id ? null : prev.active_turn_id) : view.turn.id,
        last_turn_id: view.turn.id,
      }
      conversationRef.current = next
      return next
    })
  }
  function onTurnSnapshot(value: unknown, source: "sse" | "http"): Reaction {
    if (!isTurnView(value)) return "resync"
    if (value.turn.conversation_id !== conversationRef.current?.id) return "close"
    if (value.schema_version !== 1) {
      setNotice("回答数据的协议版本和当前页面不一致。")
      return "close"
    }
    const next = clockFrom(value.stream)
    if (!next) return "resync"
    const decision = decideSnapshot(turnClock.current, next, isTerminalStatus(value.turn.status), source)
    if (decision === "ignore") return "stay"
    if (decision === "confirm") return isTerminalStatus(value.turn.status) ? "close" : "stay"
    if (decision === "resync") return "resync"
    const previous = turnClock.current
    const replace = !previous || next.revision !== previous.revision || next.runId !== previous.runId
    turnClock.current = next
    installTurn(value, replace)
    return isTerminalStatus(value.turn.status) ? "close" : "stay"
  }
  function onTurnUpsert(step: ActivityStep, currentId: string | null, position: StreamPosition): Reaction {
    const id = workspace.turn.id
    if (!id) return "resync"
    const next = clockFrom(position)
    if (!next) return "resync"
    const decision = decideUpsert(turnClock.current, next, step.last_seq)
    if (decision === "ignore") return "stay"
    if (decision === "resync") return "resync"
    const normalized = normalizeStep(step)
    turnClock.current = next
    const firstSeen = !(turnStepsRef.current[id] ?? []).some((item) => item.id === normalized.id)
    setTurnSteps((prev) => {
      const updated = { ...prev, [id]: mergeSteps(prev[id] ?? [], [normalized], false) }
      turnStepsRef.current = updated
      return updated
    })
    setTurns((turns) => turns.map((item) => item.turn.id === id ? { ...item, activity: advanceActivity(item.activity, normalized, currentId, firstSeen), stream: { ...item.stream, ...position } } : item))

    absorb(normalized.frames)
    return "stay"
  }
  function startTurnStream(view: TurnView) {
    if (isTerminalStatus(view.turn.status)) return
    if (workspace.turn.id === view.turn.id) return
    workspace.turn.stop()
    const gen = workspace.epoch
    const ownerId = context.request()?.request.id
    const id = view.turn.id
    const conversationId = view.turn.conversation_id

    turnClock.current = clockFrom(view.stream)
    const stream = new ResourceStream({
      events: (url) => context.runtime()!.events(url),
      resourcePath: (() => {
        const u = new URL(
          view.stream.url || `/api/v1/conversations/${encodeURIComponent(conversationId)}/turns/${encodeURIComponent(id)}/events`,
          location.origin,
        )
        u.searchParams.set("contract", "2")
        return u.pathname + u.search
      })(),
      scope: { type: "turn", id },
      getSnapshot: () => context.runtime()!.api.getTurn(conversationId, id),
      onSnapshot: (payload, source) => (ownsWorkspace(gen, ownerId) ? onTurnSnapshot(payload, source) : "close"),
      onUpsert: (step, currentId, position) => (ownsWorkspace(gen, ownerId) ? onTurnUpsert(step, currentId, position) : "close"),
      onAuth: (message) => { if (ownsWorkspace(gen, ownerId)) markLost(message) },
      onStatus: (status) => {
        if (ownsWorkspace(gen, ownerId) && status === "reconnecting") setLive("正在重新连接回答")
      },
    })
    workspace.turn.attach(id, stream, view.stream.cursor)
  }
  async function ensureConversation(force: boolean) {
    const current = context.request()
    const rt = context.runtime()
    if (!current || !rt)
      throw new ApiError({
        status: 409,
        code: "ARTICLE_NOT_READY",
        detail: "文章还没有发布。",
      })
    if (!force && conversationRef.current?.request_id === current.request.id) return conversationRef.current
    const gen = workspace.epoch
    const id = current.request.id
    try {
      const response = await rt.api.conversation(id)
      if (!ownsRequest(gen, id) || response.conversation.request_id !== id) throw new ObsoleteRequest()
      rememberConversation(response.conversation)
      const page = await rt.api.turns(response.conversation.id)
      if (!ownsRequest(gen, id) || conversationRef.current?.id !== response.conversation.id) throw new ObsoleteRequest()
      if (page.items.some((item) => item.turn.conversation_id !== response.conversation.id)) throw new ObsoleteRequest()
      setTurns(page.items)
      turnsRef.current = page.items
      setTurnSteps((prev) => {
        const next = { ...prev }
        for (const turn of page.items)
          next[turn.turn.id] = mergeSteps(
            force ? [] : (next[turn.turn.id] ?? []),
            asSteps(turn.activity.recent_steps).map(normalizeStep),
            force || !next[turn.turn.id],
          )
        return next
      })
      for (const turn of page.items) absorb([...(turn.turn.answer?.frames ?? [])])
      return response.conversation
    } catch (error) {
      if (!ownsRequest(gen, id)) throw new ObsoleteRequest()
      throw error
    }
  }
  useEffect(() => {
    if (!qaOpen) {
      if (workspace.turn.id) {
        workspace.turn.stop()

      }
      return
    }
    const activeId = conversation?.active_turn_id
    if (!activeId) {
      if (workspace.turn.id) {
        workspace.turn.stop()

      }
      return
    }
    const turn = turns.find((item) => item.turn.id === activeId)
    if (!turn || isTerminalStatus(turn.turn.status)) return
    startTurnStream(turn)
  }, [qaOpen, conversation?.active_turn_id, turns])
  const resolvedTurns = useMemo(
    () =>
      Object.fromEntries(
        Object.entries(turnSteps).map(([id, list]) => [
          id,
          list.map((step) => {
            const frames = (step.frames ?? []).map((frame) => frameMap.get(frame.id) ?? frame)
            return (step.frames ?? []).every((frame, index) => frame === frames[index]) ? step : { ...step, frames }
          }),
        ]),
      ),
    [turnSteps, frameMap],
  )
  const prompts = starterPrompts(article?.article.title, article?.article.sections[0]?.title)
  const suggesting = prompts[0]
  function reset() {
    workspace.turn.stop(); turnClock.current = null
    conversationRef.current = null; turnsRef.current = []; turnStepsRef.current = {}
    setConversation(null); setTurns([]); setTurnSteps({}); setQaOpen(false); setQaError(""); setQaBusy(false)
    setDraft(""); setExternalAllowed(false); setReferenceLinks(""); setFrameChip(null); setPassageChip(null); setExpanded({})
  }
  return {
    reset, hide: () => setQaOpen(false), turn: (id: string) => turnsRef.current.find(t => t.turn.id === id),
    mergeHistory: (id: string, steps: ActivityStep[]) => setTurnSteps(prev => ({ ...prev, [id]: mergeSteps(prev[id] ?? [], steps, false) })),
    view: {
      turns,
      stepsFor: (turn: TurnView) => resolvedTurns[turn.turn.id] ?? asSteps(turn.activity.recent_steps).map(normalizeStep),
      qaOpen,
      draft,
      externalAllowed,
      setExternalAllowed,
      referenceLinks,
      setReferenceLinks,
      externalAvailable: session?.capabilities.external_sources?.enabled ?? false,
      searchAvailable: session?.capabilities.external_sources?.search_enabled ?? false,
      setDraft,
      frameChip,
      passageChip,
      suggesting,
      prompts,
      qaBusy,
      qaError,
      expanded,
      focusToken,
      askFrame: (frame: Frame) => {
        if (context.phase() !== "reading" && context.phase() !== "holding") return
        setFrameChip({
          id: frame.id,
          label: `${formatClock(frame.time_ms)} · ${frame.caption || "画面"}`,
        })
        setDraft(`这张 ${formatClock(frame.time_ms)} 的画面在说明什么？`)
        if (context.phase() === "holding") context.read()
        setQaOpen(true)
        setFocusToken((value) => value + 1)
        void ensureConversation(true).catch((error: unknown) => void report(error, setQaError))
      },
      attachPassage: (passage: PassageChoice) => {
        setPassageChip(passage)
        setQaOpen(true)
        setFocusToken((value) => value + 1)
        void ensureConversation(true).catch((error: unknown) => void report(error, setQaError))
      },
      toggleQa: () => {
        if (qaOpen) {
          const panel = document.getElementById("qa")
          const toggle = document.getElementById("toggle-qa")
          if (panel && toggle && panel.contains(document.activeElement)) toggle.focus({ preventScroll: true })
          setQaOpen(false)
          return
        }
        setQaOpen(true)
        setFocusToken((value) => value + 1)
        void ensureConversation(true).catch((error: unknown) => void report(error, setQaError))
      },
      closeQa: () => {
        const panel = document.getElementById("qa")
        const toggle = document.getElementById("toggle-qa")
        if (panel && toggle && panel.contains(document.activeElement)) toggle.focus({ preventScroll: true })
        setQaOpen(false)
      },
      send: () =>
        void (async () => {
          const text = draft.trim()
          if (!text) {
            setQaError("先写下一个问题。")
            return
          }
          const max = context.session()?.capabilities.question_chars_max
          if (max != null && codePointLength(text) > max) {
            setQaError("这个问题太长了。")
            return
          }
          if (turnsRef.current.some((turn) => !isTerminalStatus(turn.turn.status))) {
            setQaError("上一轮回答仍在进行。")
            return
          }
          const rt = context.runtime()
          const id = context.request()?.request.id
          if (!rt || !id) return
          const gen = workspace.epoch
          setQaBusy(true)
          setQaError("")
          const sentFrame = frameChip
          const sentPassage = passageChip
          const selectionMax = context.session()?.capabilities.selection_chars_max
          if (sentPassage && selectionMax != null && codePointLength(normalizeQuote(sentPassage.quote)) > selectionMax) {
            setQaBusy(false)
            setQaError("选中文字合计超过了可选长度。")
            return
          }
          try {
            const current = await ensureConversation(true)
            if (!ownsRequest(gen, id)) return
            const payload = {
              question: text,
              ...(externalAllowed
                ? {
                  allow_external_sources: true,
                  reference_urls: referenceLinks
                    .split(/\n/)
                    .map((v) => v.trim())
                    .filter(Boolean),
                }
                : {}),
              expected_last_turn_id: current.last_turn_id,
              context: {
                frame_id: sentFrame?.id ?? null,
                ...passageContext(sentPassage),
              },
            }
            const body = JSON.stringify(payload)
            const slot = `turn:${current.id}`
            const pending = pendingWrite(store(), slot)
            const windowMs = (context.session()?.capabilities.idempotency_window_seconds ?? 86400) * 1000
            const key = pending && pending.body === body && canReplay(pending, Date.now()) ? pending.key : newIdempotencyKey()
            rememberWrite(store(), {
              slot,
              key,
              path: `/api/v1/conversations/${current.id}/turns`,
              body,
              expiresAt: Date.now() + windowMs,
              startedAt: Date.now(),
            })
            const accepted = await rt.api.createTurn(current.id, key, payload)
            if (pendingWrite(store(), slot)?.key === key) forgetWrite(store(), slot)
            if (!ownsRequest(gen, id)) return
            setDraft((value) => (value.trim() === text ? "" : value))
            setExternalAllowed(false)
            setReferenceLinks("")
            setFrameChip((value) => (value?.id === sentFrame?.id ? null : value))
            setPassageChip((value) => (value === sentPassage ? null : value))
            turnClock.current = clockFrom(accepted.body.stream)
            installTurn(accepted.body, true)
          } catch (error) {
            if (!ownsRequest(gen, id)) return
            if (isApiError(error) && (error.code === "TURN_IN_PROGRESS" || error.code === "TURN_HISTORY_CHANGED")) {
              forgetWrite(store(), `turn:${conversationRef.current?.id || "unknown"}`)
              setQaError(error.detail)
              try {
                await ensureConversation(true)
              } catch (again) {
                await report(again, setQaError)
              }
              return
            }
            await report(error, setQaError, conversationRef.current ? `turn:${conversationRef.current.id}` : undefined)
          } finally {
            if (ownsRequest(gen, id)) setQaBusy(false)
          }
        })(),
      stopTurn: () =>
        void (async () => {
          const rt = context.runtime()
          const current = conversationRef.current
          const active = turnsRef.current.find((turn) => turn.actions.can_cancel)
          if (!rt || !current || !active) return
          const gen = workspace.epoch
          const id = current.request_id
          try {
            const view = await rt.api.cancelTurn(current.id, active.turn.id)
            if (!ownsRequest(gen, id) || conversationRef.current?.id !== current.id) return
            const reaction = onTurnSnapshot(view, "http")
            if (reaction === "close") {
              workspace.turn.stop()

            }
          } catch (error) {
            if (!ownsRequest(gen, id)) return
            await report(error, setQaError)
          }
        })(),
      retryTurn: (turn: TurnView) =>
        void (async () => {
          const rt = context.runtime()
          const current = conversationRef.current
          if (!rt || !current) return
          const gen = workspace.epoch
          const id = current.request_id
          try {
            const accepted = await rt.api.retryTurn(current.id, turn.turn.id, newIdempotencyKey(), current.last_turn_id)
            if (!ownsRequest(gen, id) || conversationRef.current?.id !== current.id) return
            turnClock.current = clockFrom(accepted.body.stream)
            installTurn(accepted.body, true)
          } catch (error) {
            if (!ownsRequest(gen, id)) return
            await report(error, setQaError)
          }
        })(),
      suggest: (prompt?: string) => {
        const title = context.article()?.article.title
        const section = context.article()?.article.sections[0]?.title
        setDraft(prompt?.trim() || starterPrompts(title, section)[0])
        setFocusToken((value) => value + 1)
      },
      removeFrame: () => setFrameChip(null),
      removePassage: () => setPassageChip(null),
      toggleTurn: (id: string, open: boolean) => { setExpanded((prev) => ({ ...prev, [id]: open })); if (open) void loadHistory(id) }
    }
  }
}
