import { useEffect, useRef, useState } from "react"
import { isApiError, replayable } from "../api/error"
import type { VisitorApi } from "../api/runtime"
import { canReplay, forgetWrite, newIdempotencyKey, pendingWrite, rememberWrite, type KeyValue } from "../api/writes"
import type { Conversation, ConversationSummary, TurnView } from "../contract/types"
import { isTerminalStatus } from "../contract/stream"
import { errorText } from "./visitorContract"

type ConversationApi = Pick<VisitorApi, "conversations" | "createConversation" | "getConversation">
// The turn page is read after metadata and can include a reply accepted by a
// different tab in between. Use its fresh tail for ordering and stream setup.
export function conversationWithTurns(conversation: Conversation, turns: TurnView[]): Conversation {
  const tail = turns.at(-1)?.turn
  return tail ? { ...conversation, last_turn_id: tail.id, active_turn_id: isTerminalStatus(tail.status) ? null : tail.id } : conversation
}
export interface ConversationHistoryControls {
  items: ConversationSummary[]
  busy: boolean
  loading?: boolean
  error: string
  hasMore: boolean
  refresh(): void
  more(): void
  create(): void
  select(id: string): void
}

// Shared owner fencing and idempotent creation for private and example readers.
// Turn streams and composer restoration stay with their existing controllers.
export function useConversationHistory(options: {
  scope: string
  api(): ConversationApi | null
  store(): KeyValue
  current(): Conversation | null
  disabled(): boolean
  windowSeconds(): number
  activate(next: Conversation, owns: () => boolean): Promise<void>
}) {
  const latest = useRef(options); latest.current = options
  const action = useRef(0), listing = useRef(0), writing = useRef(false)
  const [items, setItems] = useState<ConversationSummary[]>([])
  const [busy, setBusy] = useState(false), [error, setError] = useState("")
  const [loading, setLoading] = useState(false)
  const [cursor, setCursor] = useState<string | null>(null)
  const selectedKey = () => `vidlens.conversation:${latest.current.scope}`
  function preferredID() { try { return latest.current.store().getItem(selectedKey()) } catch { return null } }
  function remember(next: Conversation) { try { latest.current.store().setItem(selectedKey(), next.id) } catch { /* Selection persistence is optional. */ } }
  function forgetSelection() { try { latest.current.store().removeItem(selectedKey()) } catch { /* Reads can still recover through the video. */ } }
  function reset() { action.current++; listing.current++; writing.current = false; setItems([]); setCursor(null); setBusy(false); setLoading(false); setError("") }
  async function refresh(append = false, requestID = latest.current.current()?.request_id) {
    const api = latest.current.api()
    if (!api || !requestID) return
    const scope = latest.current.scope, seq = ++listing.current
    setError(""); setLoading(true)
    try {
      const page = await api.conversations(requestID, append ? cursor : null)
      if (scope !== latest.current.scope || seq !== listing.current) return
      if (page.items.some(item => item.request_id !== requestID)) throw new Error("会话列表与当前视频不一致。")
      setItems(previous => append ? [...new Map([...previous, ...page.items].map(item => [item.id, item])).values()] : page.items)
      setCursor(page.has_more ? page.next_before : null)
    } catch (cause) { if (scope === latest.current.scope && seq === listing.current) setError(errorText(cause)) }
    finally { if (scope === latest.current.scope && seq === listing.current) setLoading(false) }
  }
  async function choose(id?: string) {
    const current = latest.current.current(), api = latest.current.api()
    if (!current || !api || writing.current || latest.current.disabled()) return
    const scope = latest.current.scope, seq = ++action.current
    const owns = () => scope === latest.current.scope && seq === action.current
    const slot = `conversation-new:${current.request_id}`
    writing.current = true; setBusy(true); setError("")
    try {
      let next: Conversation
      if (id) next = (await api.getConversation(id)).conversation
      else {
        const store = latest.current.store(), pending = pendingWrite(store, slot)
        const key = pending && canReplay(pending, Date.now()) ? pending.key : newIdempotencyKey()
        rememberWrite(store, { slot, key, path: `/api/v1/requests/${current.request_id}/conversations`, body: "{}", startedAt: Date.now(), expiresAt: Date.now() + latest.current.windowSeconds() * 1000 })
        const accepted = await api.createConversation(current.request_id, key)
        if (pendingWrite(store, slot)?.key === key) forgetWrite(store, slot)
        next = accepted.body.conversation
      }
      if (!owns()) return
      if (next.request_id !== current.request_id || next.article_id !== current.article_id) throw new Error("这条会话不属于当前视频。")
      await latest.current.activate(next, owns)
      if (!owns()) return
      remember(next)
      await refresh(false, next.request_id)
    } catch (cause) {
      if (!owns()) return
      if (!id && isApiError(cause) && !replayable(cause)) forgetWrite(latest.current.store(), slot)
      setError(errorText(cause))
    } finally { if (owns()) { writing.current = false; setBusy(false) } }
  }
  const controls: ConversationHistoryControls = { items, busy, loading, error, hasMore: cursor !== null, refresh: () => { void refresh() }, more: () => { void refresh(true) }, create: () => { void choose() }, select: id => { void choose(id) } }
  useEffect(() => () => { action.current++; listing.current++; writing.current = false }, [])
  return { controls, preferredID, remember, forgetSelection, reset, refresh, isBusy: () => writing.current }
}
