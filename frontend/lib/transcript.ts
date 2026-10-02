// 把过长的 ASR 段按句切开,方便阅读。已按句返回的短段保持原样。
// 阅读拆分不推算句子时间；每行保留其来源片段的真实起止时间。

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
  time_range_status?: string
}

export function expandTranscript<T extends TimedText>(atoms: T[]): (T & Pick<TimedText, 'time_range_status'>)[] {
  const out: (T & Pick<TimedText, 'time_range_status'>)[] = []
  for (const atom of atoms) {
    const parts = splitForReading(atom.content)
    if (parts.length <= 1) {
      out.push({ ...atom, time_range_status: atom.time_range_status || 'coarse' })
      continue
    }
    parts.forEach((text, i) => {
      out.push({
        ...atom,
        id: `${atom.id}:${i}`,
        content: text,
        time_range_status: atom.time_range_status || 'coarse',
      })
    })
  }
  return out
}
