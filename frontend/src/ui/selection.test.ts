import { describe, expect, it } from "vitest"
import { selectionPopover } from "./selection"

describe("passage question popover", () => {
  it("stays reachable when a multi-paragraph selection extends beyond the viewport", () => {
    const point = selectionPopover({ left: 300, width: 600, top: -400, bottom: 1400 }, 1200, 800)
    expect(point.top).toBeGreaterThanOrEqual(72)
    expect(point.top + 32).toBeLessThan(800)
    expect(point.left).toBeGreaterThanOrEqual(12)
    expect(point.left + 96).toBeLessThan(1200)
  })

  it("keeps the action above the selection without covering the site header", () => {
    expect(selectionPopover({ left: 300, width: 200, top: 300, bottom: 360 }, 1200, 800)).toEqual({ left: 352, top: 262 })
    expect(selectionPopover({ left: -100, width: 20, top: 68, bottom: 90 }, 1200, 800).top).toBe(98)
  })
})
