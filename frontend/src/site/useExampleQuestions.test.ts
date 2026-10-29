import { createElement } from "react"
import { act, create, type ReactTestRenderer } from "react-test-renderer"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { ApiError } from "../api/error"
import { memoryStore } from "../api/writes"
import type { PassageChoice } from "../contract/text"
import { useExampleQuestions } from "./useExampleQuestions"

const api = vi.hoisted(() => ({ session: vi.fn(), exampleConversation: vi.fn(), turns: vi.fn(), createTurn: vi.fn() }))
vi.mock("../api/http", () => ({ HttpApi: class { session = api.session; exampleConversation = api.exampleConversation; turns = api.turns; createTurn = api.createTurn } }))
let view: ReturnType<typeof useExampleQuestions>
let renderer: ReactTestRenderer | undefined
function Questions() { view = useExampleQuestions("article", "文章", "章节", "chapter-id"); return null }
const selection: PassageChoice = { quote: "第一段\n\n第二段", passages: [{ sectionId: "one", blockId: "a", quote: "第一段" }, { sectionId: "two", blockId: "b", quote: "第二段" }] }

beforeEach(() => {
  vi.resetAllMocks()
  vi.stubGlobal("localStorage", memoryStore())
  api.session.mockResolvedValue({ capabilities: { selection_chars_max: 1000, question_chars_max: 4000 } })
  api.exampleConversation.mockResolvedValue({ conversation: { id: "conversation", article_id: "article", last_turn_id: null, active_turn_id: null } })
  api.turns.mockResolvedValue({ items: [] })
})
afterEach(async () => {
  await act(async () => renderer?.unmount())
  renderer = undefined
  vi.unstubAllGlobals()
})

describe("public example passage questions", () => {
  it("shows the pending question immediately, sends its chapter identity once, and keeps the next draft", async () => {
    let resolve!: (value: unknown) => void
    api.createTurn.mockReturnValue(new Promise(done => { resolve = done }))
    await act(async () => { renderer = create(createElement(Questions)) })
    await act(async () => view.toggle())
    const loads = api.exampleConversation.mock.calls.length
    await act(async () => view.setDraft(view.prompts[1]))
    await act(async () => { view.send(); view.send() })
    expect(view.draft).toBe("")
    expect(view.pendingQuestion?.phase).toBe("sending")
    expect(api.createTurn).toHaveBeenCalledTimes(1)
    expect(api.createTurn.mock.calls[0][2].context.section_id).toBe("chapter-id")
    expect(api.exampleConversation).toHaveBeenCalledTimes(loads)
    await act(async () => view.setDraft("下一条草稿"))
    await act(async () => resolve({ body: {
      turn: { id: "turn", conversation_id: "conversation", turn_no: 1, status: "completed", context: { frame_id: null, passage: null }, answer: null },
      activity: { recent_steps: [], history_status: "available" }, stream: { cursor: "turn-1", revision: 1, run_id: "run", seq: 1 },
    } }))
    expect(view.pendingQuestion).toBeNull()
    expect(view.draft).toBe("下一条草稿")
  })

  it("keeps a failed question recoverable without discarding a new draft", async () => {
    let reject!: (error: Error) => void
    api.createTurn.mockReturnValue(new Promise((_, fail) => { reject = fail }))
    await act(async () => { renderer = create(createElement(Questions)) })
    await act(async () => view.setDraft("原问题"))
    await act(async () => view.send())
    await act(async () => view.setDraft("下一条草稿"))
    await act(async () => reject(new Error("network unavailable")))
    expect(view.pendingQuestion).toMatchObject({ question: "原问题", phase: "failed" })
    expect(view.draft).toBe("下一条草稿")
    await act(async () => view.restoreQuestion())
    expect(view.draft).toBe("原问题")
  })

  it("refreshes a conflicting conversation before retrying the saved question", async () => {
    api.createTurn.mockRejectedValue(new ApiError({ status: 409, code: "TURN_HISTORY_CHANGED", detail: "历史已变化" }))
    await act(async () => { renderer = create(createElement(Questions)) })
    await act(async () => view.toggle())
    api.exampleConversation.mockResolvedValue({ conversation: { id: "conversation", article_id: "article", last_turn_id: "from-other-tab", active_turn_id: null } })
    await act(async () => view.setDraft("原问题"))
    await act(async () => view.send())
    expect(api.exampleConversation).toHaveBeenCalledTimes(2)
    expect(view.pendingQuestion?.phase).toBe("failed")
    await act(async () => view.send())
    expect(api.createTurn.mock.calls[1][2].expected_last_turn_id).toBe("from-other-tab")
  })

  it("sends all selected anchors and preserves a new selection during an in-flight send", async () => {
    let resolve!: (value: unknown) => void
    api.createTurn.mockReturnValue(new Promise((done) => { resolve = done }))
    await act(async () => { renderer = create(createElement(Questions)) })
    await act(async () => view.askPassage(selection))
    await act(async () => view.setDraft("解释这些段落"))
    await act(async () => view.send())
    expect(api.createTurn.mock.calls[0][2].context).toEqual({ frame_id: null, passage: null, passages: [{ section_id: "one", block_id: "a", quote: "第一段" }, { section_id: "two", block_id: "b", quote: "第二段" }] })
    const next = { ...selection, passages: [{ ...selection.passages[0], blockId: "new" }, selection.passages[1]] }
    await act(async () => view.askPassage(next))
    await act(async () => resolve({ body: {
      turn: { id: "turn", conversation_id: "conversation", turn_no: 1, status: "completed", context: { frame_id: null, passage: null }, answer: null },
      activity: { recent_steps: [], history_status: "available" }, stream: { cursor: "turn-1", revision: 1, run_id: "run", seq: 1 },
    } }))
    expect(view.passageChip).toBe(next)
  })

  it("keeps the whole selection attached when the total exceeds the advertised limit", async () => {
    api.session.mockResolvedValue({ capabilities: { selection_chars_max: 6, question_chars_max: 4000 } })
    await act(async () => { renderer = create(createElement(Questions)) })
    await act(async () => view.askPassage(selection))
    await act(async () => view.setDraft("解释这些段落"))
    await act(async () => view.send())
    expect(api.createTurn).not.toHaveBeenCalled()
    expect(view.error).toBe("选中文字合计超过了可选长度。")
    expect(view.passageChip?.passages).toHaveLength(2)
  })
})
