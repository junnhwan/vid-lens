import { useEffect, useRef, useState } from "react"
import { HttpApi } from "../api/http"
import { isApiError } from "../api/error"
import { ResourceStream, type LiveSocket } from "../api/live"
import { canReplay, forgetWrite, newIdempotencyKey, pendingWrite, rememberWrite, type KeyValue } from "../api/writes"
import { advanceActivity, asSteps, mergeActivitySnapshot, mergeSteps } from "../contract/activity"
import { clockFrom, decideSnapshot, decideUpsert, isTerminalStatus, type Clock } from "../contract/stream"
import { codePointLength, normalizeQuote, passageContext, type PassageChoice } from "../contract/text"
import type { ActivityStep, Conversation, Frame, SessionResponse, TurnView, SuggestedQuestionsView, Article } from "../contract/types"
import { starterPrompts, starterSection, chapterPrompts } from "../app/labels"
import type { PendingQuestion } from "../app/pendingQuestion"
import { errorText, normalizeStep } from "../app/visitorContract"
import { conversationWithTurns, useConversationHistory } from "../app/useConversationHistory"
import { useSuggestedQuestions } from "../app/useSuggestedQuestions"

function browserStore(): KeyValue {
  return localStorage
}

export function useExampleQuestions(articleID: string | null, title: string | undefined, sectionTitle: string | undefined, sectionID?: string, suggestions?: SuggestedQuestionsView, sections?: Article["sections"]) {
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
  const sending = useRef(false)
  const canceling = useRef(new Set<string>())
  const restoreSend = useRef<(() => void) | null>(null)
  const [pendingQuestion, setPendingQuestion] = useState<PendingQuestion | null>(null)
  const conversationLoad = useRef<{ epoch: number; promise: Promise<Conversation | null> } | null>(null)
  const [error, setError] = useState("")
  const [expanded, setExpanded] = useState<Record<string, boolean | undefined>>({})
  const [focusToken, setFocusToken] = useState(0)
  const composers = useRef(new Map<string, ReturnType<typeof captureComposer>>())
  conversationRef.current = conversation
  turnsRef.current = turns
  stepsRef.current = turnSteps
  const history = useConversationHistory({
    scope: `example:${articleID ?? ""}`, api, store: browserStore, current: () => conversationRef.current,
    disabled: () => sending.current,
    windowSeconds: () => sessionRef.current?.capabilities.idempotency_window_seconds ?? 86400,
    activate: activateConversation,
  })
  function captureComposer() { return { draft, frameChip, passageChip, externalAllowed, referenceLinks, pendingQuestion, restore: restoreSend.current } }
  function restoreComposer(id: string) {
    const saved = composers.current.get(id)
    setDraft(saved?.draft ?? ""); setFrameChip(saved?.frameChip ?? null); setPassageChip(saved?.passageChip ?? null)
    setExternalAllowed(saved?.externalAllowed ?? false); setReferenceLinks(saved?.referenceLinks ?? "")
    setPendingQuestion(saved?.pendingQuestion ?? null); restoreSend.current = saved?.restore ?? null
  }
  async function activateConversation(next: Conversation, owns: () => boolean) {
    const current = conversationRef.current
    if (current) composers.current.set(current.id, captureComposer())
    const epoch = ++generation.current
    stopStream(); clockRef.current = null; conversationLoad.current = null
    conversationRef.current = next; setConversation(next); restoreComposer(next.id)
    setTurns([]); turnsRef.current = []; setExpanded({}); setError("")
    const page = await api().turns(next.id)
    if (!owns() || epoch !== generation.current) return
    if (page.items.some(item => item.turn.conversation_id !== next.id)) throw new Error("这条会话的问答记录不一致。")
    const loadedConversation = conversationWithTurns(next, page.items)
    conversationRef.current = loadedConversation; setConversation(loadedConversation)
    setTurns(page.items); turnsRef.current = page.items
    const loaded = Object.fromEntries(page.items.map(item => [item.turn.id, asSteps(item.activity.recent_steps).map(normalizeStep)]))
    stepsRef.current = loaded; setTurnSteps(loaded)
    absorb(page.items.flatMap(item => [...(item.turn.answer?.frames ?? item.answer_preview?.frames ?? []), ...asSteps(item.activity.recent_steps).flatMap(step => step.frames ?? [])]))
    const active = page.items.find(item => !isTerminalStatus(item.turn.status))
    if (active) startStream(active, epoch)
    setFocusToken(value => value + 1)
  }

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
    absorb([...(view.turn.answer?.frames ?? view.answer_preview?.frames ?? []), ...asSteps(view.activity.recent_steps).flatMap((step) => step.frames ?? [])])
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

  async function loadConversation(id: string, epoch: number, force: boolean) {
    await ensureSession()
    if (epoch !== generation.current) return null
    const current = conversationRef.current
    if (!force && current?.article_id === id) return current
    const preferred = current?.article_id === id ? current.id : history.preferredID()
    let response
    try { response = preferred ? await api().getConversation(preferred) : await api().exampleConversation(id) }
    catch (cause) {
      if (!preferred || !isApiError(cause) || cause.status !== 404) throw cause
      history.forgetSelection(); response = await api().exampleConversation(id)
    }
    if (epoch !== generation.current || response.conversation.article_id !== id) return null
    conversationRef.current = response.conversation
    setConversation(response.conversation)
    history.remember(response.conversation)
    const page = await api().turns(response.conversation.id)
    if (epoch !== generation.current || conversationRef.current?.id !== response.conversation.id) return null
    if (page.items.some(item => item.turn.conversation_id !== response.conversation.id)) throw new Error("这条会话的问答记录不一致。")
    const loadedConversation = conversationWithTurns(response.conversation, page.items)
    conversationRef.current = loadedConversation; setConversation(loadedConversation)
    setTurns(page.items)
    turnsRef.current = page.items
    const loaded: Record<string, ActivityStep[]> = {}
    for (const turn of page.items) {
      loaded[turn.turn.id] = mergeSteps([], asSteps(turn.activity.recent_steps).map(normalizeStep), true)
      absorb([...(turn.turn.answer?.frames ?? turn.answer_preview?.frames ?? []), ...asSteps(turn.activity.recent_steps).flatMap((step) => step.frames ?? [])])
    }
    stepsRef.current = loaded
    setTurnSteps(loaded)
    const active = [...page.items].reverse().find((turn) => !isTerminalStatus(turn.turn.status))
    if (active) startStream(active, epoch)
    void history.refresh(false, response.conversation.request_id)
    return loadedConversation
  }
  async function ensureConversation(id: string, epoch: number, force = false): Promise<Conversation | null> {
    if (conversationLoad.current?.epoch === epoch) return conversationLoad.current.promise
    const entry = { epoch, promise: loadConversation(id, epoch, force) }
    conversationLoad.current = entry
    try { return await entry.promise } finally { if (conversationLoad.current === entry) conversationLoad.current = null }
  }

  useEffect(() => {
    generation.current += 1
    history.reset(); composers.current.clear()
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
    sending.current = false; restoreSend.current = null; conversationLoad.current = null; setPendingQuestion(null)
  }, [articleID])

  useEffect(() => () => {
    generation.current += 1
    stopStream()
  }, [])

  const session = sessionRef.current
  const prepared=useSuggestedQuestions(articleID ?? undefined,suggestions,async()=> (await api().request<{suggested_questions?:SuggestedQuestionsView}>("GET",`/api/v1/examples/${articleID}`)).body.suggested_questions ?? {status:"unavailable",items:[]})
  const external = session?.capabilities.external_sources
  return {
    open,
    turns,
    currentConversation: conversation,
    conversationHistory: history.controls,
    frames,
    stepsFor: (turn: TurnView) => turnSteps[turn.turn.id] ?? [],
    draft,
    frameChip,
    passageChip,
    prompts: prepared?.items.length ? starterPrompts(undefined,undefined,prepared.items) : sections ? chapterPrompts(sections) : starterPrompts(title,sectionTitle),
    promptStatus:prepared?.status,
    promptSource:prepared?.items.length ? "generated" as const : "chapters" as const,
    busy: busy || history.controls.busy,
    pendingQuestion,
    restoreQuestion: () => { if (pendingQuestion?.phase === "failed") { restoreSend.current?.(); setPendingQuestion(null); setFocusToken(value => value + 1) } },
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
    stop: async () => {
      const current = conversationRef.current
      const active = turnsRef.current.find((turn) => turn.actions.can_cancel)
      if (!current || !active || canceling.current.has(active.turn.id)) return
      const epoch = generation.current
      canceling.current.add(active.turn.id)
      try {
        const view = await api().cancelTurn(current.id, active.turn.id)
        if (epoch === generation.current && conversationRef.current?.id === current.id) installTurn(view, true)
      } catch (cause: unknown) {
        if (epoch === generation.current && conversationRef.current?.id === current.id) setError(errorText(cause))
      } finally {
        canceling.current.delete(active.turn.id)
      }
    },
    retry: (turn: TurnView) => {
      const current = conversationRef.current
      if (!current || sending.current || history.isBusy() || !turn.actions.can_retry || current.last_turn_id !== turn.turn.id || turnsRef.current.some(item => !isTerminalStatus(item.turn.status))) return
      const epoch = generation.current
      const slot = `example-turn-retry:${current.id}:${turn.turn.id}`
      const body = JSON.stringify({ expected_last_turn_id: current.last_turn_id })
      const pending = pendingWrite(browserStore(), slot)
      const key = pending && pending.body === body && canReplay(pending, Date.now()) ? pending.key : newIdempotencyKey()
      sending.current = true
      setBusy(true)
      setError("")
      void Promise.resolve().then(() => {
        rememberWrite(browserStore(), { slot, key, path: `/api/v1/conversations/${current.id}/turns/${turn.turn.id}/retry`, body, startedAt: Date.now(), expiresAt: Date.now() + (sessionRef.current?.capabilities.idempotency_window_seconds ?? 86400) * 1000 })
        return api().retryTurn(current.id, turn.turn.id, key, current.last_turn_id)
      }).then((accepted) => {
        if (pendingWrite(browserStore(), slot)?.key === key) forgetWrite(browserStore(), slot)
        if (epoch !== generation.current) return
        clockRef.current = clockFrom(accepted.body.stream)
        installTurn(accepted.body, true)
        startStream(accepted.body, epoch)
      }).catch(async (cause: unknown) => {
        if (epoch !== generation.current) return
        if (isApiError(cause) && (cause.code === "TURN_IN_PROGRESS" || cause.code === "TURN_HISTORY_CHANGED")) {
          forgetWrite(browserStore(), slot)
          try { await ensureConversation(current.article_id, epoch, true) }
          catch (again) { if (epoch === generation.current) setError(errorText(again)) }
          return
        }
        if (isApiError(cause) && cause.status < 500 && cause.status !== 429) forgetWrite(browserStore(), slot)
        setError(errorText(cause))
      }).finally(() => {
        if (epoch === generation.current) { sending.current = false; setBusy(false) }
      })
    },
    send: () => {
      if (sending.current || history.isBusy()) return
      const text = draft.trim()
      if (!text || !articleID) {
        setError("先写下一个问题。")
        return
      }
      const epoch = generation.current
      if (turnsRef.current.some(turn => !isTerminalStatus(turn.turn.status))) { setError("上一轮回答仍在进行。"); return }
      sending.current = true
      setBusy(true)
      setError("")
      const sentFrame = frameChip
      const sentPassage = passageChip
      const sentContext = { frame_id: sentFrame?.id ?? null, ...passageContext(sentPassage), ...starterSection(text, sectionID && sectionTitle ? { id: sectionID, title: sectionTitle } : undefined,prepared?.items,sections) }
      restoreSend.current = () => { setDraft(text); setFrameChip(sentFrame); setPassageChip(sentPassage); setExternalAllowed(externalAllowed); setReferenceLinks(referenceLinks) }
      setDraft("")
      setPendingQuestion({ question: text, context: sentContext, phase: "sending" })
      void (async () => {
        try {
          const sessionValue = await ensureSession()
          if (epoch !== generation.current) return
          const max = sessionValue.capabilities.question_chars_max
          if (sentPassage && codePointLength(normalizeQuote(sentPassage.quote)) > sessionValue.capabilities.selection_chars_max) {
            setError("选中文字合计超过了可选长度。")
            setPendingQuestion(null); setDraft(value => value || text)
            return
          }
          if (max != null && codePointLength(text) > max) {
            setError("这个问题太长了。")
            setPendingQuestion(null); setDraft(value => value || text)
            return
          }
          if (turnsRef.current.some((turn) => !isTerminalStatus(turn.turn.status))) {
            setError("上一轮回答仍在进行。")
            setPendingQuestion(null); setDraft(value => value || text)
            return
          }
          const current = await ensureConversation(articleID, epoch)
          if (!current || epoch !== generation.current) return
          const payload = {
            question: text,
            ...(externalAllowed ? { allow_external_sources: true, reference_urls: referenceLinks.split(/\n/).map((item) => item.trim()).filter(Boolean) } : {}),
            expected_last_turn_id: current.last_turn_id,
            context: sentContext,
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
          setPendingQuestion(null)
          setExternalAllowed(false)
          setReferenceLinks("")
          setFrameChip((value) => (value?.id === sentFrame?.id ? null : value))
          setPassageChip((value) => (value === sentPassage ? null : value))
          clockRef.current = clockFrom(accepted.body.stream)
          installTurn(accepted.body, true)
          startStream(accepted.body, epoch)
        } catch (cause) {
          if (epoch !== generation.current) return
          setPendingQuestion({ question: text, context: sentContext, phase: "failed" })
          setDraft(value => value || text)
          if (isApiError(cause) && (cause.code === "TURN_IN_PROGRESS" || cause.code === "TURN_HISTORY_CHANGED")) {
            forgetWrite(browserStore(), `example-turn:${conversationRef.current?.id || "unknown"}`)
            try { await ensureConversation(articleID, epoch, true) } catch { /* Keep the original submission error and recoverable question. */ }
          }
          if (epoch === generation.current) setError(errorText(cause))
        } finally {
          if (epoch === generation.current) { sending.current = false; setBusy(false) }
        }
      })()
    },
  }
}
