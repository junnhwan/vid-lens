'use client'

// Agent 问答 UI 原型 — 仅保留两个定稿方向
// 访问：http://localhost:3000/prototype/agent-chat?style=full|simple

import { Suspense } from 'react'
import { useRouter, useSearchParams } from 'next/navigation'
import Link from 'next/link'
import { useAgentDemo } from '@/components/prototype/agent/useAgentDemo'
import { AGENT_STYLES, type AgentStyleKey } from '@/components/prototype/agent/types'
import { DemoControls } from '@/components/prototype/agent/shared'
import { AgentVariantL } from '@/components/prototype/agent/VariantL'
import { AgentVariantM } from '@/components/prototype/agent/VariantM'

export default function AgentChatPrototypePage() {
  return (
    <Suspense fallback={<div className="h-[100dvh] bg-paper-1" />}>
      <AgentChatPrototypeView />
    </Suspense>
  )
}

function AgentChatPrototypeView() {
  const sp = useSearchParams()
  const router = useRouter()
  const style = (sp.get('style') ?? 'full') as AgentStyleKey
  const { steps, running, run, reset } = useAgentDemo(true, 'research')

  const setStyle = (next: AgentStyleKey) => {
    const q = new URLSearchParams(sp.toString())
    q.set('style', next)
    router.replace(`?${q.toString()}`, { scroll: false })
  }

  return (
    <div className="h-[100dvh] flex flex-col bg-paper-1 text-ink-0 overflow-hidden proto-root">
      <header className="shrink-0 bg-paper-0/80 border-b border-ink-0/8 px-6 py-3">
        <div className="max-w-2xl mx-auto flex items-center justify-between gap-4 flex-wrap">
          <div className="flex items-center gap-3 min-w-0">
            <Link href="/prototype" className="text-[12px] text-ink-4 hover:text-ink-1 shrink-0">原型入口</Link>
            <div className="h-4 w-px bg-ink-0/10 shrink-0" />
            <div className="min-w-0">
              <h1 className="text-[15px] font-medium text-ink-0 tracking-tight">Agent 问答</h1>
              <p className="text-[11px] text-ink-4 truncate">mock 演示 · 正式产品请用 <Link href="/chat/101" className="text-sienna-700 hover:underline">/chat/101</Link></p>
            </div>
          </div>
          <div className="flex items-center gap-2 shrink-0">
            <div className="flex rounded-lg border border-ink-0/10 overflow-hidden text-[11px]">
              {AGENT_STYLES.map(s => (
                <button
                  key={s.key}
                  type="button"
                  onClick={() => setStyle(s.key)}
                  className={`px-3 py-1.5 transition-colors ${style === s.key ? 'bg-ink-0 text-paper-0' : 'text-ink-3 hover:bg-ink-0/4'}`}
                >
                  {s.name}
                </button>
              ))}
            </div>
            <DemoControls running={running} onRun={run} onReset={reset} />
          </div>
        </div>
      </header>

      {style === 'full' ? <AgentVariantM steps={steps} /> : <AgentVariantL steps={steps} />}
    </div>
  )
}
