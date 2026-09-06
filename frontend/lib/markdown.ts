// 问答 / 摘要用的轻量 Markdown:标题、列表、围栏代码、引用、粗体、行内代码、[C#] 引用芯片。
// 不引入完整编译器,流式未闭合的 ``` 会把剩余文本当成代码块,避免半截标记把版面打乱。

export type MdBlock =
  | { type: 'p'; text: string }
  | { type: 'h'; level: 1 | 2 | 3; text: string }
  | { type: 'ul'; items: string[] }
  | { type: 'ol'; items: string[] }
  | { type: 'pre'; lang: string; code: string }
  | { type: 'quote'; items: string[] }

export type InlineNode =
  | { type: 'text'; text: string }
  | { type: 'cite'; n: number }
  | { type: 'code'; text: string }
  | { type: 'strong'; text: string }
  | { type: 'em'; text: string }

const FENCE_OPEN = /^```([\w+-]*)\s*$/
const FENCE_CLOSE = /^```\s*$/
const HEADING = /^(#{1,3})\s+(.+)$/
const UL = /^[-*]\s+(.+)$/
const OL = /^\d+[.)]\s+(.+)$/
const QUOTE = /^>\s?(.*)$/
const SPECIAL_START = /^(#{1,3}\s|```|[-*]\s|\d+[.)]\s|>)/

export function parseMarkdown(src: string): MdBlock[] {
  const lines = src.replace(/\r\n/g, '\n').split('\n')
  const blocks: MdBlock[] = []
  let i = 0
  while (i < lines.length) {
    const line = lines[i]
    const fence = line.match(FENCE_OPEN)
    if (fence) {
      const lang = fence[1] || ''
      const buf: string[] = []
      i += 1
      while (i < lines.length && !FENCE_CLOSE.test(lines[i])) {
        buf.push(lines[i])
        i += 1
      }
      if (i < lines.length) i += 1
      blocks.push({ type: 'pre', lang, code: buf.join('\n') })
      continue
    }
    if (!line.trim()) {
      i += 1
      continue
    }
    const h = line.match(HEADING)
    if (h) {
      blocks.push({ type: 'h', level: h[1].length as 1 | 2 | 3, text: h[2] })
      i += 1
      continue
    }
    const q = line.match(QUOTE)
    if (q) {
      const items: string[] = []
      while (i < lines.length) {
        const m = lines[i].match(QUOTE)
        if (!m) break
        items.push(m[1])
        i += 1
      }
      blocks.push({ type: 'quote', items })
      continue
    }
    const ul = line.match(UL)
    if (ul) {
      const items: string[] = []
      while (i < lines.length) {
        const m = lines[i].match(UL)
        if (!m) break
        items.push(m[1])
        i += 1
      }
      blocks.push({ type: 'ul', items })
      continue
    }
    const ol = line.match(OL)
    if (ol) {
      const items: string[] = []
      while (i < lines.length) {
        const m = lines[i].match(OL)
        if (!m) break
        items.push(m[1])
        i += 1
      }
      blocks.push({ type: 'ol', items })
      continue
    }
    const para: string[] = [line]
    i += 1
    while (i < lines.length && lines[i].trim() && !SPECIAL_START.test(lines[i])) {
      para.push(lines[i])
      i += 1
    }
    blocks.push({ type: 'p', text: para.join('\n') })
  }
  return blocks
}

const INLINE_RE = /(\[C(\d+)\]|`([^`]+)`|\*\*([^*]+)\*\*|\*([^*\n]+)\*)/g

export function parseInline(text: string): InlineNode[] {
  const nodes: InlineNode[] = []
  let cursor = 0
  for (const match of text.matchAll(INLINE_RE)) {
    const start = match.index ?? 0
    if (start > cursor) nodes.push({ type: 'text', text: text.slice(cursor, start) })
    if (match[2]) nodes.push({ type: 'cite', n: Number(match[2]) })
    else if (match[3] != null) nodes.push({ type: 'code', text: match[3] })
    else if (match[4] != null) nodes.push({ type: 'strong', text: match[4] })
    else if (match[5] != null) nodes.push({ type: 'em', text: match[5] })
    cursor = start + match[0].length
  }
  if (cursor < text.length) nodes.push({ type: 'text', text: text.slice(cursor) })
  return nodes
}
