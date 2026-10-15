import type { Block, Citation, Inline, Range } from "./types"

export function codePointLength(value: string): number {
  return [...value].length
}

/** Collapse Unicode whitespace the same way the contract compares a passage. */
export function normalizeQuote(value: string): string {
  return value.replace(/\p{White_Space}+/gu, " ").trim()
}

function inlineText(items: Inline[] | undefined): string {
  let text = ""
  for (const item of items ?? []) {
    if (item.type === "text" || item.type === "link") text += item.text ?? ""
  }
  return text
}

export function blockPlainText(block: Block): string {
  switch (block.type) {
    case "paragraph":
    case "quote":
      return inlineText(block.content)
    case "list":
      return (block.items ?? []).map((item) => inlineText(item.content)).join("\n")
    case "code":
      return block.text ?? ""
    case "table": {
      const lines = [...(block.columns ?? [])]
      for (const row of block.rows ?? []) lines.push(...row.cells)
      return lines.join("\n")
    }
    case "figure":
      return block.caption ?? ""
    default:
      return ""
  }
}

export function quoteMatchesBlock(block: Block, quote: string): boolean {
  const normalized = normalizeQuote(quote)
  if (!normalized) return false
  return normalizeQuote(blockPlainText(block)).includes(normalized)
}

export interface PassageChoice {
  sectionId: string
  blockId: string
  quote: string
}

export type PassageResult =
  | { ok: true; passage: PassageChoice }
  | { ok: false; reason: "empty" | "cross" | "long" | "mismatch" }

export function choosePassage(
  hits: { sectionId: string; blockId: string; block: Block }[],
  rawQuote: string,
  maxCodePoints: number | null,
): PassageResult {
  const unique = new Map<string, { sectionId: string; blockId: string; block: Block }>()
  for (const hit of hits) unique.set(hit.blockId, hit)
  if (unique.size === 0) return { ok: false, reason: "empty" }
  if (unique.size > 1) return { ok: false, reason: "cross" }
  const hit = [...unique.values()][0]
  const quote = normalizeQuote(rawQuote)
  if ([...quote].length < 2) return { ok: false, reason: "empty" }
  if (maxCodePoints != null && codePointLength(quote) > maxCodePoints) return { ok: false, reason: "long" }
  if (!quoteMatchesBlock(hit.block, quote)) return { ok: false, reason: "mismatch" }
  return { ok: true, passage: { sectionId: hit.sectionId, blockId: hit.blockId, quote } }
}

export function formatClock(ms: number): string {
  const safe = Math.max(0, Math.floor(ms / 1000))
  const hours = Math.floor(safe / 3600)
  const minutes = Math.floor((safe % 3600) / 60)
  const seconds = safe % 60
  const mm = String(minutes).padStart(2, "0")
  const ss = String(seconds).padStart(2, "0")
  if (hours > 0) return `${hours}:${mm}:${ss}`
  return `${mm}:${ss}`
}

export function formatRange(range: Range | null | undefined): string {
  if (!range) return ""
  return `${formatClock(range.start_ms)} – ${formatClock(range.end_ms)}`
}

export function formatDuration(ms: number | null | undefined): string {
  if (ms == null) return ""
  return formatClock(ms)
}

export function citationClock(citation: Citation): string {
  if (citation.kind === "video_frame" && citation.time_ms != null) return formatClock(citation.time_ms)
  if (citation.kind === "video_text" && citation.range) return formatClock(citation.range.start_ms)
  return citation.label
}

export function clipText(value: string, max: number): string {
  const clean = normalizeQuote(value)
  const chars = [...clean]
  if (chars.length <= max) return clean
  return `${chars.slice(0, max).join("")}…`
}

export function platformLabel(platform: string | null | undefined): string {
  if (platform === "bilibili") return "B站"
  if (platform === "youtube") return "YouTube"
  if (platform === "local") return "本地"
  return platform || ""
}

const provenanceText: Record<string, string> = {
  platform_subtitles: "使用平台字幕。",
  asr: "平台字幕不可用，使用转写。",
  mixed: "字幕和转写一起使用。",
  unavailable: "没有可用的完整文本。",
}

export function provenanceNote(origin: string | null | undefined, coverage: string | null | undefined): string {
  const base = (origin && provenanceText[origin]) || ""
  if (coverage === "partial") return base ? `${base}文本没有覆盖全片。` : "文本没有覆盖全片。"
  if (coverage === "unknown" && base) return `${base}覆盖范围还没有核验。`
  return base
}

export function unavailableFrameCopy(reason: string | null | undefined): string {
  if (reason === "expired") return "这张画面已过期"
  if (reason === "source_unavailable") return "原来的视频已经不能再取出这张画面"
  if (reason === "temporarily_unavailable") return "这张画面暂时读不出来"
  return "这张画面现在不能显示"
}
