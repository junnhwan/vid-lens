import { describe, expect, it } from "vitest"
import { renderToStaticMarkup } from "react-dom/server"
import { SharedContent } from "./SharedRecord"
import type { SharedRecordResponse } from "../contract/types"

const data: SharedRecordResponse = {
  schema_version: 1,
  view: {
    schema_version: 1,
    request: { id: "", revision: 1, input_url: "https://youtube.com/watch?v=abcdefghijk", status: "completed", reuse: "none", retry_of_id: null, article_id: "public-article", created_at: "2026-10-18T00:00:00Z", updated_at: "2026-10-18T00:01:00Z" },
    source: null, execution: null,
    activity: { current_step_id: null, recent_steps: [], has_more: false, history_url: "", history_status: "available", history_expires_at: null },
    failure: null, actions: { can_cancel: false, can_retry: false, can_read_article: true },
  },
  article: {
    schema_version: 1,
    article: {
      schema_version: 1, id: "public-article", snapshot_id: "", title: "分享的测试图文", published_at: "2026-10-18T00:01:00Z", quality: "standard", mind_map: null, limitations: [], citations: {},
      source: { source_id: "", snapshot_id: null, platform: "youtube", canonical_url: "https://youtube.com/watch?v=abcdefghijk", title: null, duration_ms: 1000, part: null, text_provenance: null },
      sections: [{ id: "chapter", title: "公开章节", blocks: [{ id: "body", type: "paragraph", origin: "model", content: [{ type: "text", text: "保留图文阅读和出处。" }] }] }],
    },
    frames: [], media: { status: "unavailable", can_inspect_now: false, can_restore_for_question: false }, qa_available: false,
  },
  steps: [{ id: "step", run_id: "attempt-0", segment_no: 0, first_seq: 1, last_seq: 1, kind: "tool_execution", phase: "compose", title: "公开处理步骤", status: "completed", purpose: null, source_range: null, frame_ids: [], frames: [], outcome: null, retry_of_step_id: null, started_at: null, finished_at: null }],
}

describe("shared reading", () => {
  it("renders the article and process without owner actions or a private Q&A panel", () => {
    const html = renderToStaticMarkup(<SharedContent data={data} />)
    expect(html).toContain("分享的测试图文")
    expect(html).toContain("公开章节")
    expect(html).toContain("公开处理步骤")
    expect(html).toContain("查看处理过程")
    expect(html).not.toMatch(/问这条视频|问这张图|准备下载|取消分享|qa-panel|\/api\/v1\/requests\//)
  })
  it("shows active processing before an article is published", () => {
    const pending = { ...data, article: null, view: { ...data.view, request: { ...data.view.request, status: "generating" } } }
    const html = renderToStaticMarkup(<SharedContent data={pending} />)
    expect(html).toContain("图文完成后会自动显示")
    expect(html).toContain("公开处理步骤")
    expect(html).not.toContain("分享的测试图文")
  })
  it("explains expired process history while retaining the published article", () => {
    const expired = { ...data, steps: [], view: { ...data.view, activity: { ...data.view.activity, history_status: "expired" } } }
    const html = renderToStaticMarkup(<SharedContent data={expired} />)
    expect(html).toContain("已超过保留期限")
    expect(html).toContain("保留图文阅读和出处")
  })
})
