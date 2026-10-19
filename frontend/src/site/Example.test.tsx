import { describe, expect, it } from "vitest"
import { renderToStaticMarkup } from "react-dom/server"
import type { Frame } from "../contract/types"
import { ExampleArticle } from "./Example"
import type { ExampleData } from "./exampleReplay"
import type { useExampleQuestions } from "./useExampleQuestions"

const frame: Frame = {
  id: "frame", snapshot_id: "", time_ms: 1500, requested_time_ms: 1500, width: 32, height: 18,
  mime_type: "image/jpeg", caption: "手写数字", crop: null, replaces_frame_id: null,
  availability: "ready", unavailable_reason: null, expires_at: null,
  urls: { full: "/api/v1/examples/article/frames/frame/content", thumbnail: "/api/v1/examples/article/frames/frame/content" },
}

const data: ExampleData = {
  id: "article",
  schema_version: 1,
  detail_level: "standard",
  purpose: "看懂这个网络",
  qa_available: true,
  process_status: "ready",
  generation_steps: [{
    id: "look", run_id: "attempt-0", segment_no: 0, first_seq: 1, last_seq: 1, kind: "tool_execution",
    phase: "investigate", title: "查看手写数字", status: "completed", purpose: null, source_range: null,
    frame_ids: ["frame"], frames: [frame], outcome: null, retry_of_step_id: null, started_at: null, finished_at: null,
  }],
  media: { status: "ready", can_inspect_now: true, can_restore_for_question: true },
  frames: [frame],
  article: {
    schema_version: 1, id: "article", snapshot_id: "", title: "神经网络到底是什么", published_at: "2026-10-18T00:00:00Z",
    quality: "standard", mind_map: null, limitations: [], citations: {},
    source: { source_id: "", snapshot_id: null, platform: "youtube", canonical_url: "https://youtube.com/watch?v=abcdefghijk", title: null, duration_ms: 1119000, part: null, text_provenance: null },
    sections: [{
      id: "chapter", title: "从一个识别手写数字的网络讲起",
      blocks: [{ id: "figure", type: "figure", origin: "model", frame_ids: ["frame"], caption: "封面里的数字", layout: "single" }],
    }],
  },
}

function questions(open = false): ReturnType<typeof useExampleQuestions> {
  return {
    open, turns: [], frames: [], stepsFor: () => [], draft: "", frameChip: null, passageChip: null, prompts: ["这条视频主要在讲什么？"],
    busy: false, error: "", expanded: {}, focusToken: 0, externalAllowed: false, externalAvailable: false, searchAvailable: false,
    referenceLinks: "", selectionMax: 1000, setDraft: () => undefined, setExternal: () => undefined, setReferenceLinks: () => undefined,
    askFrame: () => undefined, askPassage: () => undefined, toggle: () => undefined, close: () => undefined, suggest: () => undefined,
    removeFrame: () => undefined, removePassage: () => undefined, toggleTurn: () => undefined, stop: () => undefined, retry: () => undefined, send: () => undefined,
  }
}

describe("public example reading", () => {
  it("shows the frozen generation and a visitor question control without replaying or exposing an owner question", () => {
    const html = renderToStaticMarkup(<ExampleArticle data={data} frames={new Map([[frame.id, frame]])} questions={questions()} onOpenFrame={() => undefined} onCite={() => undefined} />)
    expect(html).toContain("神经网络到底是什么")
    expect(html).toContain("查看生成过程")
    expect(html).toContain("查看手写数字")
    expect(html).toContain("问这条视频")
    expect(html).toContain("问这张图")
    expect(html).not.toContain("站长私人追问不应公开")
    expect(html).not.toContain("example-lead")
    expect(html).not.toContain("/api/v1/requests/")
  })

  it("says when the generation record was not kept and still lets the article be read", () => {
    const missing = { ...data, process_status: "unavailable", generation_steps: [], qa_available: false }
    const html = renderToStaticMarkup(<ExampleArticle data={missing} frames={new Map([[frame.id, frame]])} questions={questions()} onOpenFrame={() => undefined} onCite={() => undefined} />)
    expect(html).toContain("这份示例发布时，生成过程已经不在记录里。")
    expect(html).toContain("从一个识别手写数字的网络讲起")
    expect(html).not.toContain("问这张图")
    expect(html).toContain("原模型暂不可用")
  })
})
