import { describe, expect, it } from "vitest"
import { liveStatusText, modelActivity, openProbe, probeTiming, type ProbeClock } from "./probeStatus"

const running: ProbeClock = {
  status: "running",
  mode: "capabilities",
  created_at: "2026-10-16T08:00:00.000Z",
}
const finished: ProbeClock = {
  status: "succeeded",
  mode: "capabilities",
  created_at: "2026-10-16T07:00:00.000Z",
  updated_at: "2026-10-16T07:01:05.000Z",
}

describe("probe status across models", () => {
  const lists = {
    a: [running],
    b: [finished],
  }

  it("keeps the running model active after another model is selected", () => {
    expect(modelActivity(lists.a)?.status).toBe("running")
    expect(openProbe(lists.b, "capabilities")).toBeUndefined()
    expect(modelActivity(lists.b)).toBeUndefined()
  })

  it("shows elapsed time while a check is open and the finished duration afterward", () => {
    expect(liveStatusText(running, Date.parse("2026-10-16T08:00:18.000Z"))).toBe("正在检查 · 18 秒")
    expect(liveStatusText({ ...running, status: "queued" }, Date.parse("2026-10-16T08:01:05.000Z"))).toBe("排队中 · 1 分 05 秒")
    expect(probeTiming(finished, Date.parse("2026-10-16T09:00:00.000Z"))).toBe("1 分 05 秒")
  })
})
