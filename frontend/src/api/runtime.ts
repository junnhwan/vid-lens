import { HttpApi } from "./http"
import type { LiveSocket } from "./live"
import type {
  GenerationInput,
  Accepted,
  ActivityPage,
  ArticleResponse,
  ConversationResponse,
  Frame,
  RequestView,
  SessionResponse,
  TurnPage,
  TurnView,
} from "../contract/types"

export interface VisitorApi {
  session(): Promise<SessionResponse>
  createRequest(videoUrl: string, key: string, options?: GenerationInput): Promise<Accepted<RequestView>>
  getRequest(id: string): Promise<RequestView>
  cancelRequest(id: string): Promise<RequestView>
  retryRequest(id: string, key: string): Promise<Accepted<RequestView>>
  article(id: string): Promise<ArticleResponse>
  frame(requestId: string, frameId: string): Promise<Frame>
  activity(url: string, before?: string | null): Promise<ActivityPage>
  conversation(requestId: string): Promise<ConversationResponse>
  turns(conversationId: string, beforeTurnNo?: number | null): Promise<TurnPage>
  createTurn(conversationId: string, key: string, body: unknown): Promise<Accepted<TurnView>>
  getTurn(conversationId: string, turnId: string): Promise<TurnView>
  cancelTurn(conversationId: string, turnId: string): Promise<TurnView>
  retryTurn(conversationId: string, turnId: string, key: string, expectedLastTurnId: string | null): Promise<Accepted<TurnView>>
}

export interface Runtime {
  api: VisitorApi
  events: (url: string) => LiveSocket
}

export function wantsMock(): boolean {
  if (!import.meta.env.DEV) return false
  if (typeof location === "undefined") return false
  return new URLSearchParams(location.search).get("mock") === "1" || import.meta.env.VITE_API === "mock"
}

export function createHttpRuntime(): Runtime {
  const api = new HttpApi()
  return {
    api,
    events: (url) => new EventSource(url, { withCredentials: true }) as unknown as LiveSocket,
  }
}

export async function createRuntime(): Promise<Runtime> {
  if (import.meta.env.DEV && wantsMock()) {
    const mock = await import("./mock")
    return mock.createMockRuntime()
  }
  return createHttpRuntime()
}
