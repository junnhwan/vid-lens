'use client'

import { useMemo } from 'react'
import { parseInline, parseMarkdown, type InlineNode } from '@/lib/markdown'

export function MarkdownAnswer({
  content,
  onCite,
}: {
  content: string
  onCite?: (n: number) => void
}) {
  const blocks = useMemo(() => parseMarkdown(content), [content])
  if (!content) return null
  return (
    <>
      {blocks.map((block, i) => {
        if (block.type === 'h') {
          const Tag = block.level === 1 ? 'h3' : block.level === 2 ? 'h4' : 'h5'
          return <Tag key={i} className={`md-h md-h${block.level}`}><Inline text={block.text} onCite={onCite} /></Tag>
        }
        if (block.type === 'ul') {
          return (
            <ul key={i} className="md-list">
              {block.items.map((item, j) => <li key={j}><Inline text={item} onCite={onCite} /></li>)}
            </ul>
          )
        }
        if (block.type === 'ol') {
          return (
            <ol key={i} className="md-list md-ol">
              {block.items.map((item, j) => <li key={j}><Inline text={item} onCite={onCite} /></li>)}
            </ol>
          )
        }
        if (block.type === 'pre') {
          return (
            <div key={i} className="md-code">
              {block.lang && <span className="md-code-lang mono">{block.lang}</span>}
              <pre><code>{block.code}</code></pre>
            </div>
          )
        }
        if (block.type === 'quote') {
          return (
            <blockquote key={i} className="md-quote">
              {block.items.map((item, j) => <p key={j}><Inline text={item} onCite={onCite} /></p>)}
            </blockquote>
          )
        }
        return <p key={i}><Inline text={block.text} onCite={onCite} /></p>
      })}
    </>
  )
}

function Inline({ text, onCite }: { text: string; onCite?: (n: number) => void }) {
  const nodes = useMemo(() => parseInline(text), [text])
  return <>{nodes.map((n, i) => <InlinePiece key={i} node={n} onCite={onCite} />)}</>
}

function InlinePiece({ node, onCite }: { node: InlineNode; onCite?: (n: number) => void }) {
  if (node.type === 'cite') {
    if (!onCite) return <span className="cite">C{node.n}</span>
    return (
      <button type="button" className="cite" title="查看证据详情" onClick={() => onCite(node.n)}>
        C{node.n}
      </button>
    )
  }
  if (node.type === 'code') return <code className="md-inline-code">{node.text}</code>
  if (node.type === 'strong') return <strong>{node.text}</strong>
  if (node.type === 'em') return <em>{node.text}</em>
  return <>{node.text}</>
}
