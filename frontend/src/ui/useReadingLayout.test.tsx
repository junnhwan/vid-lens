import { act, create, type ReactTestRenderer } from "react-test-renderer"
import { afterEach, beforeEach, expect, it, vi } from "vitest"
import { useReadingLayout } from "./useReadingLayout"

type Layout = "open" | "stage"
let layout: Layout
let reduced: boolean
let visualOffset: number
let renderer: ReactTestRenderer
let controls: ReturnType<typeof useReadingLayout>
const log = { scrollTop: 240, scrollHeight: 1800, clientHeight: 700 }
const animations: { cancel: ReturnType<typeof vi.fn>; onfinish: (() => void) | null }[] = []
const rect = (left: number, top: number, width: number): DOMRect => ({ x: left, y: top, left, top, width, height: 700, right: left + width, bottom: top + 700, toJSON: () => ({ left, top, width }) })
const anchor = { isConnected: true, getBoundingClientRect: () => rect(400, 1500 - window.scrollY, 640) }
const animate = vi.fn(() => {
  const animation = { cancel: vi.fn(), onfinish: null }
  animations.push(animation)
  return animation
})
const article = { current: { getBoundingClientRect: () => rect(200, 96 - window.scrollY, 952), querySelectorAll: () => [anchor], animate, scrollTop: 0 } as unknown as HTMLDivElement }
const qa = { current: { getBoundingClientRect: () => rect((layout === "stage" ? 560 : 1200) + visualOffset, layout === "stage" ? 64 : 72, layout === "stage" ? 768 : 468), querySelector: () => log, animate } as unknown as HTMLDivElement }

function Harness() { controls = useReadingLayout(article, qa, layout, true, "article"); return null }
function change(next: Layout) { layout = next; visualOffset = 0; act(() => renderer.update(<Harness />)) }
beforeEach(() => {
  layout = "open"; reduced = false; visualOffset = 0; animations.length = 0; animate.mockClear()
  log.scrollTop = 240
  article.current.scrollTop = 0
  vi.stubGlobal("window", { scrollY: 1400, scrollTo: vi.fn(({ top }: ScrollToOptions) => { window.scrollY = top ?? 0 }), matchMedia: () => ({ matches: reduced }) })
  vi.stubGlobal("document", { documentElement: {} })
  vi.stubGlobal("getComputedStyle", () => ({ getPropertyValue: () => "cubic-bezier(0.32, 0.72, 0, 1)" }))
  act(() => { renderer = create(<Harness />) })
})
afterEach(() => { act(() => renderer.unmount()); vi.unstubAllGlobals() })

it("restores the original reading anchor and the conversation scroll when returning from question mode", () => {
  controls.prepare()
  change("stage")
  expect(window.scrollY).toBe(0)
  expect(log.scrollTop).toBe(240)
  controls.restorePaper()
  expect(article.current.scrollTop).toBe(1372)
  controls.rememberPaper()
  controls.prepare()
  change("open")
  expect(window.scrollY).toBe(1400)
  expect(anchor.getBoundingClientRect().top).toBe(100)
  expect(log.scrollTop).toBe(240)
})

it("keeps a conversation at the bottom after changing panel height", () => {
  log.scrollTop = 1100
  controls.prepare()
  log.clientHeight = 850
  change("stage")
  expect(log.scrollTop).toBe(log.scrollHeight)
  log.clientHeight = 700
})

it("starts an interrupted transition at the panel's current visible position", () => {
  controls.prepare()
  change("stage")
  visualOffset = 260
  controls.prepare()
  change("open")
  expect(animations[0].cancel).toHaveBeenCalled()
  expect(animate).toHaveBeenLastCalledWith([{ transform: "translate(-380px, -8px)" }, { transform: "none" }], expect.objectContaining({ duration: 280 }))
})

it.each(["reduced motion", "keyboard"])("restores layout without positional animation for %s", (mode) => {
  reduced = mode === "reduced motion"
  controls.prepare(mode === "keyboard")
  change("stage")
  expect(animate).not.toHaveBeenCalled()
  controls.prepare(mode === "keyboard")
  change("open")
  expect(window.scrollY).toBe(1400)
  expect(animate).not.toHaveBeenCalled()
})
