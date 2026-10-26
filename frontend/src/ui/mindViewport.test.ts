import { describe, expect, it } from "vitest"
import { mindScroll, mindWorld, mindZoom, minimapPoint } from "./mindViewport"

describe("mind map viewport", () => {
  const content = { width: 1000, height: 3000 }, viewport = { width: 800, height: 600 }
  it("centers a small graph without changing its 100% reading scale", () => {
    expect(mindWorld({ width: 200, height: 100 }, { width: 900, height: 600 }, 1)).toEqual({ width: 900, height: 600, x: 350, y: 250 })
  })
  it("keeps the graph point under the cursor when zooming", () => {
    const point = { x: 380, y: 1080 }, client = { x: 300, y: 200 }
    const scroll = mindScroll(point, client, content, viewport, 1.5)
    const world = mindWorld(content, viewport, 1.5)
    expect(scroll).toEqual({ x: 310, y: 1460 })
    expect(world.x + point.x * 1.5 - scroll.x).toBe(client.x)
    expect(world.y + point.y * 1.5 - scroll.y).toBe(client.y)
  })
  it("clamps panning at the graph boundaries", () => {
    expect(mindScroll({ x: -1000, y: -1000 }, { x: 400, y: 300 }, content, viewport, 1)).toEqual({ x: 0, y: 0 })
    expect(mindScroll({ x: 9999, y: 9999 }, { x: 400, y: 300 }, content, viewport, 1)).toEqual({ x: 280, y: 2480 })
  })
  it("accounts for letterboxing when navigating a tall graph from its minimap", () => {
    const center = minimapPoint({ x: 75, y: 54 }, { width: 150, height: 108 }, content)
    expect(center.x).toBeCloseTo(500)
    expect(center.y).toBeCloseTo(1500)
    expect(minimapPoint({ x: 57, y: 0 }, { width: 150, height: 108 }, content)).toEqual({ x: 0, y: 0 })
  })
  it("bounds manual zoom", () => { expect(mindZoom(.1)).toBe(.5); expect(mindZoom(3)).toBe(2); expect(mindZoom(1)).toBe(1) })
})
