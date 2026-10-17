import { useEffect, useMemo, useRef, useState } from "react"
import {
  clearWrites,
  canReplay,
  forgetWrite,
  memoryStore,
  newIdempotencyKey,
  pendingWrite,
  rememberWrite,
  restoreGenerationInput,
  type KeyValue,
} from "../api/writes"
import { ApiError, NetworkError, isApiError, replayable, sessionLost } from "../api/error"
import { ResourceStream, type Reaction } from "../api/live"
import { createRuntime, wantsMock, type Runtime } from "../api/runtime"
import { advanceActivity, asSteps, mergeSteps } from "../contract/activity"
import { lineageHistoryUrls, loadActivityHistory } from "../contract/activity-history"
import { clockFrom, decideSnapshot, decideUpsert, isTerminalStatus, type Clock } from "../contract/stream"
import { codePointLength, formatClock } from "../contract/text"
import type {
  ActivityStep,
  ArticleResponse,
  Citation,
  Conversation,
  Frame,
  RequestView,
  SessionResponse,
  StreamPosition,
  TurnView,
} from "../contract/types"
import { prefersReduced } from "../ui/motion"
import { HttpApi } from "../api/http"
import type { DetailLevel, GenerationInput, ModelCatalog } from "../contract/types"

export type Phase = "input" | "run" | "holding" | "reading"

export function phaseOnArrival(requestId: string | undefined): Phase {
  return requestId ? "run" : "input"
}

const sampleUrl = "https://www.bilibili.com/video/BV1mockvideo?p=1"

function browserStore(): KeyValue {
  try {
    sessionStorage.setItem("__vidlens", "1")
    sessionStorage.removeItem("__vidlens")
    return sessionStorage
  } catch {
    return memoryStore()
  }
}

function errorText(error: unknown): string {
  if (error instanceof ApiError) {
    const fields = error.meta?.field_errors?.map((item) => item.message).filter(Boolean) ?? []
    if (fields.length) return fields.join(" ")
    return error.detail || "请求没有完成。"
  }
  if (error instanceof NetworkError) return error.message
  return "请求没有完成。"
}

function normalizeStep(step: ActivityStep): ActivityStep {
  return {
    ...step,
    frames: Array.isArray(step.frames) ? step.frames : [],
    frame_ids: Array.isArray(step.frame_ids) ? step.frame_ids : [],
  }
}

function isRequestView(value: unknown): value is RequestView {
  return !!value && typeof value === "object" && "request" in value && "activity" in value && "stream" in value
}

function isTurnView(value: unknown): value is TurnView {
  return !!value && typeof value === "object" && "turn" in value && "activity" in value && "stream" in value
}

export function useVisitor(requestId: string | undefined, navigate: (path: string) => void) {
  const [booted, setBooted] = useState(false)
  const [bootError, setBootError] = useState("")
  const [lost, setLost] = useState("")
  const [notice, setNotice] = useState("")
  const [mockMode, setMockMode] = useState(false)
  const [session, setSession] = useState<SessionResponse | null>(null)
  const [phase, setPhase] = useState<Phase>(() => phaseOnArrival(requestId))
  const [link, setLink] = useState("")
  const [purpose, setPurpose] = useState("")
  const [detailLevel, setDetailLevel] = useState<DetailLevel>("standard")
  const [catalog, setCatalog] = useState<ModelCatalog | null>(null)
  const [selectedModel, setSelectedModel] = useState("")
  const [catalogError, setCatalogError] = useState("")
  const [linkError, setLinkError] = useState("")
  const [submitting, setSubmitting] = useState(false)
  const [request, setRequest] = useState<RequestView | null>(null)
  const [steps, setSteps] = useState<ActivityStep[]>([])
  const [article, setArticle] = useState<ArticleResponse | null>(null)
  const [articleError, setArticleError] = useState("")
  const [pageError, setPageError] = useState("")
  const [reconnecting, setReconnecting] = useState(false)
  const [historyOpen, setHistoryOpen] = useState(false)
  const [conversation, setConversation] = useState<Conversation | null>(null)
  const [turns, setTurns] = useState<TurnView[]>([])
  const [turnSteps, setTurnSteps] = useState<Record<string, ActivityStep[]>>({})
  const turnStepsRef = useRef<Record<string, ActivityStep[]>>({})
  turnStepsRef.current = turnSteps
  const [historyState, setHistoryState] = useState<Record<string, string>>({})
  const [qaOpen, setQaOpen] = useState(false)
  const [draft, setDraft] = useState("")
  const [externalAllowed, setExternalAllowed] = useState(false)
  const [referenceLinks, setReferenceLinks] = useState("")
  const [frameChip, setFrameChip] = useState<{
    id: string
    label: string
  } | null>(null)
  const [passageChip, setPassageChip] = useState<{
    sectionId: string
    blockId: string
    quote: string
  } | null>(null)
  const [qaError, setQaError] = useState("")
  const [qaBusy, setQaBusy] = useState(false)
  const [expanded, setExpanded] = useState<Record<string, boolean | undefined>>({})
  const [focusToken, setFocusToken] = useState(0)
  const [frameMap, setFrameMap] = useState<Map<string, Frame>>(() => new Map())
  const [lightbox, setLightbox] = useState<{
    open: boolean
    frames: Frame[]
    index: number
    origin: HTMLElement | null
  }>({ open: false, frames: [], index: 0, origin: null })
  const [live, setLive] = useState("")

  const runtimeRef = useRef<Runtime | null>(null)
  const sessionRef = useRef<SessionResponse | null>(null)
  const requestRef = useRef<RequestView | null>(null)
  const articleRef = useRef<ArticleResponse | null>(null)
  const conversationRef = useRef<Conversation | null>(null)
  const turnsRef = useRef<TurnView[]>([])
  const stepsRef = useRef<ActivityStep[]>([])
  const phaseRef = useRef<Phase>(phaseOnArrival(requestId))
  const historyRef = useRef(false)
  const lightboxRef = useRef(false)
  const engagedRef = useRef(false)
  const requestClock = useRef<Clock | null>(null)
  const turnClock = useRef<Clock | null>(null)
  const requestStream = useRef<ResourceStream | null>(null)
  const turnStream = useRef<ResourceStream | null>(null)
  const requestStreamId = useRef("")
  const turnStreamId = useRef("")
  const epoch = useRef(0)
  const loadedArticle = useRef("")
  const articleToken = useRef(0)
  const navigateRef = useRef(navigate)
  const storeRef = useRef<KeyValue | null>(null)
  const replayed = useRef(false)
  navigateRef.current = navigate
  requestRef.current = request
  articleRef.current = article
  conversationRef.current = conversation
  turnsRef.current = turns
  stepsRef.current = steps
  phaseRef.current = phase
  historyRef.current = historyOpen
  lightboxRef.current = lightbox.open
  engagedRef.current = historyOpen || lightbox.open
  sessionRef.current = session

  function store() {
    if (!storeRef.current) storeRef.current = browserStore()
    return storeRef.current
  }

  function absorb(list: Frame[]) {
    if (!list.length) return
    setFrameMap((prev) => {
      let changed = false
      const next = new Map(prev)
      for (const frame of list) {
        const old = next.get(frame.id)
        if (
          !old ||
          old.availability !== frame.availability ||
          old.urls.full !== frame.urls.full ||
          old.urls.thumbnail !== frame.urls.thumbnail ||
          old.caption !== frame.caption
        ) {
          next.set(frame.id, frame)
          changed = true
        }
      }
      return changed ? next : prev
    })
  }

  function rememberConversation(next: Conversation) {
    conversationRef.current = next
    setConversation(next)
  }

  function markLost(message: string) {
    requestStream.current?.stop()
    turnStream.current?.stop()
    requestStreamId.current = ""
    turnStreamId.current = ""
    clearWrites(store())
    setLost(message || "匿名会话已失效，请重新开始。")
  }

  async function report(error: unknown, setMessage: (value: string) => void, slot?: string) {
    if (sessionLost(error)) {
      markLost(error instanceof ApiError ? error.detail : "匿名会话已失效，请重新开始。")
      return
    }
    if (slot && !replayable(error)) forgetWrite(store(), slot)
    if (error instanceof ApiError && error.status === 403 && runtimeRef.current) {
      try {
        const next = await runtimeRef.current.api.session()
        sessionRef.current = next
        setSession(next)
      } catch {
        /* 下一次操作再取令牌 */
      }
      setMessage("请再试一次。")
      return
    }
    setMessage(errorText(error))
  }

  function installRequest(view: RequestView, replace: boolean) {
    const normalized = asSteps(view.activity.recent_steps).map(normalizeStep)
    requestRef.current = view
    setRequest(view)
    setSteps((prev) => {
      const merged = mergeSteps(prev, normalized, replace)
      stepsRef.current = merged
      return merged
    })
    absorb(normalized.flatMap((step) => step.frames))
    if (view.request.status === "completed" && view.actions.can_read_article) void loadArticle(view.request.id, false)
    if (view.request.status !== "completed") return
    setPhase((current) => {
      if (current === "reading" || current === "holding" || current === "input") return current
      return engagedRef.current ? "holding" : "reading"
    })
  }

  function onRequestSnapshot(value: unknown, source: "sse" | "http"): Reaction {
    if (!isRequestView(value)) return "resync"
    if (value.schema_version !== 1) {
      setNotice("进度数据的协议版本和当前页面不一致。")
      return "close"
    }
    const next = clockFrom(value.stream)
    if (!next) return "resync"
    const decision = decideSnapshot(requestClock.current, next, isTerminalStatus(value.request.status), source)
    if (decision === "ignore") return "stay"
    if (decision === "confirm") return isTerminalStatus(value.request.status) ? "close" : "stay"
    if (decision === "resync") return "resync"
    const previous = requestClock.current
    const replace = !previous || next.revision !== previous.revision || next.runId !== previous.runId
    requestClock.current = next
    installRequest(value, replace)
    return isTerminalStatus(value.request.status) ? "close" : "stay"
  }

  function onRequestUpsert(step: ActivityStep, currentId: string | null, position: StreamPosition): Reaction {
    const next = clockFrom(position)
    if (!next) return "resync"
    const decision = decideUpsert(requestClock.current, next, step.last_seq)
    if (decision === "ignore") return "stay"
    if (decision === "resync") return "resync"
    const known = !currentId || currentId === step.id || stepsRef.current.some((item) => item.id === currentId)
    if (!known) return "resync"
    const normalized = normalizeStep(step)
    requestClock.current = next
    const firstSeen = !stepsRef.current.some((item) => item.id === normalized.id)
    setSteps((prev) => {
      const merged = mergeSteps(prev, [normalized], false)
      stepsRef.current = merged
      return merged
    })
    setRequest((prev) =>
      prev
        ? {
            ...prev,
            activity: advanceActivity(prev.activity, normalized, currentId, firstSeen),
            stream: { ...prev.stream, ...position },
          }
        : prev,
    )
    absorb(normalized.frames)
    return "stay"
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
      [view.turn.id]: mergeSteps(prev[view.turn.id] ?? [], asSteps(view.activity.recent_steps).map(normalizeStep), replace),
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
    const id = turnStreamId.current
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

  function startRequestStream(view: RequestView, gen: number) {
    if (isTerminalStatus(view.request.status)) return
    if (requestStreamId.current === view.request.id) return
    requestStream.current?.stop()
    const id = view.request.id
    requestStreamId.current = id
    const stream = new ResourceStream({
      events: (url) => runtimeRef.current!.events(url),
      resourcePath: `/api/v1/requests/${encodeURIComponent(id)}/events`,
      scope: { type: "request", id },
      getSnapshot: () => runtimeRef.current!.api.getRequest(id),
      onSnapshot: (payload, source) => (gen === epoch.current ? onRequestSnapshot(payload, source) : "close"),
      onUpsert: (step, currentId, position) => (gen === epoch.current ? onRequestUpsert(step, currentId, position) : "close"),
      onAuth: (message) => markLost(message),
      onStatus: (status) => setReconnecting(status === "reconnecting"),
    })
    requestStream.current = stream
    stream.start(view.stream.cursor)
  }

  function startTurnStream(view: TurnView) {
    if (isTerminalStatus(view.turn.status)) return
    if (turnStreamId.current === view.turn.id) return
    turnStream.current?.stop()
    const gen = epoch.current
    const id = view.turn.id
    const conversationId = view.turn.conversation_id
    turnStreamId.current = id
    turnClock.current = clockFrom(view.stream)
    const stream = new ResourceStream({
      events: (url) => runtimeRef.current!.events(url),
      resourcePath: (() => {
        const u = new URL(
          view.stream.url || `/api/v1/conversations/${encodeURIComponent(conversationId)}/turns/${encodeURIComponent(id)}/events`,
          location.origin,
        )
        u.searchParams.set("contract", "2")
        return u.pathname + u.search
      })(),
      scope: { type: "turn", id },
      getSnapshot: () => runtimeRef.current!.api.getTurn(conversationId, id),
      onSnapshot: (payload, source) => (gen === epoch.current ? onTurnSnapshot(payload, source) : "close"),
      onUpsert: (step, currentId, position) => (gen === epoch.current ? onTurnUpsert(step, currentId, position) : "close"),
      onAuth: (message) => markLost(message),
      onStatus: (status) => {
        if (status === "reconnecting") setLive("正在重新连接回答")
      },
    })
    turnStream.current = stream
    stream.start(view.stream.cursor)
  }

  async function loadArticle(id: string, force: boolean) {
    const rt = runtimeRef.current
    if (!rt) return
    if (!force && loadedArticle.current === id) return
    const token = ++articleToken.current
    try {
      const body = await rt.api.article(id)
      if (token !== articleToken.current || requestRef.current?.request.id !== id) return
      loadedArticle.current = id
      articleRef.current = body
      setArticle(body)
      setArticleError("")
      absorb(body.frames ?? [])
    } catch (error) {
      if (token !== articleToken.current || requestRef.current?.request.id !== id) return
      if (sessionLost(error)) markLost(error instanceof ApiError ? error.detail : "匿名会话已失效，请重新开始。")
      else setArticleError(errorText(error))
    }
  }

  async function load(id: string, gen: number) {
    const rt = runtimeRef.current
    if (!rt) return
    try {
      const view = await rt.api.getRequest(id)
      if (gen !== epoch.current) return
      const reaction = onRequestSnapshot(view, "http")
      if (gen !== epoch.current) return
      if (reaction !== "close" && !isTerminalStatus(view.request.status)) startRequestStream(view, gen)
    } catch (error) {
      if (gen !== epoch.current) return
      if (sessionLost(error)) markLost(error instanceof ApiError ? error.detail : "匿名会话已失效，请重新开始。")
      else setPageError(errorText(error))
    }
  }

  async function submitUrl(videoUrl: string) {
    if (codePointLength(purpose.trim()) > 500) {
      setLinkError("关注点最多 500 字，请精简后再提交。")
      return
    }
    const chosen = catalog?.models.find((m) => m.id === selectedModel)
    if (!mockMode && !chosen) {
      setLinkError("请先读取并选择可用模型。")
      return
    }
    const options: GenerationInput = {
      detail_level: detailLevel,
      ...(purpose.trim() ? { purpose: purpose.replace(/\r\n?/g, "\n").trim() } : {}),
      ...(chosen && !mockMode ? { model_id: chosen.id, model_revision_id: chosen.revision_id } : {}),
    }
    const trimmed = videoUrl.trim()
    if (!trimmed) {
      setLinkError("先贴上链接。")
      document.getElementById("link")?.focus()
      return
    }
    if (sessionRef.current && !sessionRef.current.capabilities.accepting_requests) {
      setLinkError("现在暂时不能开始新的理解。")
      return
    }
    const rt = runtimeRef.current
    if (!rt) {
      setLinkError("正在连接服务。")
      return
    }
    setLink(trimmed)
    setLinkError("")
    setSubmitting(true)
    const body = JSON.stringify({ video_url: trimmed, ...options })
    const slot = "request:create"
    const pending = pendingWrite(store(), slot)
    const windowMs = (sessionRef.current?.capabilities.idempotency_window_seconds ?? 86400) * 1000
    const key = pending && pending.body === body && canReplay(pending, Date.now()) ? pending.key : newIdempotencyKey()
    rememberWrite(store(), {
      slot,
      key,
      path: "/api/v1/requests",
      body,
      expiresAt: Date.now() + windowMs,
      startedAt: Date.now(),
    })
    try {
      const accepted = await rt.api.createRequest(trimmed, key, options)
      forgetWrite(store(), slot)
      navigateRef.current(`/r/${encodeURIComponent(accepted.body.request.id)}`)
    } catch (error) {
      await report(error, setLinkError, slot)
    } finally {
      setSubmitting(false)
    }
  }

  async function replayPending() {
    const pending = pendingWrite(store(), "request:create")
    if (!pending || !canReplay(pending, Date.now())) return
    let videoUrl = ""
    let options: GenerationInput = {}
    try {
      const saved = JSON.parse(pending.body) as GenerationInput & {
        video_url?: string
      }
      videoUrl = String(saved.video_url || "")
      options = restoreGenerationInput(saved)
    } catch {
      forgetWrite(store(), "request:create")
      return
    }
    if (!videoUrl || !runtimeRef.current) return
    setLink(videoUrl)
    setSubmitting(true)
    try {
      const accepted = await runtimeRef.current.api.createRequest(videoUrl, pending.key, options)
      forgetWrite(store(), "request:create")
      navigateRef.current(`/r/${encodeURIComponent(accepted.body.request.id)}`)
    } catch (error) {
      await report(error, setLinkError, "request:create")
    } finally {
      setSubmitting(false)
    }
  }

  async function ensureConversation(force: boolean) {
    const current = requestRef.current
    const rt = runtimeRef.current
    if (!current || !rt)
      throw new ApiError({
        status: 409,
        code: "ARTICLE_NOT_READY",
        detail: "文章还没有发布。",
      })
    if (!force && conversationRef.current?.request_id === current.request.id) return conversationRef.current
    const response = await rt.api.conversation(current.request.id)
    rememberConversation(response.conversation)
    const page = await rt.api.turns(response.conversation.id)
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
  }

  async function reloadCatalog() {
    try {
      const c = await new HttpApi().models()
      setCatalog(c)
      setSelectedModel((prev) => prev || c.default_model_id || c.models[0]?.id || "")
      setCatalogError(c.models.length ? "" : "站长暂时没有启用可用模型。")
    } catch (e) {
      setCatalogError(errorText(e))
    }
  }
  useEffect(() => {
    void reloadCatalog()
  }, [])

  useEffect(() => {
    let cancelled = false
    void createRuntime()
      .then(async (rt) => {
        if (cancelled) return
        runtimeRef.current = rt
        setMockMode(wantsMock())
        try {
          const next = await rt.api.session()
          if (cancelled) return
          sessionRef.current = next
          setSession(next)
          if (next.schema_version !== 1) setNotice("当前页面与服务的协议版本不一致。")
          setBooted(true)
        } catch (error) {
          if (cancelled) return
          if (sessionLost(error)) markLost(error instanceof ApiError ? error.detail : "匿名会话已失效，请重新开始。")
          else setBootError(errorText(error))
        }
      })
      .catch((error: unknown) => {
        if (!cancelled) setBootError(errorText(error))
      })
    return () => {
      cancelled = true
    }
  }, [])

  useEffect(() => {
    if (!booted || requestId || replayed.current) return
    replayed.current = true
    void replayPending()
  }, [booted, requestId])

  useEffect(() => {
    if (!booted) return
    const gen = ++epoch.current
    requestStream.current?.stop()
    turnStream.current?.stop()
    requestStreamId.current = ""
    turnStreamId.current = ""
    requestClock.current = null
    turnClock.current = null
    loadedArticle.current = ""
    articleToken.current += 1
    requestRef.current = null
    setRequest(null)
    setSteps([])
    stepsRef.current = []
    setArticle(null)
    setArticleError("")
    setPageError("")
    setConversation(null)
    conversationRef.current = null
    setTurns([])
    setTurnSteps({})
    setQaOpen(false)
    setQaError("")
    setQaBusy(false)
    setDraft("")
    setExternalAllowed(false)
    setReferenceLinks("")
    setFrameChip(null)
    setPassageChip(null)
    setExpanded({})
    setHistoryOpen(false)
    setReconnecting(false)
    setLightbox({ open: false, frames: [], index: 0, origin: null })
    setFrameMap(new Map())
    if (!requestId) {
      setPhase("input")
      return
    }
    setPhase("run")
    void load(requestId, gen)
    return () => {
      epoch.current += 1
      requestStream.current?.stop()
      turnStream.current?.stop()
    }
  }, [booted, requestId])

  useEffect(() => {
    if (!qaOpen) {
      if (turnStreamId.current) {
        turnStream.current?.stop()
        turnStreamId.current = ""
      }
      return
    }
    const activeId = conversation?.active_turn_id
    if (!activeId) {
      if (turnStreamId.current) {
        turnStream.current?.stop()
        turnStreamId.current = ""
      }
      return
    }
    const turn = turns.find((item) => item.turn.id === activeId)
    if (!turn || isTerminalStatus(turn.turn.status)) return
    startTurnStream(turn)
  }, [qaOpen, conversation?.active_turn_id, turns])

  useEffect(() => {
    if (phase === "holding") setLive("文章已就绪")
  }, [phase])

  useEffect(() => {
    if (phase !== "run" || !request) return
    if (request.request.status === "canceled") setLive("已停止理解")
    if (request.request.status === "failed") setLive(request.failure?.message || "这次没能完成")
  }, [phase, request])

  const resolvedSteps = useMemo(
    () =>
      steps.map((step) => {
        const frames = (step.frames ?? []).map((frame) => frameMap.get(frame.id) ?? frame)
        if ((step.frames ?? []).every((frame, index) => frame === frames[index])) return step
        return { ...step, frames }
      }),
    [steps, frameMap],
  )

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

  const suggesting = article?.article.title ? `《${article.article.title}》主要在讲什么？` : "这条视频主要在讲什么？"

  async function loadHistory(turnId?: string) {
    const rt = runtimeRef.current
    const view = turnId ? turnsRef.current.find((item) => item.turn.id === turnId) : requestRef.current
    if (!rt || !view) return
    const id = turnId ?? requestRef.current!.request.id
    if (historyState[id] === "正在加载完整过程…") return
    setHistoryState((prev) => ({ ...prev, [id]: "正在加载完整过程…" }))
    try {
      const urls = [view.activity.history_url, ...(view.activity.attempts ?? []).filter((attempt) => !attempt.current).map((attempt) => attempt.history_url)]
      if ("request" in view) urls.push(...await lineageHistoryUrls(view, (id) => rt.api.getRequest(id)))
      const incoming = (await loadActivityHistory(urls, (url, before) => rt.api.activity(url, before))).map(normalizeStep)
      // Navigation and history loading may race. Never mix another video's steps.
      if (turnId) {
        if (!turnsRef.current.some((item) => item.turn.id === turnId)) return
        setTurnSteps((prev) => ({ ...prev, [turnId]: mergeSteps(prev[turnId] ?? [], incoming, false) }))
      } else {
        if (requestRef.current?.request.id !== id) return
        setSteps((prev) => { const merged = mergeSteps(prev, incoming, false); stepsRef.current = merged; return merged })
      }
      absorb(incoming.flatMap((step) => step.frames))
      setHistoryState((prev) => ({ ...prev, [id]: "" }))
    } catch (error) {
      setHistoryState((prev) => ({ ...prev, [id]: error instanceof Error ? error.message : "完整过程加载失败，请收起后重新展开。" }))
    }
  }

  return {
    booted,
    csrfToken: session?.csrf_token || "",
    bootError,
    lost,
    notice,
    mockMode,
    phase,
    link,
    purpose,
    setPurpose,
    detailLevel,
    setDetailLevel,
    catalog,
    selectedModel,
    setSelectedModel,
    catalogError,
    reloadCatalog,
    setLink,
    linkError,
    submitting,
    request,
    steps: resolvedSteps,
    article,
    articleError: articleError || pageError,
    reconnecting,
    historyOpen,
    historyState,
    loadHistory: () => void loadHistory(),
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
    qaBusy,
    qaError,
    expanded,
    focusToken,
    frames: frameMap,
    lightbox: {
      ...lightbox,
      frames: lightbox.frames.map((frame) => frameMap.get(frame.id) ?? frame),
    },
    live,
    announce: setLive,
    selectionMax: session?.capabilities.selection_chars_max ?? null,
    setLinkValue: setLink,
    submit: () => void submitUrl(link),
    sample: () => {
      if (mockMode) {
        setLink(sampleUrl)
        void submitUrl(sampleUrl)
        return
      }
      setLinkError("请贴上你自己的视频链接。")
      document.getElementById("link")?.focus()
    },
    stop: () =>
      void (async () => {
        const rt = runtimeRef.current
        const current = requestRef.current
        if (!rt || !current) return
        try {
          const view = await rt.api.cancelRequest(current.request.id)
          const reaction = onRequestSnapshot(view, "http")
          if (reaction === "close") {
            requestStream.current?.stop()
            requestStreamId.current = ""
          }
        } catch (error) {
          await report(error, setPageError)
        }
      })(),
    retry: () =>
      void (async () => {
        const rt = runtimeRef.current
        const current = requestRef.current
        if (!rt || !current) return
        try {
          const accepted = await rt.api.retryRequest(current.request.id, newIdempotencyKey())
          navigateRef.current(`/r/${encodeURIComponent(accepted.body.request.id)}`)
        } catch (error) {
          await report(error, setPageError)
        }
      })(),
    beginReading: () => {
      setPhase("reading")
      setQaOpen(false)
    },
    toggleHistory: () =>
      void (async () => {
        const next = !historyRef.current
        historyRef.current = next
        engagedRef.current = next || lightboxRef.current
        setHistoryOpen(next)
        if (next) await loadHistory()
      })(),
    retryArticle: () => {
      const id = requestRef.current?.request.id
      if (id) void loadArticle(id, true)
    },
    openFrames: (frames: Frame[], index: number, origin: HTMLElement) => {
      lightboxRef.current = true
      engagedRef.current = true
      setLightbox({ open: true, frames, index, origin })
    },
    closeLightbox: () => {
      lightboxRef.current = false
      engagedRef.current = historyRef.current
      setLightbox((prev) => ({ ...prev, open: false }))
    },
    setLightboxIndex: (index: number) => setLightbox((prev) => ({ ...prev, index })),
    refreshFrame: (frame: Frame) =>
      void (async () => {
        const rt = runtimeRef.current
        const id = requestRef.current?.request.id
        if (!rt || !id) return
        try {
          absorb([await rt.api.frame(id, frame.id)])
        } catch (error) {
          if (sessionLost(error)) markLost(error instanceof ApiError ? error.detail : "匿名会话已失效，请重新开始。")
        }
      })(),
    cite: (citation: Citation) =>
      void (async () => {
        if (citation.kind === "external_reference" && citation.reference_id) {
          const node = document.getElementById(`reference-${citation.reference_id}`)
          if (node instanceof HTMLDetailsElement) {
            node.open = true
            node.scrollIntoView({ block: "nearest", behavior: "auto" })
            node.querySelector("summary")?.focus()
          }
          return
        }
        if (citation.kind === "video_frame" && citation.frame_id) {
          let frame = frameMap.get(citation.frame_id) ?? null
          const rt = runtimeRef.current
          const id = requestRef.current?.request.id
          if (!frame && rt && id) {
            try {
              frame = await rt.api.frame(id, citation.frame_id)
              absorb([frame])
            } catch (error) {
              setLive(errorText(error))
              return
            }
          }
          if (frame) {
            const origin = document.activeElement instanceof HTMLElement ? document.activeElement : document.body
            lightboxRef.current = true
            engagedRef.current = true
            setLightbox({ open: true, frames: [frame], index: 0, origin })
          }
          return
        }
        if (citation.block_id) {
          try {
            const node = document.querySelector(`[data-block-id="${CSS.escape(citation.block_id)}"]`)
            node?.scrollIntoView({
              behavior: prefersReduced() ? "auto" : "smooth",
              block: "center",
            })
          } catch {
            setLive(citation.label)
          }
          return
        }
        setLive(citation.quote || citation.label)
      })(),
    askFrame: (frame: Frame) => {
      if (phaseRef.current !== "reading" && phaseRef.current !== "holding") return
      setFrameChip({
        id: frame.id,
        label: `${formatClock(frame.time_ms)} · ${frame.caption || "画面"}`,
      })
      setDraft(`这张 ${formatClock(frame.time_ms)} 的画面在说明什么？`)
      if (phaseRef.current === "holding") setPhase("reading")
      setQaOpen(true)
      setFocusToken((value) => value + 1)
      void ensureConversation(true).catch((error: unknown) => void report(error, setQaError))
    },
    attachPassage: (passage: { sectionId: string; blockId: string; quote: string }) => {
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
        const max = sessionRef.current?.capabilities.question_chars_max
        if (max != null && codePointLength(text) > max) {
          setQaError("这个问题太长了。")
          return
        }
        if (turnsRef.current.some((turn) => !isTerminalStatus(turn.turn.status))) {
          setQaError("上一轮回答仍在进行。")
          return
        }
        const rt = runtimeRef.current
        if (!rt) return
        setQaBusy(true)
        setQaError("")
        const sentFrame = frameChip
        const sentPassage = passageChip
        try {
          const current = await ensureConversation(true)
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
              passage: sentPassage
                ? {
                    section_id: sentPassage.sectionId,
                    block_id: sentPassage.blockId,
                    quote: sentPassage.quote,
                  }
                : null,
            },
          }
          const body = JSON.stringify(payload)
          const slot = `turn:${current.id}`
          const pending = pendingWrite(store(), slot)
          const windowMs = (sessionRef.current?.capabilities.idempotency_window_seconds ?? 86400) * 1000
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
          forgetWrite(store(), slot)
          setDraft((value) => (value.trim() === text ? "" : value))
          setExternalAllowed(false)
          setReferenceLinks("")
          setFrameChip((value) => (value?.id === sentFrame?.id ? null : value))
          setPassageChip((value) => (value?.blockId === sentPassage?.blockId && value?.quote === sentPassage?.quote ? null : value))
          turnClock.current = clockFrom(accepted.body.stream)
          installTurn(accepted.body, true)
        } catch (error) {
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
          setQaBusy(false)
        }
      })(),
    stopTurn: () =>
      void (async () => {
        const rt = runtimeRef.current
        const current = conversationRef.current
        const active = turnsRef.current.find((turn) => turn.actions.can_cancel)
        if (!rt || !current || !active) return
        try {
          const view = await rt.api.cancelTurn(current.id, active.turn.id)
          const reaction = onTurnSnapshot(view, "http")
          if (reaction === "close") {
            turnStream.current?.stop()
            turnStreamId.current = ""
          }
        } catch (error) {
          await report(error, setQaError)
        }
      })(),
    retryTurn: (turn: TurnView) =>
      void (async () => {
        const rt = runtimeRef.current
        const current = conversationRef.current
        if (!rt || !current) return
        try {
          const accepted = await rt.api.retryTurn(current.id, turn.turn.id, newIdempotencyKey(), current.last_turn_id)
          turnClock.current = clockFrom(accepted.body.stream)
          installTurn(accepted.body, true)
        } catch (error) {
          await report(error, setQaError)
        }
      })(),
    suggest: () => {
      const title = articleRef.current?.article.title
      setDraft(title ? `《${title}》主要在讲什么？` : "这条视频主要在讲什么？")
      setFocusToken((value) => value + 1)
    },
    removeFrame: () => setFrameChip(null),
    removePassage: () => setPassageChip(null),
    toggleTurn: (id: string, open: boolean) => { setExpanded((prev) => ({ ...prev, [id]: open })); if (open) void loadHistory(id) },
  }
}
