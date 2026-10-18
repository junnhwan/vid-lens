import { renderToStaticMarkup } from "react-dom/server"
import { describe, expect, it } from "vitest"
import type { Citation } from "../contract/types"
import { citationClusterLabel, Inlines } from "./Blocks"

const asr: Citation = { id: "asr", kind: "video_text", label: "01:00–02:00", text_origin: "asr", range: { start_ms: 60_000, end_ms: 120_000 } }
const subtitle: Citation = { id: "subtitle", kind: "video_text", label: "字幕", text_origin: "platform_subtitles", range: { start_ms: 90_000, end_ms: 92_000 } }
const frame: Citation = { id: "frame", kind: "video_frame", label: "画面", time_ms: 75_000 }

function renderCitations(cites: Citation[]) {
  return renderToStaticMarkup(<Inlines
    items={[{ type: "text", text: "保留正文和来源。" }, ...cites.map((citation) => ({ type: "citation", citation_id: citation.id }))]}
    citations={Object.fromEntries(cites.map((citation) => [citation.id, citation]))}
    onCite={() => undefined}
  />)
}

describe("reading and answer citation labels", () => {
  it("keeps the ASR source entry without displaying its window time", () => {
    const html = renderCitations([asr])
    expect(html).toContain("保留正文和来源。")
    expect(html).toContain(">视频原文<")
    expect(html).toContain('data-citation="true"')
    expect(html).not.toMatch(/01:00|02:00/)
  })

  it("does not turn ASR windows into a range when citations are grouped", () => {
    const later = { ...asr, id: "later", range: { start_ms: 180_000, end_ms: 240_000 } }
    expect(citationClusterLabel([asr, later])).toBe("视频原文 +1")
    expect(renderCitations([asr, later])).not.toMatch(/01:00|02:00|03:00|04:00/)
  })

  it("keeps frame times in a mixed group without exposing the ASR window", () => {
    const html = renderCitations([asr, frame])
    expect(html).toContain(">视频原文<")
    expect(html).toContain(">01:15<")
    expect(html).not.toMatch(/01:00|02:00/)
  })

  it("continues to group timestamps backed by subtitle cues and real frames", () => {
    expect(citationClusterLabel([subtitle, frame])).toBe("01:15–01:30")
    const html = renderCitations([subtitle, frame])
    expect(html).toContain(">01:30<")
    expect(html).toContain(">01:15<")
  })
})
