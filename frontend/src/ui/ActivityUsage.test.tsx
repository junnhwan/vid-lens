import { describe, expect, it } from "vitest"
import { renderToStaticMarkup } from "react-dom/server"
import { ActivityList } from "./ActivityList"
import type { ActivityStep } from "../contract/types"

const step = (id: string, kind: string, call: number, submission?: number, usage?: ActivityStep["usage"]): ActivityStep => ({
  id,
  kind,
  model_call: call,
  submission,
  usage,
  run_id: "run",
  segment_no: 0,
  first_seq: 1,
  last_seq: 1,
  phase: "compose",
  title: kind === "model_response" ? "模型调用" : `模型调用 ${call} · 发送 ${submission}`,
  purpose: null,
  status: "completed",
  source_range: null,
  frame_ids: [],
  frames: [],
  outcome: { message: "已完成。", evidence_status: "not_assessed" },
  retry_of_step_id: null,
  started_at: null,
  finished_at: null,
})

describe("model token usage", () => {
  it("shows the call total and each physical send separately", () => {
    const steps = [
      step("call", "model_response", 1, undefined, { input: 21649, output: 3102 }),
      step("send1", "model_submission", 1, 1, { input: 12412, output: 1806 }),
      step("send2", "model_submission", 1, 2, { input: 9237, output: 1296 }),
    ]
    const html = renderToStaticMarkup(<ActivityList steps={steps} open detailed resourceStatus="completed" onOpenFrame={() => {}} onBroken={() => {}} />)
    expect(html).toContain("第 1 次模型调用 · 发送 2 次")
    expect(html).toContain("用量")
    expect(html).toContain("入 <b>21.6k</b> · 出 <b>3.1k</b>")
    expect(html).toContain("入 <b>12.4k</b> · 出 <b>1.8k</b>")
    expect(html).toContain("入 <b>9.2k</b> · 出 <b>1.3k</b>")
    expect(html).toContain("输入 21,649 tokens，输出 3,102 tokens")
  })

  it("leaves unreported usage out instead of rendering a zero", () => {
    const html = renderToStaticMarkup(<ActivityList steps={[step("call", "model_response", 1)]} open detailed resourceStatus="completed" onOpenFrame={() => {}} onBroken={() => {}} />)
    expect(html).not.toContain("用量")
    expect(html).not.toContain("入 <b>")
  })
})
