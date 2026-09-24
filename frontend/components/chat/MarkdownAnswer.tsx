'use client'

import React, { useMemo } from 'react'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import { peelDomainTags, parseMarkdown, unwrapMarkdownFence } from '@/lib/markdown'

const citePattern = /\[C(\d+)\]/g

export function MarkdownAnswer({ content, onCite, domainTags = false }: {
  content: string
  onCite?: (n: number) => void
  domainTags?: boolean
}) {
  const { body, tags } = useMemo(() => {
    const normalized = unwrapMarkdownFence(content)
    if (!domainTags) return { body: normalized, tags: [] as string[] }
    const result = peelDomainTags(parseMarkdown(normalized))
    if (!result.tags.length) return { body: normalized, tags: [] as string[] }
    const heading = normalized.search(/^#{1,6}\s+领域标签\s*$/m)
    const inline = normalized.search(/^(?:\*\*)?领域标签(?:\*\*)?[：:]/m)
    const end = heading >= 0 ? heading : inline
    return { body: end >= 0 ? normalized.slice(0, end).trimEnd() : normalized, tags: result.tags }
  }, [content, domainTags])
  if (!content) return null
  return <>
    <ReactMarkdown remarkPlugins={[remarkGfm]} skipHtml components={{
      h1: ({ children }) => <h3 className="md-h md-h1"><Citations onCite={onCite}>{children}</Citations></h3>,
      h2: ({ children }) => <h4 className="md-h md-h2"><Citations onCite={onCite}>{children}</Citations></h4>,
      h3: ({ children }) => <h5 className="md-h md-h3"><Citations onCite={onCite}>{children}</Citations></h5>,
      h4: ({ children }) => <h6 className="md-h md-h4"><Citations onCite={onCite}>{children}</Citations></h6>,
      h5: ({ children }) => <h6 className="md-h md-h5"><Citations onCite={onCite}>{children}</Citations></h6>,
      h6: ({ children }) => <h6 className="md-h md-h6"><Citations onCite={onCite}>{children}</Citations></h6>,
      p: ({ children }) => <p><Citations onCite={onCite}>{children}</Citations></p>,
      li: ({ children }) => <li><Citations onCite={onCite}>{children}</Citations></li>,
      blockquote: ({ children }) => <blockquote className="md-quote">{children}</blockquote>,
      table: ({ children }) => <div className="md-table-scroll"><table>{children}</table></div>,
      th: ({ children }) => <th><Citations onCite={onCite}>{children}</Citations></th>,
      td: ({ children }) => <td><Citations onCite={onCite}>{children}</Citations></td>,
      pre: ({ children }) => <div className="md-code"><pre>{children}</pre></div>,
      code: ({ className, children }) => <code className={className || 'md-inline-code'}>{children}</code>,
      a: ({ href, children }) => href && /^https?:\/\//i.test(href) ? <a href={href} target="_blank" rel="noopener noreferrer">{children}</a> : <>{children}</>,
    }}>{body}</ReactMarkdown>
    {tags.length > 0 && <div className="domain-tags">{tags.map((tag, i) => <span key={`${tag}-${i}`} className={`domain-tag t${i % 4}`}>{tag}</span>)}</div>}
  </>
}

function Citations({ children, onCite }: { children: React.ReactNode; onCite?: (n: number) => void }) {
  return <>{React.Children.map(children, (child, i) => <CitationText key={i} value={child} onCite={onCite} />)}</>
}

function CitationText({ value, onCite }: { value: React.ReactNode; onCite?: (n: number) => void }) {
  if (React.isValidElement<{ children?: React.ReactNode }>(value)) {
    if (value.type === 'code' || value.type === 'a' || value.type === 'button') return value
    return React.cloneElement(value, { children: <Citations onCite={onCite}>{value.props.children}</Citations> })
  }
  if (typeof value !== 'string') return <>{value}</>
  const parts = value.split(citePattern)
  return <>{parts.map((part, i) => i % 2 ? onCite ? <button type="button" className="cite" key={i} onClick={() => onCite(Number(part))} title="查看证据详情">C{part}</button> : <span className="cite" key={i}>C{part}</span> : part)}</>
}
