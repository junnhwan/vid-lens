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
    // Wrapping and expansion handle long utterances. A character-count cut
    // can split an English word or a Chinese sentence without a speech break.
    return [trimmed]
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

// Acoustic words form sentence reading rows across windows. Coarse legacy
// projections remain grouped by their measured window; no times are invented.
export function groupTranscriptSources<T extends TimedText>(atoms: T[]): (T & { paragraphs: string[]; source_ids: string[] })[] {
  const groups: (T & { paragraphs: string[]; source_ids: string[] })[] = []
  for (const atom of atoms) {
    const paragraphs = splitForReading(atom.content)
    if (!paragraphs.length) continue
    const status = atom.time_range_status || 'coarse'
    const previous = groups[groups.length - 1]
    const timed = status !== 'unknown' && Number.isFinite(atom.start_ms) && Number.isFinite(atom.end_ms) && atom.start_ms >= 0 && atom.end_ms > atom.start_ms
    const continuousSpeech = timed && status === 'exact' && previous?.time_range_status === 'exact'
      && atom.start_ms >= previous.start_ms && atom.start_ms - previous.end_ms <= 2000
      && !/[。！？!?；;.]\s*[”’"）)]?\s*$/.test(previous.content)
    if (continuousSpeech && previous) {
      // Acoustic word positions supply time, sentence punctuation supplies
      // reading boundaries. Crossing an ASR window does not create a new row.
      previous.content += atom.content
      previous.end_ms = Math.max(previous.end_ms, atom.end_ms)
      previous.paragraphs = splitForReading(previous.content)
      previous.source_ids.push(atom.id)
    } else if (timed && status !== 'exact' && previous && previous.time_range_status === status && previous.start_ms === atom.start_ms && previous.end_ms === atom.end_ms) {
      previous.paragraphs.push(...paragraphs)
      previous.source_ids.push(atom.id)
    } else groups.push({ ...atom, time_range_status: status, paragraphs, source_ids: [atom.id] })
  }
  return groups
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
