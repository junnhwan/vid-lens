import { createElement } from "react"
import { act, create, type ReactTestRenderer } from "react-test-renderer"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { memoryStore } from "../api/writes"
import type { PassageChoice } from "../contract/text"
import { useExampleQuestions } from "./useExampleQuestions"

const api = vi.hoisted(() => ({ session: vi.fn(), exampleConversation: vi.fn(), turns: vi.fn(), createTurn: vi.fn() }))
vi.mock("../api/http", () => ({ HttpApi: class { session = api.session; exampleConversation = api.exampleConversation; turns = api.turns; createTurn = api.createTurn } }))
let view: ReturnType<typeof useExampleQuestions>
let renderer: ReactTestRenderer | undefined
function Questions() { view = useExampleQuestions("article", "文章", "章节"); return null }
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
