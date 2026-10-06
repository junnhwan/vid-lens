import React, { createContext, useContext, useMemo } from 'react'
import ReactMarkdown, { type Components } from 'react-markdown'
import remarkGfm from 'remark-gfm'
import { peelDomainTags, parseMarkdown, unwrapMarkdownFence } from '@/lib/markdown'

import { renderCitationNodes } from '@/lib/citationNodes'

export function MarkdownAnswer({ content, onCite, domainTags = false, activeCite }: {
  content: string
  onCite?: (n: number) => void
  domainTags?: boolean
  activeCite?: number
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
    <CitationContext.Provider value={{ onCite, activeCite }}>
      <ReactMarkdown remarkPlugins={[remarkGfm]} skipHtml components={markdownComponents}>{body}</ReactMarkdown>
    </CitationContext.Provider>
    {tags.length > 0 && <div className="domain-tags">{tags.map((tag, i) => <span key={`${tag}-${i}`} className={`domain-tag t${i % 4}`}>{tag}</span>)}</div>}
  </>
}

const CitationContext = createContext<{ onCite?: (n: number) => void; activeCite?: number }>({})

// Stable renderers preserve a citation trigger's focus when selection changes.
const markdownComponents: Components = {
  h1: ({ children }) => <h3 className="md-h md-h1"><Citations>{children}</Citations></h3>,
  h2: ({ children }) => <h4 className="md-h md-h2"><Citations>{children}</Citations></h4>,
  h3: ({ children }) => <h5 className="md-h md-h3"><Citations>{children}</Citations></h5>,
  h4: ({ children }) => <h6 className="md-h md-h4"><Citations>{children}</Citations></h6>,
  h5: ({ children }) => <h6 className="md-h md-h5"><Citations>{children}</Citations></h6>,
  h6: ({ children }) => <h6 className="md-h md-h6"><Citations>{children}</Citations></h6>,
  p: ({ children }) => <p><Citations>{children}</Citations></p>,
  li: ({ children }) => <li><Citations>{children}</Citations></li>,
  blockquote: ({ children }) => <blockquote className="md-quote">{children}</blockquote>,
  table: ({ children }) => <div className="md-table-scroll"><table>{children}</table></div>,
  th: ({ children }) => <th><Citations>{children}</Citations></th>,
  td: ({ children }) => <td><Citations>{children}</Citations></td>,
  pre: ({ children }) => <div className="md-code"><pre>{children}</pre></div>,
  code: ({ className, children }) => <code className={className || 'md-inline-code'}>{children}</code>,
  a: ({ href, children }) => href && /^https?:\/\//i.test(href) ? <a href={href} target="_blank" rel="noopener noreferrer">{children}</a> : <>{children}</>,
}

function Citations({ children }: { children: React.ReactNode }) {
  const { onCite, activeCite } = useContext(CitationContext)
  return <>{renderCitationNodes(children, onCite, activeCite)}</>
}
