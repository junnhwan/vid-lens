import { createElement } from "react"
import { act, create, type ReactTestRenderer } from "react-test-renderer"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { memoryStore, rememberWrite } from "../api/writes"
import type { Conversation, Frame, RequestView, TurnView } from "../contract/types"
import { useVisitor } from "./useVisitor"

const api = vi.hoisted(() => ({
  session: vi.fn(), getRequest: vi.fn(), createRequest: vi.fn(), cancelRequest: vi.fn(), retryRequest: vi.fn(),
  conversation: vi.fn(), turns: vi.fn(), createTurn: vi.fn(), cancelTurn: vi.fn(), retryTurn: vi.fn(),
  frame: vi.fn(), article: vi.fn(), models: vi.fn(), activity: vi.fn(), closeEvents: vi.fn(),
}))
vi.mock("../api/runtime", () => ({
  createRuntime: async () => ({ api, events: () => ({ readyState: 1, onerror: null, addEventListener() {}, close: api.closeEvents }) }),
  wantsMock: () => false,
}))
vi.mock("../api/http", () => ({ HttpApi: class { models = api.models } }))

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (reason: unknown) => void
  const promise = new Promise<T>((done, fail) => { resolve = done; reject = fail })
  return { promise, resolve, reject }
}
function activity() { return {current_step_id:null,recent_steps:[],has_more:false,history_url:"",history_status:"available",history_expires_at:null} }
const createdAt = "2026-10-18T10:00:00Z"
function request(id: string, status = "generating", revision = 1): RequestView {
  return {
    schema_version: 1, source: null, execution: null, failure: null,
    request: { id, status, revision, input_url:"https://youtube.com/watch?v=abcdefghijk",reuse:"none",retry_of_id:null,article_id:null,created_at:createdAt,updated_at:createdAt },
    actions: { can_read_article: false,can_cancel:status==="generating",can_retry:false },
    activity: activity(),
    stream: { cursor: `${id}-${revision}`, revision, run_id: `run-${id}`, seq: 1 },
  }
}
function conversation(id: string): Conversation {
  return { id: `conversation-${id}`, request_id: id, article_id:`article-${id}`,snapshot_id:`snapshot-${id}`,created_at:createdAt,active_turn_id: null, last_turn_id: null }
}
function turn(id: string, status = "completed"): TurnView {
  return {
    schema_version: 1, execution: null, failure: null,
    turn: { id: `turn-${id}`, conversation_id: `conversation-${id}`, request_id:id,article_id:`article-${id}`,snapshot_id:`snapshot-${id}`,revision:1,turn_no: 1, status, question: `question ${id}`,context:{frame_id:null,passage:null},retry_of_id:null,answer:null,created_at:createdAt,updated_at:createdAt },
    actions: { can_cancel: status === "running",can_retry:false,can_submit_next:true }, activity: activity(),
    stream: { cursor: `turn-${id}-1`, revision: 1, run_id: `answer-${id}`, seq: 1 },
  }
}
let view: ReturnType<typeof useVisitor>
let renderer: ReactTestRenderer | undefined
const navigate = vi.fn()
function Visitor({ id }: { id?: string }) { view = useVisitor(id, navigate); return null }
async function open(id?: string) {
  await act(async () => {
    if (renderer) renderer.update(createElement(Visitor, { id }))
    else renderer = create(createElement(Visitor, { id }))
  })
}

beforeEach(() => {
  vi.resetAllMocks()
  vi.stubGlobal("location", { origin: "http://visitor.test", search: "" })
  vi.stubGlobal("document", { getElementById: () => null, activeElement: null })
  vi.stubGlobal("sessionStorage", memoryStore())
  api.models.mockResolvedValue({ models: [] })
  api.session.mockResolvedValue({ schema_version: 1, csrf_token: "test", capabilities: {} })
  api.getRequest.mockImplementation(async (id: string) => request(id))
  api.conversation.mockImplementation(async (id: string) => ({ conversation: conversation(id) }))
  api.turns.mockResolvedValue({ items: [] })
})
afterEach(async () => {
  await act(async () => renderer?.unmount())
  renderer = undefined
  vi.unstubAllGlobals()
})

describe("visitor request ownership", () => {
  it("sends all selected paragraphs with their own anchors", async () => {
    api.createTurn.mockResolvedValue({ body: turn("A", "queued") })
    await open("A")
    await act(async () => view.attachPassage({ quote: "第一段\n\n第二段", passages: [{ sectionId: "one", blockId: "a", quote: "第一段" }, { sectionId: "two", blockId: "b", quote: "第二段" }] }))
    await act(async () => view.setDraft("解释它们的联系"))
    await act(async () => view.send())
    expect(api.createTurn).toHaveBeenCalledTimes(1)
    expect(api.createTurn.mock.calls[0][2].context).toEqual({ frame_id: null, passage: null, passages: [{ section_id: "one", block_id: "a", quote: "第一段" }, { section_id: "two", block_id: "b", quote: "第二段" }] })
    expect(view.passageChip).toBeNull()
  })

  it("checks the combined selection length again when sending", async () => {
    api.session.mockResolvedValue({ schema_version: 1, csrf_token: "test", capabilities: { selection_chars_max: 6 } })
    await open("A")
    await act(async () => view.attachPassage({ quote: "第一段\n\n第二段", passages: [{ sectionId: "one", blockId: "a", quote: "第一段" }, { sectionId: "two", blockId: "b", quote: "第二段" }] }))
    await act(async () => view.setDraft("解释它们的联系"))
    await act(async () => view.send())
    expect(api.createTurn).not.toHaveBeenCalled()
    expect(view.qaError).toBe("选中文字合计超过了可选长度。")
    expect(view.passageChip?.passages).toHaveLength(2)
  })

  it("still applies cancellation to the current request", async () => {
    api.cancelRequest.mockResolvedValueOnce(request("A", "canceled", 2))
    await open("A")
    await act(async () => view.stop())
    expect(view.request?.request.status).toBe("canceled")
  })

  it("still replays a pending creation after boot", async () => {
    rememberWrite(sessionStorage, {slot:"request:create",key:"pending-key",path:"/api/v1/requests",body:JSON.stringify({video_url:"https://youtube.com/watch?v=abcdefghijk",regenerate:true}),startedAt:Date.now(),expiresAt:Date.now()+60000})
    api.createRequest.mockResolvedValueOnce({body:{request:{id:"replayed"}}})
    await open()
    expect(api.createRequest).toHaveBeenCalledWith("https://youtube.com/watch?v=abcdefghijk", "pending-key", { regenerate: true })
    expect(navigate).toHaveBeenCalledWith("/r/replayed")
    expect(view.submitting).toBe(false)
  })

  it("submits the chosen generation mode and returns to reuse after acceptance", async () => {
    api.models.mockResolvedValue({ default_model_id: "model", models: [{ id: "model", revision_id: "rev", display_name: "Model", brand_id: "generic" }] })
    api.session.mockResolvedValue({ schema_version: 1, csrf_token: "test", capabilities: { accepting_requests: true } })
    api.createRequest.mockResolvedValueOnce({body:{request:{id:"fresh"}}})
    await open()
    expect(view.regenerate).toBe(false)
    await act(async () => { view.setLink("https://youtube.com/watch?v=abcdefghijk"); view.setRegenerate(true) })
    await act(async () => view.submit())
    expect(api.createRequest).toHaveBeenCalledWith("https://youtube.com/watch?v=abcdefghijk", expect.any(String), { detail_level: "standard", model_id: "model", model_revision_id: "rev", regenerate: true })
    expect(navigate).toHaveBeenCalledWith("/r/fresh")
    expect(view.regenerate).toBe(false)
  })

  it("drops a conversation response after navigating to another request", async () => {
    const pending = deferred<{ conversation: Conversation }>()
    api.conversation.mockReturnValueOnce(pending.promise)
    await open("A")
    await act(async () => view.toggleQa())
    await open("B")
    await act(async () => pending.resolve({ conversation: conversation("A") }))
    expect(api.turns).not.toHaveBeenCalled()
    expect(view.request?.request.id).toBe("B")
    expect(view.turns).toEqual([])
    expect(view.qaError).toBe("")
  })

  it("drops a delayed turn page after its conversation was loaded", async () => {
    const pending = deferred<{ items: TurnView[] }>()
    api.turns.mockReturnValueOnce(pending.promise)
    await open("A")
    await act(async () => view.toggleQa())
    await open("B")
    await act(async () => pending.resolve({ items: [turn("A")] }))
    expect(view.turns).toEqual([])
    expect(view.request?.request.id).toBe("B")
  })

  it("ignores a late conversation error", async () => {
    const pending = deferred<{ conversation: Conversation }>()
    api.conversation.mockReturnValueOnce(pending.promise)
    await open("A")
    await act(async () => view.toggleQa())
    await open("B")
    await act(async () => pending.reject(new Error("late conversation error")))
    expect(view.qaError).toBe("")
  })

  it("does not install a turn page belonging to a different conversation", async () => {
    api.turns.mockResolvedValueOnce({items:[turn("wrong")]})
    await open("A")
    await act(async () => view.toggleQa())
    expect(view.turns).toEqual([])
  })

  it.each(["B", "A"])("ignores an old cancellation after A to B to %s navigation", async (destination) => {
    const pending = deferred<RequestView>()
    api.cancelRequest.mockReturnValueOnce(pending.promise)
    await open("A")
    await act(async () => view.stop())
    await open("B")
    if (destination === "A") await open("A")
    await act(async () => pending.resolve(request("A", "canceled", 2)))
    expect(view.request?.request.id).toBe(destination)
    expect(view.request?.request.status).toBe("generating")
  })

  it("does not install an HTTP snapshot for a different resource", async () => {
    api.getRequest.mockResolvedValueOnce(request("wrong"))
    await open("B")
    expect(view.request).toBeNull()
  })

  it("does not put a late cancellation error on a different article", async () => {
    const pending = deferred<RequestView>()
    api.cancelRequest.mockReturnValueOnce(pending.promise)
    await open("A")
    await act(async () => view.stop())
    await open("B")
    await act(async () => pending.reject(new Error("late error")))
    expect(view.articleError).toBe("")
  })

  it("keeps a late answer response out of the next request", async () => {
    const pending = deferred<{ body: TurnView }>()
    api.createTurn.mockReturnValueOnce(pending.promise)
    await open("A")
    await act(async () => view.setDraft("question about A"))
    await act(async () => view.send())
    expect(api.createTurn).toHaveBeenCalledOnce()
    await open("B")
    await act(async () => pending.resolve({ body: turn("A", "running") }))
    expect(view.turns).toEqual([])
    expect(view.qaBusy).toBe(false)
  })

  it("does not navigate away when an old retry is accepted", async () => {
    const pending = deferred<{ body: { request: { id: string } } }>()
    api.retryRequest.mockReturnValueOnce(pending.promise)
    await open("A")
    await act(async () => view.retry())
    await open("B")
    await act(async () => pending.resolve({ body: { request: { id: "retry-A" } } }))
    expect(navigate).not.toHaveBeenCalled()
  })

  it("does not add an old frame to another request", async () => {
    const pending = deferred<Frame>()
    const frame = {id:"old-frame",availability:"ready",urls:{full:"/full",thumbnail:"/thumb"}} as Frame
    api.frame.mockReturnValueOnce(pending.promise)
    await open("A")
    await act(async () => view.refreshFrame(frame))
    await open("B")
    await act(async () => pending.resolve(frame))
    expect(view.frames.size).toBe(0)
  })
  it("restores older screenshots from the summary and applies refreshed availability", async () => {
    const frame = {id:"earlier",availability:"ready",urls:{full:"/full",thumbnail:"/thumb"}} as Frame
    api.getRequest.mockResolvedValueOnce({...request("A"), activity:{...activity(),frames:[frame]}})
    await open("A")
    expect(view.frames.get("earlier")).toEqual(frame)
    const expired = {...frame,availability:"expired",urls:{full:null,thumbnail:null}}
    api.frame.mockResolvedValueOnce(expired)
    await act(async () => { await view.refreshFrame(frame) })
    expect(view.request?.activity.frames?.[0]).toEqual(expired)
    await open("B")
    expect(view.frames.size).toBe(0)
  })
  it("closes each request and answer subscription on panel close and unmount", async () => {
    api.conversation.mockImplementation(async (id: string) => ({conversation: {...conversation(id), active_turn_id: `turn-${id}`}}))
    api.turns.mockResolvedValue({items:[turn("A", "running")]})
    await open("A")
    await act(async () => view.toggleQa())
    await act(async () => view.closeQa())
    expect(api.closeEvents).toHaveBeenCalledTimes(1)
    await act(async () => renderer?.unmount())
    renderer = undefined
    expect(api.closeEvents).toHaveBeenCalledTimes(2)
  })

  it("can reload history after returning while an old load is pending", async () => {
    const pending = deferred<{items:never[],has_more:boolean}>()
    api.getRequest.mockImplementation(async (id: string) => ({...request(id),activity:{...activity(),history_url:`/history/${id}`}}))
    api.activity.mockReturnValueOnce(pending.promise).mockResolvedValue({items:[],has_more:false})
    await open("A")
    await act(async () => view.loadHistory())
    expect(view.historyState.A).toBe("正在加载完整过程…")
    await open("B")
    await open("A")
    await act(async () => view.loadHistory())
    expect(api.activity).toHaveBeenCalledTimes(2)
    expect(view.historyState.A).toBe("")
    await act(async () => pending.resolve({items:[],has_more:false}))
    expect(view.historyState.A).toBe("")
  })

})
