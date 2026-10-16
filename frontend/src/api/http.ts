import type { SiteCapacity } from "../contract/capacity"
import type {
  GenerationInput,
  ModelCatalog,
  Accepted,
  ActivityPage,
  ArticleResponse,
  ConversationResponse,
  Frame,
  Problem,
  RequestView,
  SessionResponse,
  TurnPage,
  TurnView,
} from "../contract/types"
import { ApiError, NetworkError } from "./error"

export interface HttpOptions {
  fetchImpl?: typeof fetch
  csrf?: () => string
  setCsrf?: (token: string) => void
}

function problemFrom(status: number, payload: unknown): Problem {
  const body = payload && typeof payload === "object" ? (payload as Partial<Problem>) : {}
  return {
    status: body.status || status,
    code: body.code || "SERVICE_UNAVAILABLE",
    detail: body.detail || "服务暂不可用，请稍后读取资源状态。",
    trace_id: body.trace_id,
    meta: body.meta ?? null,
    title: body.title,
    type: body.type,
    instance: body.instance,
  }
}

export class HttpApi {
  csrfToken = ""
  private fetchImpl: typeof fetch

  constructor(options: HttpOptions = {}) {
    this.fetchImpl = options.fetchImpl ?? fetch.bind(globalThis)
    if (options.csrf) this.csrfToken = options.csrf()
  }

  async request<T>(method: string, path: string, body?: unknown, key?: string): Promise<{ status: number; headers: Headers; body: T }> {
    let response: Response
    try {
      response = await this.fetchImpl(path, {
        method,
        credentials: "include",
        headers: {
          accept: "application/json",
          "X-VidLens-Contract": "2",
          ...(body !== undefined ? { "content-type": "application/json" } : {}),
          ...(method !== "GET" && path !== "/api/v1/session" ? { "x-csrf-token": this.csrfToken } : {}),
          ...(key ? { "idempotency-key": key } : {}),
        },
        body: body !== undefined ? JSON.stringify(body) : undefined,
      })
    } catch {
      throw new NetworkError()
    }
    const text = await response.text()
    let parsed: unknown = null
    if (text) {
      try {
        parsed = JSON.parse(text)
      } catch {
        throw new ApiError({
          status: response.status,
          code: "SERVICE_UNAVAILABLE",
          detail: "响应无法读取。",
        })
      }
    }
    if (!response.ok) throw new ApiError(problemFrom(response.status, parsed))
    return {
      status: response.status,
      headers: response.headers,
      body: parsed as T,
    }
  }

  private accepted<T>(result: { status: number; headers: Headers; body: T }): Accepted<T> {
    return {
      status: result.status,
      replayed: result.headers.get("idempotency-replayed") === "true",
      expiresAt: result.headers.get("idempotency-expires-at"),
      location: result.headers.get("location"),
      body: result.body,
    }
  }

  async session(): Promise<SessionResponse> {
    const result = await this.request<SessionResponse>("POST", "/api/v1/session", {})
    this.csrfToken = result.body.csrf_token
    return result.body
  }

  createRequest(videoUrl: string, key: string, options?: GenerationInput) {
    return this.request<RequestView>("POST", "/api/v1/requests", { video_url: videoUrl, ...options }, key).then((result) =>
      this.accepted(result),
    )
  }

  models() {
    return this.request<ModelCatalog>("GET", "/api/v1/models").then((r) => r.body)
  }
  stats() {
    return this.request<{ completed_readings: number; likes: number }>("GET", "/api/v1/site/stats").then((r) => r.body)
  }
  capacity() {
    return this.request<SiteCapacity>("GET", "/api/v1/site/capacity").then((r) => r.body)
  }
  like(set = false) {
    return this.request<{ liked: boolean; count: number }>(set ? "PUT" : "GET", "/api/v1/site/like", set ? {} : undefined).then(
      (r) => r.body,
    )
  }

  getRequest(id: string) {
    return this.request<RequestView>("GET", `/api/v1/requests/${encodeURIComponent(id)}`).then((result) => result.body)
  }

  cancelRequest(id: string) {
    return this.request<RequestView>("POST", `/api/v1/requests/${encodeURIComponent(id)}/cancel`, {}).then((result) => result.body)
  }

  retryRequest(id: string, key: string) {
    return this.request<RequestView>("POST", `/api/v1/requests/${encodeURIComponent(id)}/retry`, {}, key).then((result) =>
      this.accepted(result),
    )
  }

  article(id: string) {
    return this.request<ArticleResponse>("GET", `/api/v1/requests/${encodeURIComponent(id)}/article`).then((result) => result.body)
  }

  frame(requestId: string, frameId: string) {
    return this.request<{ schema_version: number; frame: Frame }>(
      "GET",
      `/api/v1/requests/${encodeURIComponent(requestId)}/frames/${encodeURIComponent(frameId)}`,
    ).then((result) => result.body.frame)
  }

  activity(url: string, before?: string | null) {
    const path = new URL(url, "http://vidlens.local")
    if (before) path.searchParams.set("before", before)
    const query = `${path.pathname}${path.search}`
    return this.request<ActivityPage>("GET", query).then((result) => result.body)
  }

  conversation(requestId: string) {
    return this.request<ConversationResponse>("POST", `/api/v1/requests/${encodeURIComponent(requestId)}/conversation`, {}).then(
      (result) => result.body,
    )
  }

  turns(conversationId: string, beforeTurnNo?: number | null) {
    const query = beforeTurnNo ? `?before_turn_no=${beforeTurnNo}` : ""
    return this.request<TurnPage>("GET", `/api/v1/conversations/${encodeURIComponent(conversationId)}/turns${query}`).then(
      (result) => result.body,
    )
  }

  createTurn(conversationId: string, key: string, body: unknown) {
    return this.request<TurnView>("POST", `/api/v1/conversations/${encodeURIComponent(conversationId)}/turns`, body, key).then((result) =>
      this.accepted(result),
    )
  }

  getTurn(conversationId: string, turnId: string) {
    return this.request<TurnView>(
      "GET",
      `/api/v1/conversations/${encodeURIComponent(conversationId)}/turns/${encodeURIComponent(turnId)}`,
    ).then((result) => result.body)
  }

  cancelTurn(conversationId: string, turnId: string) {
    return this.request<TurnView>(
      "POST",
      `/api/v1/conversations/${encodeURIComponent(conversationId)}/turns/${encodeURIComponent(turnId)}/cancel`,
      {},
    ).then((result) => result.body)
  }

  retryTurn(conversationId: string, turnId: string, key: string, expectedLastTurnId: string | null) {
    return this.request<TurnView>(
      "POST",
      `/api/v1/conversations/${encodeURIComponent(conversationId)}/turns/${encodeURIComponent(turnId)}/retry`,
      { expected_last_turn_id: expectedLastTurnId },
      key,
    ).then((result) => this.accepted(result))
  }
}
