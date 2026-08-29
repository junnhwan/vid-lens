'use client'

// 三种 Agent 问答 UI 变体，?variant=A|B|C 切换。演示思考/检索/工具/回答动效。

import { Suspense, useState } from 'react'
import { useSearchParams } from 'next/navigation'
import Link from 'next/link'
import { ArrowLeft } from 'lucide-react'
import PrototypeSwitcher from '@/components/prototype/PrototypeSwitcher'
import { useAgentDemo } from '@/components/prototype/agent/useAgentDemo'
import { VARIANTS, type AgentVariantKey } from '@/components/prototype/agent/types'
import { ScopePicker, DemoControls } from '@/components/prototype/agent/shared'
import AgentVariantA from '@/components/prototype/agent/VariantA'
import AgentVariantB from '@/components/prototype/agent/VariantB'
import AgentVariantC from '@/components/prototype/agent/VariantC'
import AgentVariantD from '@/components/prototype/agent/VariantD'
import { AgentVariantE } from '@/components/prototype/agent/VariantE'

export default function AgentChatPrototypePage() {
  return (
    <Suspense fallback={<div className="h-screen bg-[#f7f4ef]" />}>
      <AgentChatPrototypeView />
    </Suspense>
  )
}

function AgentChatPrototypeView() {
  const sp = useSearchParams()
  const variant = (sp.get('variant') ?? 'D') as AgentVariantKey
  const [scope, setScope] = useState<'video' | 'kb'>('video')
  const [videoId, setVideoId] = useState(101)
  const [kbId, setKbId] = useState(1)
  const { steps, running, run, reset } = useAgentDemo(true)

  const scopeLayout = variant === 'A' ? 'tabs' : variant === 'B' ? 'pills' : 'cards'

  return (
    <div className="h-screen flex flex-col bg-[#f7f4ef] text-stone-800 overflow-hidden proto-root">
      <header className="shrink-0 bg-[#faf8f5] border-b border-stone-200 px-6 py-4">
        <div className="flex items-start gap-4 flex-wrap">
          <Link href="/prototype/dashboard" className="text-[12px] text-stone-500 hover:text-stone-800 mt-1">← 返回</Link>
          <div className="flex-1 min-w-[200px]">
            <div className="text-[10px] text-stone-400 uppercase tracking-wider">Agent 问答 UI 探索</div>
            <h1 className="text-[18px] font-semibold text-stone-900 proto-serif">思考 · 检索 · 工具 · 回答</h1>
            <p className="text-[12px] text-stone-500 mt-1 max-w-xl">
              为后续 Agent 化预留视觉方案。底部 ← → 切换三种布局；顶部可切换「单视频 / 知识库」问答范围。
            </p>
          </div>
          <div className="flex flex-col items-end gap-2">
            <DemoControls running={running} onRun={run} onReset={reset} />
            <div className="w-[280px]">
              <ScopePicker
                scope={scope}
                onScope={setScope}
                layout={scopeLayout as 'tabs' | 'pills' | 'cards'}
                videoId={videoId}
                kbId={kbId}
                onVideo={setVideoId}
                onKb={setKbId}
              />
            </div>
          </div>
        </div>
      </header>

      {variant === 'D' && <AgentVariantD steps={steps} />}
      {variant === 'E' && <AgentVariantE steps={steps} />}
      {variant === 'A' && <AgentVariantA steps={steps} />}
      {variant === 'B' && <AgentVariantB steps={steps} />}
      {variant === 'C' && <AgentVariantC steps={steps} />}

      <footer className="shrink-0 border-t border-stone-200 bg-[#faf8f5] px-6 py-3 text-[10px] text-stone-400 text-center">
        原型演示 · 不发真实请求 · 状态见左侧/内嵌/右侧工作区
      </footer>

      <div className="pb-20">
        <PrototypeSwitcher variants={[...VARIANTS]} current={variant} />
      </div>
    </div>
  )
}
