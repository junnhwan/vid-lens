'use client'

import { useCallback, useEffect, useRef, useState } from 'react'

interface CodeBlockProps {
  lang?: string
  children: string
}

const LABEL: Record<string, string> = {
  bash: 'Terminal',
  sh: 'Terminal',
  shell: 'Terminal',
  json: 'JSON',
  yaml: 'YAML',
  go: 'Go',
  ts: 'TypeScript',
  js: 'JavaScript',
  env: '.env',
  text: 'Text',
}

async function copyText(text: string): Promise<boolean> {
  try {
    if (navigator.clipboard?.writeText) {
      await navigator.clipboard.writeText(text)
      return true
    }
  } catch {
    // 非安全上下文或被拒:走下面的兜底
  }
  // 兜底: 临时 textarea + execCommand(https 之外仍可用)
  const ta = document.createElement('textarea')
  ta.value = text
  ta.setAttribute('readonly', '')
  ta.style.position = 'fixed'
  ta.style.opacity = '0'
  document.body.appendChild(ta)
  ta.select()
  const ok = document.execCommand('copy')
  document.body.removeChild(ta)
  return ok
}

export function CodeBlock({ lang = 'text', children }: CodeBlockProps) {
  const [done, setDone] = useState(false)
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null)

  useEffect(() => () => { if (timer.current) clearTimeout(timer.current) }, [])

  const onCopy = useCallback(async () => {
    const ok = await copyText(children)
    if (!ok) return
    setDone(true)
    if (timer.current) clearTimeout(timer.current)
    timer.current = setTimeout(() => setDone(false), 1600)
  }, [children])

  return (
    <div className="docs-code">
      <div className="docs-code-bar">
        <span className="docs-code-lang">{LABEL[lang] || lang.toUpperCase()}</span>
        <button type="button" className={`docs-copy${done ? ' done' : ''}`} onClick={onCopy} aria-label="复制代码" aria-live="polite">
          {done ? '已复制' : '复制'}
        </button>
      </div>
      <pre>
        <code>{children}</code>
      </pre>
    </div>
  )
}
