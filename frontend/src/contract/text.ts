import type { Block, Citation, Inline, QuestionContext, Range } from "./types"

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

export interface PassagePick {
  sectionId: string
  blockId: string
  quote: string
}

export interface PassageChoice {
  quote: string
  passages: PassagePick[]
}

export type PassageResult = { ok: true; passage: PassageChoice } | { ok: false; reason: "empty" | "long" | "mismatch" }

export function choosePassage(
  hits: PassagePick[],
  sections: { id: string; blocks: Block[] }[],
  maxCodePoints: number | null,
): PassageResult {
  if (!hits.length) return { ok: false, reason: "empty" }
  const passages = hits.map((hit) => ({ ...hit, quote: normalizeQuote(hit.quote) }))
  const joined = passages.map((part) => part.quote).join(" ")
  if (codePointLength(joined) < 2) return { ok: false, reason: "empty" }
  if (maxCodePoints != null && codePointLength(joined) > maxCodePoints) return { ok: false, reason: "long" }
  const blocks = sections.flatMap((section) => section.blocks.map((block) => ({
    sectionId: section.id, blockId: block.id, text: normalizeQuote(blockPlainText(block)),
  }))).filter((block) => block.text)
  let previous = -1
  for (const [index, part] of passages.entries()) {
    const position = blocks.findIndex((block) => block.sectionId === part.sectionId && block.blockId === part.blockId)
    const text = blocks[position]?.text
    if (!part.quote || !text?.includes(part.quote) || (index > 0 && position !== previous + 1)) return { ok: false, reason: "mismatch" }
    if (passages.length > 1) {
      const matches = index === 0 ? text.endsWith(part.quote) : index === passages.length - 1 ? text.startsWith(part.quote) : text === part.quote
      if (!matches) return { ok: false, reason: "mismatch" }
    }
    previous = position
  }
  return { ok: true, passage: { quote: passages.map((part) => part.quote).join("\n\n"), passages } }
}

export function passageContext(selection: PassageChoice | null): Pick<QuestionContext, "passage" | "passages"> {
  if (!selection?.passages.length) return { passage: null }
  const passages = selection.passages.map((part) => ({ section_id: part.sectionId, block_id: part.blockId, quote: part.quote }))
  // Keep single-block requests byte-compatible with cached clients and history.
  return passages.length === 1 ? { passage: passages[0] } : { passage: null, passages }
}

export function contextQuote(context: QuestionContext): string {
  return context.passages?.length ? context.passages.map((part) => part.quote).join("\n\n") : context.passage?.quote ?? ""
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

export function formatTokens(value: number): string {
  const n = Math.max(0, Math.round(value))
  if (n < 1000) return String(n)
  const divisor = n < 1_000_000 ? 1_000 : 1_000_000
  const unit = n < 1_000_000 ? "k" : "m"
  return `${(n / divisor).toFixed(1).replace(/\.0$/, "")}${unit}`
}

export function tokenUsageLabel(usage: { input: number; output: number }): string {
  return `输入 ${usage.input.toLocaleString("en-US")} tokens，输出 ${usage.output.toLocaleString("en-US")} tokens`
}

export function citationClock(citation: Citation): string {
  if (citation.kind === "external_reference") return "网页 ↗"
  const start = citationStart(citation)
  if (start != null) return formatClock(start)
  if (citation.kind === "video_text") return "视频原文"
  return citation.label
}

export function citationStart(citation: Citation): number | null {
  if (citation.kind === "video_frame") return citation.time_ms ?? null
  // ASR ranges can be whole audio windows. Legacy or unknown origins have no
  // timing guarantee either, so only platform subtitle cues expose text times.
  if (citation.kind === "video_text" && (citation.text_origin === "platform_subtitles" || citation.text_origin === "platform_subtitle")) {
    return citation.range?.start_ms ?? null
  }
  return null
}

export function citationURL(citation: Citation, sourceURL: string): string | null {
  if (!["video_text", "video_frame", "external_reference"].includes(citation.kind)) return null
  try {
    const url = new URL(citation.kind === "external_reference" ? citation.url || "" : sourceURL)
    if (!/^https?:$/.test(url.protocol)) return null
    if (citation.kind !== "external_reference") {
      const start = citationStart(citation)
      if (start == null) url.searchParams.delete("t")
      else url.searchParams.set("t", String(Math.floor(start / 1000)))
    }
    return url.toString()
  } catch {
    return null
  }
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
