import { describe, expect, it } from "vitest"
import { PHASES } from "./progress"
import { tipKindLabel, waitTips } from "./waitTips"

describe("waiting tips", () => {
  it("leads with what is happening now, then general practice", () => {
    const investigating = waitTips("investigate", "running")
    expect(investigating[0].text).toContain("回到某一帧")
    expect(investigating.slice(-1)[0].text).toBe("进度可见，本身就是一种可靠性。")

    const retrying = waitTips("compose", "retry_wait")
    expect(retrying[0].text).toContain("自动恢复不是重来")
    expect(retrying[2].text).toContain("文章是给人读的")

    const queued = waitTips("prepare_source", "queued")
    expect(queued[0].text).toContain("排队")
    expect(queued[2].text).toContain("视频先完整落地")
  })

  it("changes its opening note with the phase and falls back cleanly", () => {
    const openings = PHASES.map((phase) => waitTips(phase.id, "running")[0].text)
    expect(new Set(openings).size).toBe(PHASES.length)
    expect(waitTips(null, null)[0].text).toContain("好的 Agent")
    expect(waitTips("answer", undefined)[0].text).toContain("好的 Agent")
  })

  it("keeps every pool unique and short enough for one line", () => {
    for (const phase of PHASES) {
      for (const status of ["running", "queued", "retry_wait", "waiting_dependency"]) {
        const tips = waitTips(phase.id, status)
        expect(new Set(tips.map((tip) => tip.text)).size).toBe(tips.length)
        for (const tip of tips) {
          expect(tip.text.length).toBeLessThanOrEqual(36)
          expect(tipKindLabel[tip.kind]).toBeTruthy()
        }
      }
    }
  })
})
