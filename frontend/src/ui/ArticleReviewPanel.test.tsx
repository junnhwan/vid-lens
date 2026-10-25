import { act, create } from "react-test-renderer"
import { afterEach, describe, expect, it, vi } from "vitest"
import { memoryStore } from "../api/writes"
import { ArticleReviewPanel } from "./ArticleReviewPanel"

afterEach(() => vi.unstubAllGlobals())

describe("optional article review", () => {
  it("shows a failed review and starts another only after the reader clicks", async () => {
    vi.stubGlobal("document", { hidden: false })
    vi.stubGlobal("sessionStorage", memoryStore())
    const calls: { method?: string; key?: string }[] = []
    let status = "incomplete"
    vi.stubGlobal("fetch", vi.fn(async (_path: string, options: RequestInit) => {
      calls.push({ method: options.method, key: (options.headers as Record<string, string>)["idempotency-key"] })
      if (options.method === "POST") status = "queued"
      return new Response(JSON.stringify({ review: { id: "review", article_id: "article", trigger: "manual", status, result: null } }), { status: 200 })
    }))
    let tree!: ReturnType<typeof create>
    await act(async () => { tree = create(<ArticleReviewPanel requestId="request" csrf="csrf" onJump={() => {}} />) })
    expect(calls.filter((call) => call.method === "POST")).toHaveLength(0)
    expect(JSON.stringify(tree.toJSON())).toContain("文章可正常阅读和导出")
    const button = tree.root.findByType("button")
    expect(button.children.join("")).toBe("再次审核")
    await act(async () => { await button.props.onClick() })
    const writes = calls.filter((call) => call.method === "POST")
    expect(writes).toHaveLength(1)
    expect(writes[0].key).toMatch(/^[a-f0-9]{32}$/)
    expect(tree.root.findByType("button").props.disabled).toBe(true)
    await act(async () => tree.unmount())
  })
})
