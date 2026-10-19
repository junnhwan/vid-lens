import { renderToStaticMarkup } from "react-dom/server"
import { describe, expect, it } from "vitest"
import { QueueDisplay } from "./ServiceQueue"
import type { SiteQueue } from "../contract/queue"

const queue: SiteQueue = { schema_version: 1, max_concurrent: 2, processing: 1, waiting: 3, as_of: "2026-10-18T06:00:00Z" }
describe("service queue", () => {
  it("uses the live capacity and distinguishes occupied from empty slots", () => {
    const html = renderToStaticMarkup(<QueueDisplay queue={queue} state="ready" />)
    expect(html.match(/class="queue-seat"/g)).toHaveLength(2)
    expect(html.match(/data-occupied="true"/g)).toHaveLength(1)
    expect(html).toContain("1 个进行中")
    expect(html).toContain("3 个等待中")
    expect(html).toContain("data-live=\"true\"")
  })
  it("does not present old counts or moving slots as current when updates fail", () => {
    const html = renderToStaticMarkup(<QueueDisplay queue={queue} state="error" />)
    expect(html).toContain("暂时无法更新")
    expect(html).not.toContain("1 个进行中")
    expect(html).not.toContain("暂无等待")
    expect(html).not.toContain("class=\"queue-seat\"")
    expect(html).toContain("data-live=\"false\"")
  })
  it("shows an honest loading state and follows changed capacity", () => {
    expect(renderToStaticMarkup(<QueueDisplay queue={null} state="loading" />)).toContain("正在读取状态")
    const html = renderToStaticMarkup(<QueueDisplay queue={{ ...queue, max_concurrent: 1, processing: 0, waiting: 0 }} state="ready" active={false} />)
    expect(html.match(/class="queue-seat"/g)).toHaveLength(1)
    expect(html).toContain("现在有空位")
    expect(html).toContain("暂无等待")
    expect(html).toContain("data-live=\"false\"")
  })
  it("keeps every running slot visible while a lowered limit drains", () => {
    const html = renderToStaticMarkup(<QueueDisplay queue={{ ...queue, max_concurrent: 2, processing: 10 }} state="ready" />)
    expect(html.match(/data-occupied="true"/g)).toHaveLength(10)
    expect(html).toContain("10 个进行中")
    expect(html).toContain("已开始的继续完成")
  })
})
