import { act, create, type ReactTestRenderer } from "react-test-renderer"
import { afterEach, expect, it, vi } from "vitest"
import type { Frame } from "../contract/types"
import { ArticleGallery } from "./ArticleGallery"
import { ReadingDialog } from "./ReadingDialog"
import type { ArticleImageGroup } from "./articleImages"

const frame: Frame = { id: "adopted", snapshot_id: "s", time_ms: 12500, requested_time_ms: 12000, width: 1280, height: 720, mime_type: "image/jpeg", caption: "真实画面", crop: null, replaces_frame_id: null, availability: "ready", unavailable_reason: null, expires_at: null, urls: { full: "/adopted.jpg", thumbnail: "/adopted-thumb.jpg" } }
const groups: ArticleImageGroup[] = [{ chapter: 2, sectionId: "steps", sectionTitle: "操作", images: [{ frameId: frame.id, frame, caption: "已采用的截图", sectionId: "steps", blockId: "figure" }, { frameId: "missing", frame: undefined, caption: "仍可回到正文", sectionId: "steps", blockId: "missing-figure" }] }]
let renderer: ReactTestRenderer
afterEach(() => { act(() => renderer?.unmount()) })

it("loads the gallery on demand and opens the selected article frame in the existing viewer", () => {
  const onOpenFrame = vi.fn()
  act(() => { renderer = create(<ArticleGallery groups={groups} onOpenFrame={onOpenFrame} onLocate={vi.fn()} onBroken={vi.fn()} />) })
  expect(renderer.root.findAllByType("img")).toHaveLength(0)
  act(() => renderer.root.findByProps({ className: "disclosure gallery-trigger" }).props.onClick())
  const buttons = renderer.root.findAllByProps({ className: "gallery-image" })
  expect(buttons[0].props.disabled).toBe(false)
  expect(buttons[1].props.disabled).toBe(true)
  expect(renderer.root.findByType("time").children).toEqual(["00:12"])
  const origin = { id: "thumbnail" }
  act(() => buttons[0].props.onClick({ currentTarget: origin }))
  expect(onOpenFrame).toHaveBeenCalledWith([frame], 0, origin)
  expect(renderer.root.findByType(ReadingDialog).props.open).toBe(false)
})

it("waits until the dialog closes before focusing and scrolling to a missing image's original location", () => {
  const onLocate = vi.fn()
  act(() => { renderer = create(<ArticleGallery groups={groups} onOpenFrame={vi.fn()} onLocate={onLocate} onBroken={vi.fn()} />) })
  act(() => renderer.root.findByProps({ className: "disclosure gallery-trigger" }).props.onClick())
  const locate = renderer.root.findAllByType("button").filter((button) => button.children.includes("定位正文"))
  act(() => locate[1].props.onClick())
  expect(onLocate).not.toHaveBeenCalled()
  act(() => renderer.root.findByType(ReadingDialog).props.onAfterClose())
  expect(onLocate).toHaveBeenCalledWith("steps", "missing-figure")
})
