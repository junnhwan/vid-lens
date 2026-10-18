import type { Runtime } from "../api/runtime"
import type { KeyValue } from "../api/writes"
import type { ArticleResponse, RequestView, SessionResponse } from "../contract/types"
import type { VisitorWorkspace } from "./visitorWorkspace"

export type Phase = "input" | "run" | "holding" | "reading"

export interface VisitorContext {
  workspace: VisitorWorkspace
  runtime(): Runtime | null
  request(): RequestView | null
  session(): SessionResponse | null
  article(): ArticleResponse | null
  phase(): Phase
  store(): KeyValue
  read(): void
  messages: {
    lost(message: string): void
    announce(message: string): void
    notice(message: string): void
    report(error: unknown, setMessage: (message: string) => void, slot?: string): Promise<void>
  }
}
