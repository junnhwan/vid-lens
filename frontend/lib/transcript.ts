// 把过长的 ASR 段按句切开,方便阅读。已按句返回的短段保持原样。
// 没有更细时间码时,在原片段的起止时间内按字符比例插值,点击仍能落到大致位置。

const SENTENCE = /[^。！？!?；;\n]+[。！？!?；;\n]?/g
const MAX_CHUNK = 140

export function splitForReading(content: string): string[] {
  const trimmed = content.replace(/\s+/g, ' ').trim()
  if (!trimmed) return []
  if (trimmed.length <= MAX_CHUNK) return [trimmed]
  const parts = trimmed.match(SENTENCE)
  if (!parts || parts.length <= 1) {
    const hard: string[] = []
    for (let i = 0; i < trimmed.length; i += MAX_CHUNK) hard.push(trimmed.slice(i, i + MAX_CHUNK))
    return hard
  }
  const chunks: string[] = []
  let buf = ''
  for (const part of parts) {
    const next = buf + part
    if (next.length > MAX_CHUNK && buf) {
      chunks.push(buf.trim())
      buf = part
    } else {
      buf = next
    }
  }
  if (buf.trim()) chunks.push(buf.trim())
  return chunks
}

export interface TimedText {
  id: string
  start_ms: number
  end_ms: number
  content: string
}

export function expandTranscript<T extends TimedText>(atoms: T[]): T[] {
  const out: T[] = []
  for (const atom of atoms) {
    const parts = splitForReading(atom.content)
    if (parts.length <= 1) {
      out.push(atom)
      continue
    }
    const span = Math.max(atom.end_ms - atom.start_ms, parts.length * 800)
    const total = Math.max(1, parts.reduce((n, p) => n + p.length, 0))
    let offset = 0
    parts.forEach((text, i) => {
      const startRatio = offset / total
      offset += text.length
      const endRatio = offset / total
      out.push({
        ...atom,
        id: `${atom.id}:${i}`,
        content: text,
        start_ms: Math.round(atom.start_ms + span * startRatio),
        end_ms: Math.round(atom.start_ms + span * endRatio),
      })
    })
  }
  return out
}
