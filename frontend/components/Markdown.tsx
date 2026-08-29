'use client'

import React from 'react'

// 极简 Markdown 渲染器：覆盖 LLM 常见输出，不引依赖（沿用项目手搓风格）。
// 支持：标题 #/##/###、加粗 **x**、斜体 *x*、行内代码 `x`、
// 无序列表 -/+/*、有序列表 1.、段落、代码块 ```lang\n```。
// 不追求 CommonMark 完整性，够产品展示用。
//
// 安全性：纯文本节点走 React 文本子节点（默认转义），不 dangerouslySetInnerHTML，
// 所以后端返回的 HTML 标签会被当文本显示，不会注入。

interface Block {
  type: 'h1' | 'h2' | 'h3' | 'p' | 'ul' | 'ol' | 'code'
  text?: string
  items?: string[]
  lang?: string
}

export default function Markdown({ content, className }: { content: string; className?: string }) {
  const blocks = parseBlocks(content)
  return (
    <div className={className}>
      {blocks.map((b, i) => {
        switch (b.type) {
          case 'h1':
            return <h1 key={i} className="font-sans text-[18px] font-semibold text-ink-0 mt-5 mb-2 leading-snug">{renderInline(b.text!)}</h1>
          case 'h2':
            return <h2 key={i} className="font-sans text-[16px] font-semibold text-ink-0 mt-4 mb-2 leading-snug">{renderInline(b.text!)}</h2>
          case 'h3':
            return <h3 key={i} className="font-sans text-[14px] font-semibold text-ink-1 mt-3 mb-1.5 leading-snug">{renderInline(b.text!)}</h3>
          case 'code':
            return (
              <pre key={i} className="font-mono text-[12px] bg-ink-0/[.04] border border-ink-0/10 px-3.5 py-2.5 my-3 overflow-x-auto scroll-thin">
                <code className="text-ink-1">{b.text}</code>
              </pre>
            )
          case 'ul':
            return (
              <ul key={i} className="space-y-1 my-2.5">
                {b.items!.map((it, j) => (
                  <li key={j} className="font-sans text-[14px] leading-[1.7] text-ink-1 flex gap-2">
                    <span className="text-ink-4 mt-[7px] w-1 h-1 rounded-full bg-ink-4 shrink-0" />
                    <span className="flex-1">{renderInline(it)}</span>
                  </li>
                ))}
              </ul>
            )
          case 'ol':
            return (
              <ol key={i} className="space-y-1 my-2.5">
                {b.items!.map((it, j) => (
                  <li key={j} className="font-sans text-[14px] leading-[1.7] text-ink-1 flex gap-2.5">
                    <span className="font-mono text-[11px] text-ink-4 pt-0.5 shrink-0">{String(j + 1).padStart(2, '0')}</span>
                    <span className="flex-1">{renderInline(it)}</span>
                  </li>
                ))}
              </ol>
            )
          default:
            return <p key={i} className="font-sans text-[14.5px] leading-[1.8] text-ink-1 my-2.5">{renderInline(b.text!)}</p>
        }
      })}
    </div>
  )
}

// 块级解析：按空行分块，识别标题/列表/代码块
function parseBlocks(src: string): Block[] {
  const out: Block[] = []
  const lines = src.replace(/\r\n/g, '\n').split('\n')
  let i = 0
  let para: string[] = []
  let list: { type: 'ul' | 'ol'; items: string[] } | null = null

  const flushPara = () => {
    if (para.length) {
      out.push({ type: 'p', text: para.join(' ').trim() })
      para = []
    }
  }
  const flushList = () => {
    if (list) {
      out.push({ type: list.type, items: list.items })
      list = null
    }
  }

  while (i < lines.length) {
    const line = lines[i]
    const trimmed = line.trim()

    // 代码块
    if (trimmed.startsWith('```')) {
      flushPara(); flushList()
      const lang = trimmed.slice(3).trim()
      const code: string[] = []
      i++
      while (i < lines.length && !lines[i].trim().startsWith('```')) {
        code.push(lines[i])
        i++
      }
      i++ // 跳过结束 ```
      out.push({ type: 'code', text: code.join('\n'), lang })
      continue
    }

    // 空行 → 段落/列表收尾
    if (trimmed === '') {
      flushPara(); flushList()
      i++
      continue
    }

    // 标题
    const h = /^(#{1,3})\s+(.*)$/.exec(trimmed)
    if (h) {
      flushPara(); flushList()
      const level = h[1].length
      out.push({ type: level === 1 ? 'h1' : level === 2 ? 'h2' : 'h3', text: h[2] })
      i++
      continue
    }

    // 无序列表项 -/+/*
    const ul = /^[-+*]\s+(.*)$/.exec(trimmed)
    if (ul) {
      flushPara()
      if (!list || list.type !== 'ul') { flushList(); list = { type: 'ul', items: [] } }
      list.items.push(ul[1])
      i++
      continue
    }
    // 有序列表项 1.
    const ol = /^\d+[.)]\s+(.*)$/.exec(trimmed)
    if (ol) {
      flushPara()
      if (!list || list.type !== 'ol') { flushList(); list = { type: 'ol', items: [] } }
      list.items.push(ol[1])
      i++
      continue
    }

    // 普通段落行
    flushList()
    para.push(trimmed)
    i++
  }
  flushPara(); flushList()
  return out
}

// 行内解析：**加粗** / *斜体* / `代码`。纯文本走 React 文本节点（自动转义安全）。
export function renderMarkdownInline(text: string): React.ReactNode[] {
  return renderInline(text)
}

function renderInline(text: string): React.ReactNode[] {
  const nodes: React.ReactNode[] = []
  const re = /(\*\*([^*]+)\*\*|\*([^*]+)\*|`([^`]+)`)/g
  let last = 0
  let m: RegExpExecArray | null
  let key = 0
  while ((m = re.exec(text))) {
    if (m.index > last) nodes.push(text.slice(last, m.index))
    if (m[2] !== undefined) {
      nodes.push(<strong key={key++} className="font-semibold text-ink-0">{m[2]}</strong>)
    } else if (m[3] !== undefined) {
      nodes.push(<em key={key++}>{m[3]}</em>)
    } else if (m[4] !== undefined) {
      nodes.push(<code key={key++} className="font-mono text-[12px] px-1 py-0.5 bg-ink-0/[.06] rounded text-ink-0">{m[4]}</code>)
    }
    last = m.index + m[0].length
  }
  if (last < text.length) nodes.push(text.slice(last))
  return nodes
}
