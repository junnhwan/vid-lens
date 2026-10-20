import { act, create, type ReactTestRenderer } from "react-test-renderer"
import { expect, it } from "vitest"
import type { HttpApi } from "../api/http"
import { VideoStorage } from "./VideoStorage"

const overview = {
	max_video_ms: 120 * 60 * 1000,
  settings: { revision: 1, video_quota_bytes: 8 * 1024 ** 3, image_quota_bytes: 2 * 1024 ** 3, disk_stop_percent: 99, host_min_free_bytes: 3 * 1024 ** 3, source_download_limit_bytes: 192 * 1024 ** 2 },
  original_bytes: 42, image_bytes: 8, export_bytes: 0, reserved_video_bytes: 0, reserved_image_bytes: 0, retention: "manual" as const,
}

function video(patch: Record<string, unknown>) {
  return {
    id: "video", title: "示例", url: "https://www.youtube.com/watch?v=abcdefghijk", platform: "youtube",
    duration_ms: 60000, request_count: 1, article_count: 1, latest_status: "completed", last_requested_at: "2026-10-19T08:00:00Z",
    original_bytes: 42, image_bytes: 8, asset_ids: ["asset"], active: false, protected: false, deleting: false, delete_failures: 0,
    ...patch,
  }
}

it("shows a cleanup button only for videos that can actually be removed", async () => {
  const api = {
    request: (_method: string, path: string) => Promise.resolve({
      status: 200,
      headers: new Headers(),
      body: path.startsWith("/api/v1/admin/storage") ? overview : {
        items: [
          video({ id: "ready", title: "还能清理", cleanup: "ready" }),
          video({ id: "held", title: "正在使用", cleanup: "held", active: true, asset_ids: ["asset"] }),
          video({ id: "gone", title: "已经清理", cleanup: "gone", asset_ids: [], original_bytes: 0 }),
        ],
        total: 3,
        offset: 0,
        limit: 20,
        counts: { ready: 1, held: 1, gone: 1 },
      },
    }),
  } as unknown as HttpApi
  let view!: ReactTestRenderer
  await act(async () => {
    view = create(<VideoStorage api={api} onDirty={() => undefined} onAuthLost={() => undefined} query="" offset={0} cleanup="" onRoute={() => undefined} />)
    await new Promise((resolve) => setTimeout(resolve, 0))
  })
  const markup = JSON.stringify(view.toJSON())
  expect(markup).toContain("清理原视频")
  expect(markup).toContain("暂不能清理")
  expect(markup).toContain("正在使用，处理结束后才能清理")
  expect(markup).toContain("无可清理")
  expect(markup).toContain("原视频已清理，或当时没有下载")
  expect(markup).toContain('"可清理"," 1"')
  expect(markup).toContain('"使用中"," 1"')
  expect(markup).toContain('"已清理"," 1"')
  const gone = markup.slice(markup.indexOf("已经清理"))
  expect(gone.includes("清理原视频")).toBe(false)
})
