import { describe, expect, it } from "vitest"
import { ApiError } from "./error"
import { createMockRuntime } from "./mock"

async function finishArticle() {
  const runtime = createMockRuntime({ auto: false })
  const created = await runtime.api.createRequest("https://www.bilibili.com/video/BV1mockvideo?p=1", "key-article")
  for (let i = 0; i < 5; i += 1) runtime.advance()
  const view = await runtime.api.getRequest(created.body.request.id)
  return { runtime, view }
}

describe("mock visitor api", () => {
  it("replays the same submission and rejects a conflicting body", async () => {
    const runtime = createMockRuntime({ auto: false })
    const first = await runtime.api.createRequest("https://www.bilibili.com/video/BV1mockvideo", "same")
    const again = await runtime.api.createRequest("https://www.bilibili.com/video/BV1mockvideo", "same")
    expect(again.replayed).toBe(true)
    expect(again.body.request.id).toBe(first.body.request.id)
    await expect(runtime.api.createRequest("https://www.youtube.com/watch?v=abcdefghijk", "same")).rejects.toMatchObject({ code: "IDEMPOTENCY_CONFLICT" })
    runtime.dispose()
  })

  it("publishes an article only after the scripted run completes", async () => {
    const runtime = createMockRuntime({ auto: false })
    const created = await runtime.api.createRequest("https://www.bilibili.com/video/BV1mockvideo?p=1", "once")
    await expect(runtime.api.article(created.body.request.id)).rejects.toBeInstanceOf(ApiError)
    runtime.advance()
    const transcribing = await runtime.api.getRequest(created.body.request.id)
    expect(transcribing.request.status).toBe("waiting_source")
    expect(transcribing.activity.recent_steps[0]?.title).toBe("正在转写音频（18:00 / 35:42）")
    runtime.advance()
    expect((await runtime.api.getRequest(created.body.request.id)).request.status).toBe("generating")
    runtime.advance()
    runtime.advance()
    runtime.advance()
    const done = await runtime.api.getRequest(created.body.request.id)
    expect(done.request.status).toBe("completed")
    expect(done.actions.can_read_article).toBe(true)
    const article = await runtime.api.article(done.request.id)
    expect(article.article.title).toBe("把重复操作变成可复用流程")
    expect(article.frames.map((frame) => frame.id)).toEqual(["frm_closed", "frm_open"])
    runtime.dispose()
  })

  it("cancels the active run without creating another request", async () => {
    const runtime = createMockRuntime({ auto: false })
    const created = await runtime.api.createRequest("https://www.bilibili.com/video/BV1mockvideo", "cancel")
    runtime.advance()
    const stopped = await runtime.api.cancelRequest(created.body.request.id)
    expect(stopped.request.status).toBe("canceled")
    expect(stopped.actions.can_retry).toBe(true)
    const again = await runtime.api.cancelRequest(created.body.request.id)
    expect(again.request.id).toBe(stopped.request.id)
    runtime.dispose()
  })

  it("guards turn order, context, and answers from the attached frame", async () => {
    const { runtime, view } = await finishArticle()
    const conversation = await runtime.api.conversation(view.request.id)
    const input = { question: "这张画面在说明什么？", expected_last_turn_id: null, context: { frame_id: "frm_closed", passage: null } }
    const first = await runtime.api.createTurn(conversation.conversation.id, "turn-1", input)
    await expect(runtime.api.createTurn(conversation.conversation.id, "turn-2", { ...input, question: "另一个问题", expected_last_turn_id: first.body.turn.id })).rejects.toMatchObject({ code: "TURN_IN_PROGRESS" })
    await expect(runtime.api.createTurn(conversation.conversation.id, "turn-1", { ...input, question: "另一个问题" })).rejects.toMatchObject({ code: "IDEMPOTENCY_CONFLICT" })
    for (let i = 0; i < 3; i += 1) runtime.advance()
    const answered = await runtime.api.getTurn(conversation.conversation.id, first.body.turn.id)
    expect(answered.turn.status).toBe("completed")
    expect(answered.turn.answer?.blocks[0]?.content?.[0]?.text).toContain("编辑状态")
    const open = await runtime.api.createTurn(conversation.conversation.id, "turn-open", {
      question: "展开之后呢？",
      expected_last_turn_id: answered.turn.id,
      context: { frame_id: "frm_open", passage: null },
    })
    for (let i = 0; i < 3; i += 1) runtime.advance()
    const second = await runtime.api.getTurn(conversation.conversation.id, open.body.turn.id)
    expect(second.turn.answer?.blocks[0]?.content?.[0]?.text).toContain("展开后的菜单")
    await expect(runtime.api.createTurn(conversation.conversation.id, "bad-quote", {
      question: "这段呢？",
      expected_last_turn_id: second.turn.id,
      context: { frame_id: null, passage: { section_id: "scene", block_id: "blk_scene", quote: "这段不在正文里" } },
    })).rejects.toMatchObject({ status: 422, code: "INVALID_CONTEXT" })
    await expect(runtime.api.createTurn(conversation.conversation.id, "stale", {
      question: "再问一次",
      expected_last_turn_id: first.body.turn.id,
      context: { frame_id: null, passage: null },
    })).rejects.toMatchObject({ code: "TURN_HISTORY_CHANGED" })
    runtime.dispose()
  })

  it("persists every paragraph in a multi-part question context and rejects forged pieces", async () => {
    const { runtime, view } = await finishArticle()
    const conversation = await runtime.api.conversation(view.request.id)
    const article = (await runtime.api.article(view.request.id)).article
    const first = article.sections[0].blocks[0].content?.[0].text ?? ""
    const context = { frame_id: null, passage: null, passages: [
      { section_id: "scene", block_id: "blk_scene", quote: first },
      { section_id: "steps", block_id: "blk_steps", quote: "配置从菜单开始。" },
    ] }
    await expect(runtime.api.createTurn(conversation.conversation.id, "forged", { question: "说明它们的联系", expected_last_turn_id: null, context: { ...context, passages: [context.passages[0], { ...context.passages[1], quote: "不存在的句子" }] } })).rejects.toMatchObject({ code: "INVALID_CONTEXT" })
    const accepted = await runtime.api.createTurn(conversation.conversation.id, "multi", { question: "说明它们的联系", expected_last_turn_id: null, context })
    expect(accepted.body.turn.context.passages).toEqual(context.passages)
    runtime.dispose()
  })
})
