import { describe, expect, it } from "vitest"
import type { ActivityStep } from "../contract/types"
import { stepPresentation } from "./labels"

describe("terminal activity", () => {
  const step = { status: "running" } as ActivityStep

  it("stops presenting unfinished work as active after failure", () => {
    expect(stepPresentation(step, "failed")).toEqual({ status: "stopped", outcome: "处理失败时尚未完成" })
  })

  it("keeps completed work completed after a later failure", () => {
    expect(stepPresentation({ ...step, status: "completed" }, "failed").status).toBe("done")
    expect(stepPresentation(step, "waiting_source").status).toBe("active")
  })
})
