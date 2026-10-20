import { describe, expect, it } from "vitest"
import { liveStatusText, modelActivity, openProbe, probeTiming, probeFailureText, probeStatusText, type ProbeClock } from "./probeStatus"

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
    expect(liveStatusText({ ...running, status: "queued" }, Date.parse("2026-10-16T08:01:05.000Z"))).toBe("准备检查 · 1 分 05 秒")
    expect(probeTiming(finished, Date.parse("2026-10-16T09:00:00.000Z"))).toBe("1 分 05 秒")
  })

  it("does not count startup delay as model execution time", () => {
    expect(probeTiming({ ...finished, started_at: "2026-10-16T07:01:00.000Z" }, 0)).toBe("5 秒")
    expect(liveStatusText({ ...running, started_at: "2026-10-16T08:01:00.000Z" }, Date.parse("2026-10-16T08:01:02.000Z"))).toBe("正在检查 · 2 秒")
  })

  it("distinguishes supplier failures from incorrect image observations", () => {
    const cooldown = { status: "failed", submissions: 1, failure_code: "PROVIDER_ACCOUNT_COOLDOWN" }
    expect(probeStatusText(cooldown)).toBe("供应商调用未完成")
    expect(probeFailureText(cooldown)).toContain("账号正在冷却")
    expect(probeFailureText({ ...cooldown, failure_code: "PROVIDER_MODEL_UNAVAILABLE" })).toContain("没有这个模型的可用通道")
    const wrong = { ...cooldown, failure_code: "CAPABILITY_OBSERVATION_MISMATCH" }
    expect(probeStatusText(wrong)).toBe("检查失败")
    expect(probeFailureText(wrong)).toContain("与图片不符")
  })

  it("does not recommend resubmitting an uncertain paid request", () => {
    expect(probeFailureText({ status: "unknown", submissions: 1, failure_code: "PROBE_EXECUTION_TIMEOUT" })).toContain("不会自动重发")
    expect(probeFailureText({ status: "failed", submissions: 0, failure_code: "PROBE_QUEUE_TIMEOUT" })).not.toContain("可能已被上游接收")
  })
})
