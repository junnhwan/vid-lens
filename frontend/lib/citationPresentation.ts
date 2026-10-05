import { fromMarkdown } from 'mdast-util-from-markdown'
import type { CiteRef } from '../components/Citation'
import { withClaimCitations } from './citationText.ts'

interface CitationToken { id: string; start: number; end: number }

// Number only visible prose citations. Code, links, images and Markdown
// definitions are source material, not answer-to-evidence links.
function proseTokens(content: string): CitationToken[] {
  const tokens: CitationToken[] = []
  type Node = { type: string; children?: Node[]; position?: { start: { offset?: number }; end: { offset?: number } } }
  function visit(node: Node) {
    if (['code', 'inlineCode', 'link', 'linkReference', 'image', 'imageReference', 'html', 'definition'].includes(node.type)) return
    if (node.type === 'text') {
      const start = node.position?.start.offset
      const end = node.position?.end.offset
      if (start === undefined || end === undefined) return
      const source = content.slice(start, end)
      for (const match of source.matchAll(/\[(C[1-9]\d*)\]/g)) {
        const index = match.index!
        let escapes = 0
        for (let i = index - 1; i >= 0 && source[i] === '\\'; i--) escapes++
        if (escapes % 2) continue
        tokens.push({ id: match[1], start: start + index, end: start + index + match[0].length })
      }
    } else node.children?.forEach(visit)
  }
  visit(fromMarkdown(content))
  return tokens
}

function replaceTokens(content: string, tokens: CitationToken[], ids: Map<string, string>): string {
  let result = '', cursor = 0
  for (const token of tokens) {
    result += content.slice(cursor, token.start)
    const id = ids.get(token.id)
    // An unknown candidate cannot become a link to a newly numbered source.
    if (id) result += `[${id}]`
    cursor = token.end
  }
  return result + content.slice(cursor)
}

// A presentation projection only: saved answers, candidate IDs, source spans
// and semantic review results stay untouched for imports and traceability.
export function presentAnswerCitations(content: string, cites: CiteRef[]): { content: string; cites: CiteRef[] } {
  if (!cites.length) return { content, cites }
  let tokens = proseTokens(content)
  if (!tokens.length) {
    content = withClaimCitations(content, cites)
    tokens = proseTokens(content)
  }
  const byID = new Map(cites.map(cite => [cite.id, cite]))
  const ids = new Map<string, string>()
  const order: string[] = []
  const add = (id: string) => {
    if (!byID.has(id) || ids.has(id)) return
    ids.set(id, `C${ids.size + 1}`)
    order.push(id)
  }
  tokens.forEach(token => add(token.id))
  // Older snapshots can carry evidence without inline links. Keep it visible
  // after the referenced evidence, with deterministic consecutive labels.
  cites.forEach(cite => add(cite.id))
  return {
    content: replaceTokens(content, tokens, ids),
    cites: order.map(id => {
      const cite = byID.get(id)!
      return { ...cite, id: ids.get(id)!, candidateId: cite.candidateId || cite.id,
        claimTexts: cite.claimTexts?.map(claim => replaceTokens(claim, proseTokens(claim), ids)),
      }
    }),
  }
}
