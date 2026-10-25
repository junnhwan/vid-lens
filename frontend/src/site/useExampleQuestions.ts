import { useEffect, useRef, useState } from "react"
import { HttpApi } from "../api/http"
import { isApiError } from "../api/error"
import { ResourceStream, type LiveSocket } from "../api/live"
import { canReplay, forgetWrite, newIdempotencyKey, pendingWrite, rememberWrite, type KeyValue } from "../api/writes"
import { advanceActivity, asSteps, mergeActivitySnapshot, mergeSteps } from "../contract/activity"
import { clockFrom, decideSnapshot, decideUpsert, isTerminalStatus, type Clock } from "../contract/stream"
import { codePointLength, normalizeQuote, passageContext, type PassageChoice } from "../contract/text"
import type { ActivityStep, Conversation, Frame, SessionResponse, TurnView } from "../contract/types"
import { starterPrompts } from "../app/labels"
import { errorText, normalizeStep } from "../app/visitorContract"

function browserStore(): KeyValue {
  return localStorage
}

export function useExampleQuestions(articleID: string | null, title: string | undefined, sectionTitle: string | undefined) {
  const apiRef = useRef<HttpApi | null>(null)
  const sessionRef = useRef<SessionResponse | null>(null)
  const streamRef = useRef<ResourceStream | null>(null)
  const clockRef = useRef<Clock | null>(null)
  const conversationRef = useRef<Conversation | null>(null)
  const turnsRef = useRef<TurnView[]>([])
  const stepsRef = useRef<Record<string, ActivityStep[]>>({})
  const generation = useRef(0)
  const [conversation, setConversation] = useState<Conversation | null>(null)
  const [turns, setTurns] = useState<TurnView[]>([])
  const [turnSteps, setTurnSteps] = useState<Record<string, ActivityStep[]>>({})
  const [frames, setFrames] = useState<Frame[]>([])
  const [open, setOpen] = useState(false)
  const [draft, setDraft] = useState("")
  const [frameChip, setFrameChip] = useState<{ id: string; label: string } | null>(null)
  const [passageChip, setPassageChip] = useState<PassageChoice | null>(null)
  const [externalAllowed, setExternalAllowed] = useState(false)
  const [referenceLinks, setReferenceLinks] = useState("")
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState("")
  const [expanded, setExpanded] = useState<Record<string, boolean | undefined>>({})
  const [focusToken, setFocusToken] = useState(0)
  conversationRef.current = conversation
  turnsRef.current = turns
  stepsRef.current = turnSteps

  function api() {
    if (!apiRef.current) apiRef.current = new HttpApi()
    return apiRef.current
  }

  function absorb(next: Frame[]) {
    if (!next.length) return
    setFrames((current) => {
      const map = new Map(current.map((frame) => [frame.id, frame]))
      for (const frame of next) map.set(frame.id, frame)
      return [...map.values()]
    })
  }

  function stopStream() {
    streamRef.current?.stop()
    streamRef.current = null
  }

  function installTurn(view: TurnView, replace: boolean) {
    setTurns((prev) => {
      const index = prev.findIndex((item) => item.turn.id === view.turn.id)
      const next = index < 0 ? [...prev, view] : prev.map((item, itemIndex) => (itemIndex === index ? view : item))
      next.sort((a, b) => a.turn.turn_no - b.turn.turn_no)
      turnsRef.current = next
      return next
    })
    setTurnSteps((prev) => {
      const updated = {
        ...prev,
        [view.turn.id]: mergeActivitySnapshot(prev[view.turn.id] ?? [], asSteps(view.activity.recent_steps).map(normalizeStep), {
          replace,
          sameResource: !!prev[view.turn.id],
          expired: view.activity.history_status === "expired",
        }),
      }
      stepsRef.current = updated
      return updated
    })
    absorb([...(view.turn.answer?.frames ?? []), ...asSteps(view.activity.recent_steps).flatMap((step) => step.frames ?? [])])
    setConversation((prev) => {
      if (!prev || prev.id !== view.turn.conversation_id) return prev
      const terminal = isTerminalStatus(view.turn.status)
      const next = { ...prev, last_turn_id: view.turn.id, active_turn_id: terminal ? null : view.turn.id }
      conversationRef.current = next
      return next
    })
  }

  function startStream(view: TurnView, epoch: number) {
    if (isTerminalStatus(view.turn.status)) return
    stopStream()
    clockRef.current = clockFrom(view.stream)
    const conversationId = view.turn.conversation_id
    const id = view.turn.id
    const stream = new ResourceStream({
      events: (url) => new EventSource(url, { withCredentials: true }) as unknown as LiveSocket,
      resourcePath: (() => {
        const address = new URL(view.stream.url || `/api/v1/conversations/${encodeURIComponent(conversationId)}/turns/${encodeURIComponent(id)}/events`, location.origin)
        address.searchParams.set("contract", "2")
        return address.pathname + address.search
      })(),
      scope: { type: "turn", id },
      getSnapshot: () => api().getTurn(conversationId, id),
      onSnapshot: (payload) => {
        if (epoch !== generation.current || !payload || typeof payload !== "object") return "close"
        const value = payload as TurnView
        if (value.turn?.id !== id) return "resync"
        const next = clockFrom(value.stream)
        if (!next) return "resync"
        const decision = decideSnapshot(clockRef.current, next, isTerminalStatus(value.turn.status), "http")
        if (decision === "ignore" || decision === "confirm") return isTerminalStatus(value.turn.status) ? "close" : "stay"
        if (decision === "resync") return "resync"
        const previous = clockRef.current
        clockRef.current = next
        installTurn(value, !previous || next.revision !== previous.revision || next.runId !== previous.runId)
        return isTerminalStatus(value.turn.status) ? "close" : "stay"
      },
      onUpsert: (step, currentId, position) => {
        if (epoch !== generation.current) return "close"
        const next = clockFrom(position)
        if (!next) return "resync"
        const decision = decideUpsert(clockRef.current, next, step.last_seq)
        if (decision === "ignore") return "stay"
        if (decision === "resync") return "resync"
        const normalized = normalizeStep(step)
        clockRef.current = next
        const firstSeen = !(stepsRef.current[id] ?? []).some((item) => item.id === normalized.id)
        setTurnSteps((prev) => {
          const updated = { ...prev, [id]: mergeSteps(prev[id] ?? [], [normalized], false) }
          stepsRef.current = updated
          return updated
        })
        setTurns((current) => current.map((item) => item.turn.id === id ? { ...item, activity: advanceActivity(item.activity, normalized, currentId, firstSeen), stream: { ...item.stream, ...position } } : item))
        absorb(normalized.frames ?? [])
        return "stay"
      },
      onAuth: (message) => { if (epoch === generation.current) setError(message) },
      onStatus: () => undefined,
    })
    streamRef.current = stream
    stream.start(view.stream.cursor)
  }

  async function ensureSession() {
    if (sessionRef.current) return sessionRef.current
    const value = await api().session()
    sessionRef.current = value
    return value
  }

  async function ensureConversation(id: string, epoch: number) {
    await ensureSession()
    if (epoch !== generation.current) return null
    const current = conversationRef.current
    if (current?.article_id === id) return current
    const response = await api().exampleConversation(id)
    if (epoch !== generation.current || response.conversation.article_id !== id) return null
    conversationRef.current = response.conversation
    setConversation(response.conversation)
    const page = await api().turns(response.conversation.id)
    if (epoch !== generation.current || conversationRef.current?.id !== response.conversation.id) return null
    setTurns(page.items)
    turnsRef.current = page.items
    const loaded: Record<string, ActivityStep[]> = {}
    for (const turn of page.items) {
      loaded[turn.turn.id] = mergeSteps([], asSteps(turn.activity.recent_steps).map(normalizeStep), true)
      absorb([...(turn.turn.answer?.frames ?? []), ...asSteps(turn.activity.recent_steps).flatMap((step) => step.frames ?? [])])
    }
    stepsRef.current = loaded
    setTurnSteps(loaded)
    const active = [...page.items].reverse().find((turn) => !isTerminalStatus(turn.turn.status))
    if (active) startStream(active, epoch)
    return response.conversation
  }

  useEffect(() => {
    generation.current += 1
    stopStream()
    conversationRef.current = null
    turnsRef.current = []
    stepsRef.current = {}
    clockRef.current = null
    setConversation(null)
    setTurns([])
    setTurnSteps({})
    setFrames([])
    setOpen(false)
    setDraft("")
    setFrameChip(null)
    setPassageChip(null)
    setExternalAllowed(false)
    setReferenceLinks("")
    setError("")
    setExpanded({})
    setBusy(false)
  }, [articleID])

  useEffect(() => () => {
    generation.current += 1
    stopStream()
  }, [])

  const session = sessionRef.current
  const external = session?.capabilities.external_sources
  return {
    open,
    turns,
    frames,
    stepsFor: (turn: TurnView) => turnSteps[turn.turn.id] ?? [],
    draft,
    frameChip,
    passageChip,
    prompts: starterPrompts(title, sectionTitle),
    busy,
    error,
    expanded,
    focusToken,
    externalAllowed,
    externalAvailable: !!external?.enabled,
    searchAvailable: !!external?.search_enabled,
    referenceLinks,
    selectionMax: session?.capabilities.selection_chars_max ?? null,
    setDraft,
    setExternal: setExternalAllowed,
    setReferenceLinks,
    askFrame: (frame: Frame) => {
      const label = frame.caption || "这张画面"
      setFrameChip({ id: frame.id, label })
      setOpen(true)
      setFocusToken((value) => value + 1)
      const epoch = generation.current
      if (articleID) void ensureConversation(articleID, epoch).catch((cause: unknown) => setError(errorText(cause)))
    },
    askPassage: (passage: PassageChoice) => {
      setPassageChip(passage)
      setOpen(true)
      setFocusToken((value) => value + 1)
      const epoch = generation.current
      if (articleID) void ensureConversation(articleID, epoch).catch((cause: unknown) => setError(errorText(cause)))
    },
    toggle: () => {
      if (open) {
        setOpen(false)
        return
      }
      setOpen(true)
      setFocusToken((value) => value + 1)
      setError("")
      const epoch = generation.current
      if (!articleID) return
      void ensureConversation(articleID, epoch).catch((cause: unknown) => setError(errorText(cause)))
    },
    close: () => setOpen(false),
    suggest: (prompt: string) => {
      setDraft(prompt)
      setFocusToken((value) => value + 1)
    },
    removeFrame: () => setFrameChip(null),
    removePassage: () => setPassageChip(null),
    toggleTurn: (id: string, shown: boolean) => {
      setExpanded((prev) => ({ ...prev, [id]: shown }))
      if (!shown) return
      const turn = turnsRef.current.find((item) => item.turn.id === id)
      if (!turn?.activity.history_url || turn.activity.history_status === "expired" || turn.activity.history_status === "none") return
      void api().activity(turn.activity.history_url).then((page) => {
        setTurnSteps((prev) => ({ ...prev, [id]: mergeSteps(prev[id] ?? [], page.items.map(normalizeStep), true) }))
      }).catch((cause: unknown) => setError(errorText(cause)))
    },
    stop: () => {
      const current = conversationRef.current
      const active = turnsRef.current.find((turn) => turn.actions.can_cancel)
      if (!current || !active) return
      void api().cancelTurn(current.id, active.turn.id).then((view) => installTurn(view, true)).catch((cause: unknown) => setError(errorText(cause)))
    },
    retry: (turn: TurnView) => {
      const current = conversationRef.current
      if (!current) return
      const epoch = generation.current
      void api().retryTurn(current.id, turn.turn.id, newIdempotencyKey(), current.last_turn_id).then((accepted) => {
        if (epoch !== generation.current) return
        clockRef.current = clockFrom(accepted.body.stream)
        installTurn(accepted.body, true)
        startStream(accepted.body, epoch)
      }).catch((cause: unknown) => setError(errorText(cause)))
    },
    send: () => {
      const text = draft.trim()
      if (!text || !articleID) {
        setError("先写下一个问题。")
        return
      }
      const epoch = generation.current
      setBusy(true)
      setError("")
      const sentFrame = frameChip
      const sentPassage = passageChip
      void (async () => {
        try {
          const sessionValue = await ensureSession()
          const max = sessionValue.capabilities.question_chars_max
          if (sentPassage && codePointLength(normalizeQuote(sentPassage.quote)) > sessionValue.capabilities.selection_chars_max) {
            setError("选中文字合计超过了可选长度。")
            return
          }
          if (max != null && codePointLength(text) > max) {
            setError("这个问题太长了。")
            return
          }
          if (turnsRef.current.some((turn) => !isTerminalStatus(turn.turn.status))) {
            setError("上一轮回答仍在进行。")
            return
          }
          const current = await ensureConversation(articleID, epoch)
          if (!current || epoch !== generation.current) return
          const payload = {
            question: text,
            ...(externalAllowed ? { allow_external_sources: true, reference_urls: referenceLinks.split(/\n/).map((item) => item.trim()).filter(Boolean) } : {}),
            expected_last_turn_id: current.last_turn_id,
            context: {
              frame_id: sentFrame?.id ?? null,
              ...passageContext(sentPassage),
            },
          }
          const body = JSON.stringify(payload)
          const slot = `example-turn:${current.id}`
          const pending = pendingWrite(browserStore(), slot)
          const windowMs = (sessionValue.capabilities.idempotency_window_seconds ?? 86400) * 1000
          const key = pending && pending.body === body && canReplay(pending, Date.now()) ? pending.key : newIdempotencyKey()
          rememberWrite(browserStore(), { slot, key, path: `/api/v1/conversations/${current.id}/turns`, body, expiresAt: Date.now() + windowMs, startedAt: Date.now() })
          const accepted = await api().createTurn(current.id, key, payload)
          if (pendingWrite(browserStore(), slot)?.key === key) forgetWrite(browserStore(), slot)
          if (epoch !== generation.current) return
          setDraft((value) => (value.trim() === text ? "" : value))
          setExternalAllowed(false)
          setReferenceLinks("")
          setFrameChip((value) => (value?.id === sentFrame?.id ? null : value))
          setPassageChip((value) => (value === sentPassage ? null : value))
          clockRef.current = clockFrom(accepted.body.stream)
          installTurn(accepted.body, true)
          startStream(accepted.body, epoch)
        } catch (cause) {
          if (epoch !== generation.current) return
          if (isApiError(cause) && (cause.code === "TURN_IN_PROGRESS" || cause.code === "TURN_HISTORY_CHANGED")) {
            forgetWrite(browserStore(), `example-turn:${conversationRef.current?.id || "unknown"}`)
          }
          setError(errorText(cause))
        } finally {
          if (epoch === generation.current) setBusy(false)
        }
      })()
    },
  }
}
