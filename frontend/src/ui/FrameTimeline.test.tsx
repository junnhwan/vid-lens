import { act, create, type ReactTestRenderer } from "react-test-renderer"
import { afterEach, beforeEach, expect, it, vi } from "vitest"
import type { Frame } from "../contract/types"
import { FrameTimeline } from "./FrameTimeline"

vi.mock("react-dom", () => ({ createPortal: (children: unknown) => children }))
const frame = (id: string, time_ms: number): Frame => ({ id, time_ms, requested_time_ms: time_ms, snapshot_id: "s", width: 1280, height: 720, mime_type: "image/jpeg", caption: "", crop: null, replaces_frame_id: null, availability: "ready", unavailable_reason: null, expires_at: null, urls: { full: `/${id}.jpg`, thumbnail: `/${id}-thumb.jpg` } })
const frames = [frame("late", 900000), frame("early", 234067)]
let renderer: ReactTestRenderer
const listeners = new Map<string, (event: unknown) => void>()
beforeEach(() => {
  vi.useFakeTimers()
  vi.stubGlobal("document", { body: {} })
  vi.stubGlobal("window", { innerWidth: 1440, innerHeight: 900, addEventListener: (name: string, cb: (event: unknown) => void) => listeners.set(name, cb), removeEventListener: (name: string) => listeners.delete(name) })
})
afterEach(() => { act(() => renderer?.unmount()); vi.unstubAllGlobals(); vi.useRealTimers() })
function mount(onOpenFrame = vi.fn(), onBroken = vi.fn()) {
  act(() => { renderer = create(<FrameTimeline duration={1872471} ranges={[]} frames={frames} activeRange={null} progress={null} stopped={false} live latestId="late" onOpenFrame={onOpenFrame} onBroken={onBroken} />) })
  return { onOpenFrame, onBroken, pins: renderer.root.findAllByProps({ className: "ruler-pin" }) }
}
const origin = { getBoundingClientRect: () => ({ left: 1200, top: 500, bottom: 518, width: 18 }) }
it("orders timestamps and previews the actual image on hover, retaining it across the pointer gap", () => {
  const { pins } = mount()
  expect(pins[0].props["aria-label"]).toContain("03:54")
  act(() => pins[0].props.onPointerEnter({ pointerType: "mouse", currentTarget: origin }))
  expect(renderer.root.findByType("img").props.src).toBe("/early-thumb.jpg")
  expect(renderer.root.findByType("time").children).toEqual(["03:54"])
  act(() => { pins[0].props.onPointerLeave(); vi.advanceTimersByTime(100) })
  act(() => renderer.root.findByProps({ className: "frame-peek" }).props.onPointerEnter())
  act(() => { vi.advanceTimersByTime(200) })
  expect(renderer.root.findAllByType("img")).toHaveLength(1)
  act(() => listeners.get("keydown")?.({ key: "Escape" }))
  expect(renderer.root.findAllByType("img")).toHaveLength(0)
})
it("supports keyboard previews, image errors and opening the correct full-size frame", () => {
  const { pins, onOpenFrame, onBroken } = mount()
  act(() => pins[1].props.onFocus({ currentTarget: origin }))
  Object.assign(document, { activeElement: origin })
  act(() => listeners.get("scroll")?.({}))
  expect(renderer.root.findAllByType("img")).toHaveLength(1)
  act(() => renderer.root.findByType("img").props.onError())
  expect(onBroken).toHaveBeenCalledWith(frames[0])
  act(() => pins[1].props.onClick({ currentTarget: origin }))
  expect(onOpenFrame).toHaveBeenCalledWith(frames[0], origin)
  expect(renderer.root.findAllByProps({ className: "frame-peek" })).toHaveLength(0)
})
