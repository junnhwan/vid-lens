import { describe, expect, it } from "vitest"
import type { Citation } from "./types"
import { citationClock, citationStart, citationURL } from "./text"

const source = "https://www.youtube.com/watch?v=abcdefghijk"
const text: Citation = {
  id: "text", kind: "video_text", label: "01:00–02:00",
  text_origin: "asr", range: { start_ms: 60_000, end_ms: 120_000 },
}

describe("citation timing", () => {
  it.each(["asr", "mixed", undefined])("hides unverified text times for %s and opens the source without seeking", (origin) => {
    const citation = { ...text, text_origin: origin }
    expect(citationClock(citation)).toBe("视频原文")
    expect(citationStart(citation)).toBeNull()
    const url = new URL(citationURL(citation, `${source}&t=60`)!)
    expect(url.searchParams.has("t")).toBe(false)
    expect(url.searchParams.get("v")).toBe("abcdefghijk")
  })

  it.each(["platform_subtitles", "platform_subtitle"])("keeps subtitle cue times for %s", (origin) => {
    const citation = { ...text, text_origin: origin }
    expect(citationClock(citation)).toBe("01:00")
    expect(new URL(citationURL(citation, source)!).searchParams.get("t")).toBe("60")
  })

  it("uses the real frame time, including the beginning of the video", () => {
    const frame: Citation = { id: "frame", kind: "video_frame", label: "截图", time_ms: 0 }
    expect(citationClock(frame)).toBe("00:00")
    expect(new URL(citationURL(frame, source)!).searchParams.get("t")).toBe("0")
  })

  it("preserves external reference URLs and rejects unusable source links", () => {
    const external: Citation = { id: "web", kind: "external_reference", label: "网页", url: "https://example.com/read?t=60" }
    expect(citationURL(external, source)).toBe(external.url)
    expect(citationURL(text, "local://fixture")).toBeNull()
    expect(citationURL(text, "invalid")).toBeNull()
  })
})
