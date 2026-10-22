import { useEffect, useMemo, useRef, useState } from "react"
import { ApiError, replayable, sessionLost } from "../api/error"
import { HttpApi } from "../api/http"
import { ResourceStream, type Reaction } from "../api/live"
import { createRuntime, wantsMock, type Runtime } from "../api/runtime"
import {
  canReplay,
  clearWrites,
  forgetWrite,
  memoryStore,
  newIdempotencyKey,
  pendingWrite,
  rememberWrite,
  restoreGenerationInput,
  type KeyValue,
} from "../api/writes"
import { advanceActivity, asSteps, mergeActivitySnapshot, mergeSteps } from "../contract/activity"
import { clockFrom, decideSnapshot, decideUpsert, isTerminalStatus, type Clock } from "../contract/stream"
import { codePointLength } from "../contract/text"
import type {
  ActivityStep,
  DetailLevel, GenerationInput, ModelCatalog,
  RequestView,
  SessionResponse,
  StreamPosition
} from "../contract/types"
import { useVisitorArticle } from "./useVisitorArticle"
import { useVisitorConversation } from "./useVisitorConversation"
import { useVisitorHistory } from "./useVisitorHistory"
import { useVisitorMedia } from "./useVisitorMedia"
import type { Phase, VisitorContext } from "./visitorContext"
import { ObsoleteRequest, errorText, isRequestView, normalizeStep } from "./visitorContract"
import { useVisitorWorkspace } from "./visitorWorkspace"

export type { Phase } from "./visitorContext"

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

export function useVisitor(requestId: string | undefined, navigate: (path: string) => void) {
  const workspace = useVisitorWorkspace(requestId)
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
  const [pageError, setPageError] = useState("")
  const [reconnecting, setReconnecting] = useState(false)
  const [live, setLive] = useState("")

  const runtimeRef = useRef<Runtime | null>(null)
  const sessionRef = useRef<SessionResponse | null>(null)
  const requestRef = useRef<RequestView | null>(null)
  const stepsRef = useRef<ActivityStep[]>([])
  const phaseRef = useRef<Phase>(phaseOnArrival(requestId))
  const requestClock = useRef<Clock | null>(null)
  const navigateRef = useRef(navigate)
  const storeRef = useRef<KeyValue | null>(null)
  const replayed = useRef(false)
  const context: VisitorContext = {
    workspace, runtime: () => runtimeRef.current, request: () => requestRef.current,
    session: () => sessionRef.current, article: () => articleResource.current(), phase: () => phaseRef.current,
    store, read: () => setPhase("reading"), messages: { lost: markLost, announce: setLive, notice: setNotice, report },
  }
  const media = useVisitorMedia(context)
  const articleResource = useVisitorArticle(context, media.absorb)
  const { article, articleError } = articleResource
  const qa = useVisitorConversation(context, media.frames, loadHistory, media.absorb)
  const { frames: frameMap, historyOpen } = media
  const history = useVisitorHistory(context, {
    snapshot: turnId => turnId ? qa.turn(turnId) : requestRef.current,
    merge: (id, incoming, turnId) => {
      if (turnId) {
        if (!qa.turn(turnId)) return false
        qa.mergeHistory(turnId, incoming)
      } else {
        if (requestRef.current?.request.id !== id) return false
        setSteps(previous => { const merged = mergeSteps(previous, incoming, false); stepsRef.current = merged; return merged })
      }
      return true
    },
    absorb: media.absorb,
  })
  const { historyState } = history
  const absorb = media.absorb
  const loadArticle = articleResource.load
  navigateRef.current = navigate
  workspace.route = requestId
  requestRef.current = request
  stepsRef.current = steps
  phaseRef.current = phase
  sessionRef.current = session

  function ownsWorkspace(gen: number, id: string | undefined) {
    return workspace.owns(gen, id)
  }

  function ownsRequest(gen: number, id: string) {
    return workspace.ownsRequest(gen, id, requestRef.current?.request.id)
  }

  function store() {
    if (!storeRef.current) storeRef.current = browserStore()
    return storeRef.current
  }

  function markLost(message: string) {
    workspace.request.stop()
    workspace.turn.stop()


    clearWrites(store())
    setLost(message || "匿名会话已失效，请重新开始。")
  }

  async function report(error: unknown, setMessage: (value: string) => void, slot?: string) {
    if (error instanceof ObsoleteRequest) return
    const gen = workspace.epoch
    const id = workspace.route
    if (sessionLost(error)) {
      markLost(error instanceof ApiError ? error.detail : "匿名会话已失效，请重新开始。")
      return
    }
    if (slot && !replayable(error)) forgetWrite(store(), slot)
    if (error instanceof ApiError && error.status === 403 && runtimeRef.current) {
      try {
        const next = await runtimeRef.current.api.session()
        if (!ownsWorkspace(gen, id)) return
        sessionRef.current = next
        setSession(next)
      } catch {
        /* 下一次操作再取令牌 */
      }
      if (!ownsWorkspace(gen, id)) return
      setMessage("请再试一次。")
      return
    }
    setMessage(errorText(error))
  }

  function installRequest(view: RequestView, replace: boolean) {
    const normalized = asSteps(view.activity.recent_steps).map(normalizeStep)
    const sameResource = requestRef.current?.request.id === view.request.id
    requestRef.current = view
    setRequest(view)
    setSteps((prev) => {
      const merged = mergeActivitySnapshot(prev, normalized, { replace, sameResource, expired: view.activity.history_status === "expired" })
      stepsRef.current = merged
      return merged
    })
    absorb([...(view.activity.frames ?? []), ...normalized.flatMap((step) => step.frames)])
    if (view.request.status === "completed" && view.actions.can_read_article) void loadArticle(view.request.id, false)
    if (view.request.status !== "completed") return
    setPhase((current) => {
      if (current === "reading" || current === "holding" || current === "input") return current
      return media.engaged() ? "holding" : "reading"
    })
  }

  function onRequestSnapshot(value: unknown, source: "sse" | "http"): Reaction {
    if (!isRequestView(value)) return "resync"
    if (value.request.id !== workspace.route) return "close"
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

  function startRequestStream(view: RequestView, gen: number) {
    if (isTerminalStatus(view.request.status)) return
    if (workspace.request.id === view.request.id) return
    workspace.request.stop()
    const id = view.request.id

    const stream = new ResourceStream({
      events: (url) => runtimeRef.current!.events(url),
      resourcePath: `/api/v1/requests/${encodeURIComponent(id)}/events`,
      scope: { type: "request", id },
      getSnapshot: () => runtimeRef.current!.api.getRequest(id),
      onSnapshot: (payload, source) => (ownsRequest(gen, id) ? onRequestSnapshot(payload, source) : "close"),
      onUpsert: (step, currentId, position) => (ownsRequest(gen, id) ? onRequestUpsert(step, currentId, position) : "close"),
      onAuth: (message) => { if (ownsRequest(gen, id)) markLost(message) },
      onStatus: (status) => { if (ownsRequest(gen, id)) setReconnecting(status === "reconnecting") },
    })
    workspace.request.attach(id, stream, view.stream.cursor)
  }

  async function load(id: string, gen: number) {
    const rt = runtimeRef.current
    if (!rt) return
    try {
      const view = await rt.api.getRequest(id)
      if (!ownsWorkspace(gen, id)) return
      const reaction = onRequestSnapshot(view, "http")
      if (!ownsWorkspace(gen, id)) return
      if (reaction !== "close" && !isTerminalStatus(view.request.status)) startRequestStream(view, gen)
    } catch (error) {
      if (!ownsWorkspace(gen, id)) return
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
    const gen = workspace.epoch
    const ownerId = workspace.route
    try {
      const accepted = await rt.api.createRequest(trimmed, key, options)
      if (pendingWrite(store(), slot)?.key === key) forgetWrite(store(), slot)
      if (!ownsWorkspace(gen, ownerId)) return
      navigateRef.current(`/r/${encodeURIComponent(accepted.body.request.id)}`)
    } catch (error) {
      if (!ownsWorkspace(gen, ownerId)) return
      await report(error, setLinkError, slot)
    } finally {
      if (ownsWorkspace(gen, ownerId)) setSubmitting(false)
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
    const gen = workspace.epoch
    const ownerId = workspace.route
    try {
      const accepted = await runtimeRef.current.api.createRequest(videoUrl, pending.key, options)
      if (pendingWrite(store(), "request:create")?.key === pending.key) forgetWrite(store(), "request:create")
      if (!ownsWorkspace(gen, ownerId)) return
      navigateRef.current(`/r/${encodeURIComponent(accepted.body.request.id)}`)
    } catch (error) {
      if (!ownsWorkspace(gen, ownerId)) return
      await report(error, setLinkError, "request:create")
    } finally {
      if (ownsWorkspace(gen, ownerId)) setSubmitting(false)
    }
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
    if (!booted) return
    const gen = workspace.begin()
    workspace.request.stop()
    workspace.turn.stop()


    requestClock.current = null
    articleResource.reset()
    qa.reset()
    media.reset()
    history.reset()
    requestRef.current = null
    setRequest(null)
    setSteps([])
    stepsRef.current = []
    setPageError("")
    setSubmitting(false)
    setReconnecting(false)
    const cleanup = () => {
      workspace.invalidate()
    }
    if (!requestId) {
      setPhase("input")
      return cleanup
    }
    setPhase("run")
    void load(requestId, gen)
    return cleanup
  }, [booted, requestId])

  useEffect(() => {
    if (!booted || requestId || replayed.current) return
    replayed.current = true
    void replayPending()
  }, [booted, requestId])

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

  async function loadHistory(turnId?: string) { await history.load(turnId) }

  return {
    ...qa.view,
    ...media.view,
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
    request: request ? { ...request, activity: { ...request.activity, frames: request.activity.frames?.map((frame) => frameMap.get(frame.id) ?? frame) } } : request,
    steps: resolvedSteps,
    article,
    articleError: articleError || pageError,
    reconnecting,
    historyOpen,
    historyState,
    loadHistory: () => void loadHistory(),
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
        const gen = workspace.epoch
        const id = current.request.id
        try {
          const view = await rt.api.cancelRequest(current.request.id)
          if (!ownsRequest(gen, id)) return
          const reaction = onRequestSnapshot(view, "http")
          if (reaction === "close") {
            workspace.request.stop()

          }
        } catch (error) {
          if (!ownsRequest(gen, id)) return
          await report(error, setPageError)
        }
      })(),
    retry: () =>
      void (async () => {
        const rt = runtimeRef.current
        const current = requestRef.current
        if (!rt || !current) return
        const gen = workspace.epoch
        const id = current.request.id
        try {
          const accepted = await rt.api.retryRequest(current.request.id, newIdempotencyKey())
          if (!ownsRequest(gen, id)) return
          navigateRef.current(`/r/${encodeURIComponent(accepted.body.request.id)}`)
        } catch (error) {
          if (!ownsRequest(gen, id)) return
          await report(error, setPageError)
        }
      })(),
    beginReading: () => {
      setPhase("reading")
      qa.hide()
    },
    toggleHistory: () =>
      void (async () => {
        const next = media.toggleHistory()
        if (next) await loadHistory()
      })(),
    retryArticle: () => {
      const id = requestRef.current?.request.id
      if (id) void loadArticle(id, true)
    },
  }
}
