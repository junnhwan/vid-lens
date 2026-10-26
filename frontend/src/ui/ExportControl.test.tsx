import { act, create } from "react-test-renderer"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { memoryStore } from "../api/writes"
import { ChoiceMenu } from "./ChoiceMenu"
import { ExportControl } from "./ExportControl"

beforeEach(() => {
  vi.stubGlobal("document", { hidden: false })
  vi.stubGlobal("sessionStorage", memoryStore())
  vi.stubGlobal("window", { clearTimeout })
  vi.stubGlobal("cancelAnimationFrame", vi.fn())
})
afterEach(() => { vi.useRealTimers(); vi.unstubAllGlobals() })

function response(format: string, status = "ready") {
  return new Response(JSON.stringify({ export: { id: format, request_id: "request", revision: 1, status, format, download_url: status === "ready" ? `/saved/${format}` : null }, failure: null }), { status: 200 })
}

describe("reader export", () => {
  it("reads on demand and only prepares an export after an explicit click", async () => {
    const fetch = vi.fn(async (_path: string, options: RequestInit) => options.method === "POST" ? response("markdown", "queued") : new Response(JSON.stringify({ export: null, failure: null }), { status: 200 }))
    vi.stubGlobal("fetch", fetch)
    let tree!: ReturnType<typeof create>
    const props = { requestId: "request", csrf: "csrf", onOpenChange: vi.fn() }
    await act(async () => { tree = create(<ExportControl {...props} open={false} />) })
    expect(fetch).not.toHaveBeenCalled()
    await act(async () => tree.update(<ExportControl {...props} open />))
    await act(async () => tree.root.findByType(ChoiceMenu).props.onChange("markdown"))
    expect(fetch.mock.calls.filter(([, options]) => options.method === "POST")).toHaveLength(0)
    const prepare = tree.root.findAllByType("button").find((button) => button.children.join("") === "准备下载 ↓")!
    await act(async () => { await prepare.props.onClick() })
    const writes = fetch.mock.calls.filter(([, options]) => options.method === "POST")
    expect(writes).toHaveLength(1)
    expect(writes[0][1].body).toBe(JSON.stringify({ format: "markdown" }))
    expect((writes[0][1].headers as Record<string, string>)["idempotency-key"]).toMatch(/^[a-f0-9]{32}$/)
    await act(async () => tree.unmount())
  })

  it("ignores an earlier format response after switching formats", async () => {
    let finishOld!: (value: Response) => void
    vi.stubGlobal("fetch", vi.fn(async (path: string) => path.includes("format=markdown_zip") ? new Promise<Response>((resolve) => { finishOld = resolve }) : response("markdown")))
    let tree!: ReturnType<typeof create>
    await act(async () => { tree = create(<ExportControl requestId="request" csrf="csrf" open onOpenChange={() => {}} />) })
    await act(async () => tree.root.findByType(ChoiceMenu).props.onChange("markdown"))
    await act(async () => finishOld(response("markdown_zip")))
    expect(tree.root.findByType("a").props.href).toBe("/saved/markdown")
    await act(async () => tree.unmount())
  })

  it("keeps a queued export disabled and reveals the finished download after polling", async () => {
    vi.useFakeTimers()
    let reads = 0
    const fetch = vi.fn(async () => response("markdown_zip", ++reads === 1 ? "queued" : "ready"))
    vi.stubGlobal("fetch", fetch)
    let tree!: ReturnType<typeof create>
    await act(async () => { tree = create(<ExportControl requestId="request" csrf="csrf" open onOpenChange={() => {}} />) })
    const preparing = tree.root.findAllByType("button").find((button) => button.children.join("") === "正在准备")!
    expect(preparing.props.disabled).toBe(true)
    await act(async () => { await vi.advanceTimersByTimeAsync(2000) })
    expect(tree.root.findByType("a").props.href).toBe("/saved/markdown_zip")
    expect(fetch).toHaveBeenCalledTimes(2)
    await act(async () => tree.unmount())
  })
})
