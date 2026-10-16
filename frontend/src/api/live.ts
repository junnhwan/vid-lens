import type { ActivityStep, StreamEnvelope, StreamPosition } from "../contract/types"
import { ApiError } from "./error"

export type Reaction = "stay" | "resync" | "close"

export interface LiveSocket {
  readyState: number
  onerror: ((this: LiveSocket, ev: Event) => void) | null
  addEventListener(type: string, listener: (event: MessageEvent) => void): void
  close(): void
}

export interface ResourceStreamOptions {
  events: (url: string) => LiveSocket
  getSnapshot: () => Promise<unknown>
  onSnapshot: (view: unknown, source: "sse" | "http") => Reaction
  onUpsert: (step: ActivityStep, currentStepId: string | null, position: StreamPosition) => Reaction
  onAuth: (message: string) => void
  onStatus: (status: "live" | "reconnecting" | "closed") => void
  scope: { type: string; id: string }
  resourcePath: string
}

const CONNECTING = 0
const CLOSED = 2

function sleep(ms: number, token: { cancelled: boolean }) {
  return new Promise<void>((resolve) => {
    if (token.cancelled) {
      resolve()
      return
    }
    const timer = setTimeout(() => {
      clearInterval(watch)
      resolve()
    }, ms)
    const watch = setInterval(() => {
      if (!token.cancelled) return
      clearTimeout(timer)
      clearInterval(watch)
      resolve()
    }, 30)
  })
}

export class ResourceStream {
  private socket: LiveSocket | null = null
  private generation = 0
  private stopped = false
  private recovering = false
  private attempt = 0
  private cancelSleep = { cancelled: false }

  constructor(private options: ResourceStreamOptions) {}

  start(cursor: string) {
    this.stopped = false
    this.open(cursor)
  }

  stop() {
    this.stopped = true
    this.cancelSleep.cancelled = true
    this.generation += 1
    this.socket?.close()
    this.socket = null
    this.options.onStatus("closed")
  }

  private endpoint(cursor: string) {
    const url = new URL(this.options.resourcePath, "http://vidlens.local")
    url.searchParams.set("after", cursor)
    return `${url.pathname}${url.search}`
  }

  private open(cursor: string) {
    if (this.stopped) return
    this.socket?.close()
    const generation = ++this.generation
    const socket = this.options.events(this.endpoint(cursor))
    this.socket = socket
    const hear = (type: string) => (event: MessageEvent) => {
      if (generation !== this.generation || this.stopped) return
      this.onEvent(type, event)
    }
    socket.addEventListener("view.snapshot", hear("view.snapshot"))
    socket.addEventListener("activity.upsert", hear("activity.upsert"))
    socket.addEventListener("stream.reset", hear("stream.reset"))
    socket.addEventListener("stream.auth_required", hear("stream.auth_required"))
    socket.onerror = () => {
      if (generation !== this.generation || this.stopped) return
      if (socket.readyState === CONNECTING) {
        this.options.onStatus("reconnecting")
        return
      }
      if (socket.readyState === CLOSED) void this.recover(false)
    }
  }

  private onEvent(type: string, event: MessageEvent) {
    if (type === "stream.reset" || type === "stream.auth_required") {
      let message = "匿名会话已失效。"
      try {
        const envelope = JSON.parse(String(event.data)) as StreamEnvelope
        if (envelope.payload?.code === "CLIENT_UPDATE_REQUIRED") {
          this.stopped = true
          this.socket?.close()
          this.options.onAuth("此问答包含新版引用，请刷新页面。")
          this.options.onStatus("closed")
          return
        }
        if (type === "stream.auth_required") message = String(envelope.payload?.message || message)
      } catch {
        message = "连接需要重新同步。"
      }
      this.socket?.close()
      this.socket = null
      if (type === "stream.auth_required") {
        this.stopped = true
        this.options.onAuth(message)
        this.options.onStatus("closed")
        return
      }
      void this.recover(true)
      return
    }
    let envelope: StreamEnvelope
    try {
      envelope = JSON.parse(String(event.data)) as StreamEnvelope
    } catch {
      void this.recover(true)
      return
    }
    if (envelope.schema_version !== 1 || envelope.scope?.type !== this.options.scope.type || envelope.scope?.id !== this.options.scope.id) {
      void this.recover(true)
      return
    }
    if (type === "view.snapshot") {
      const reaction = this.options.onSnapshot(envelope.payload, "sse")
      this.after(reaction)
      return
    }
    if (type === "activity.upsert") {
      const payload = envelope.payload as {
        step?: ActivityStep
        current_step_id?: string | null
      }
      const position = envelope.position
      if (!payload?.step || !position) {
        void this.recover(true)
        return
      }
      const reaction = this.options.onUpsert(payload.step, payload.current_step_id ?? null, { ...position, url: this.options.resourcePath })
      this.after(reaction)
    }
  }

  private after(reaction: Reaction) {
    if (reaction === "close") {
      this.stopped = true
      this.socket?.close()
      this.socket = null
      this.options.onStatus("closed")
      return
    }
    if (reaction === "resync") {
      void this.recover(true)
      return
    }
    this.attempt = 0
    this.options.onStatus("live")
  }

  private async recover(immediate: boolean) {
    if (this.stopped || this.recovering) return
    this.recovering = true
    this.generation += 1
    this.socket?.close()
    this.socket = null
    this.options.onStatus("reconnecting")
    const token = { cancelled: false }
    this.cancelSleep = token
    const wait = immediate ? 0 : Math.min(8000, 1000 * 2 ** this.attempt)
    this.attempt += 1
    await sleep(wait, token)
    if (this.stopped || token.cancelled) {
      this.recovering = false
      return
    }
    try {
      const view = await this.options.getSnapshot()
      if (this.stopped) return
      const reaction = this.options.onSnapshot(view, "http")
      this.recovering = false
      if (reaction === "close") {
        this.stopped = true
        this.options.onStatus("closed")
        return
      }
      if (reaction === "resync") {
        void this.recover(false)
        return
      }
      const cursor = (view as { stream?: StreamPosition }).stream?.cursor
      if (!cursor) {
        void this.recover(false)
        return
      }
      this.attempt = 0
      const terminal = reaction === "stay" && isTerminalView(view)
      if (terminal) {
        this.stopped = true
        this.options.onStatus("closed")
        return
      }
      this.open(cursor)
      this.options.onStatus("live")
    } catch (error) {
      this.recovering = false
      if (this.stopped) return
      if (
        error instanceof ApiError &&
        (error.status === 401 || error.status === 404 || error.status === 410 || error.code === "CLIENT_UPDATE_REQUIRED")
      ) {
        this.stopped = true
        if (error.status === 401 || error.code === "CLIENT_UPDATE_REQUIRED") this.options.onAuth(error.detail)
        this.options.onStatus("closed")
        return
      }
      void this.recover(false)
    }
  }
}

function isTerminalView(view: unknown): boolean {
  if (!view || typeof view !== "object") return false
  const body = view as {
    request?: { status?: string }
    turn?: { status?: string }
  }
  const status = body.request?.status || body.turn?.status
  return status === "completed" || status === "failed" || status === "canceled"
}
