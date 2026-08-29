'use client'

import { useState } from 'react'
import type { AgentStep } from '@/components/prototype/agent/types'
import { DEMO_USER_QUESTION } from '@/components/prototype/agent/types'
import { StepIcon, StatusDot, UserBubble, CiteChips, AnswerTypewriter } from '@/components/prototype/agent/shared'

function StepCard({ step, index }: { step: AgentStep; index: number }) {
  const active = step.status === 'running'
  const done = step.status === 'done'

  return (
    <div
      className={`relative pl-8 pb-6 proto-fade-in ${index === 0 ? '' : ''}`}
      style={{ animationDelay: `${index * 40}ms` }}
    >
      {index > 0 && <div className="absolute left-[11px] -top-6 w-px h-6 bg-stone-200" />}
      <div className={`absolute left-0 top-0 w-6 h-6 rounded-full border-2 flex items-center justify-center transition-all duration-300 ${
        active ? 'border-amber-500 bg-amber-50 scale-110' : done ? 'border-emerald-500 bg-emerald-50' : 'border-stone-200 bg-white'
      }`}>
        <StatusDot status={step.status} />
      </div>

      <div className={`rounded-xl border p-3 transition-all duration-300 ${
        active ? 'border-amber-300 bg-amber-50/50 proto-agent-glow' : done ? 'border-stone-200 bg-white' : 'border-stone-100 bg-stone-50/50 opacity-60'
      }`}>
        <div className="flex items-center gap-2 text-[11px] font-medium text-stone-700 mb-1">
          <StepIcon kind={step.kind} />
          {step.label}
          {active && <span className="text-amber-600 text-[10px] font-normal proto-agent-shimmer">处理中…</span>}
        </div>

        {step.kind === 'think' && (
          <p className={`text-[12px] text-stone-600 leading-relaxed ${active ? 'proto-agent-type' : ''}`}>
            {step.detail}
          </p>
        )}
        {step.kind === 'retrieve' && (
          <div className="space-y-2">
            <div className="text-[10px] font-mono text-stone-400">query: {step.query}</div>
            {active && <div className="h-1.5 rounded-full bg-stone-100 overflow-hidden"><div className="h-full bg-violet-500 proto-agent-scan" /></div>}
            {done && (
              <>
                <div className="text-[11px] text-emerald-700">命中 {step.hits} 个片段</div>
                <div className="flex flex-wrap gap-1">
                  {step.sources.map(s => (
                    <span key={s} className="text-[10px] px-2 py-0.5 rounded bg-violet-50 text-violet-700 border border-violet-200">{s}</span>
                  ))}
                </div>
              </>
            )}
          </div>
        )}
        {step.kind === 'tool' && (
          <div className="font-mono text-[11px] space-y-1">
            <div className="text-stone-500">{step.tool}({step.input})</div>
            {step.output && <div className="text-emerald-700 bg-emerald-50 px-2 py-1 rounded">{step.output}</div>}
          </div>
        )}
        {step.kind === 'answer' && step.status === 'running' && (
          <p className="text-[12px] text-stone-500 flex items-center gap-2">
            <span className="w-1.5 h-1.5 rounded-full bg-amber-500 proto-agent-pulse" />
            正在生成最终回答…
          </p>
        )}
        {step.kind === 'answer' && done && (
          <p className="text-[12px] text-emerald-700">回答已输出至右侧</p>
        )}
      </div>
    </div>
  )
}

export default function AgentVariantA({ steps }: { steps: AgentStep[] }) {
  const [openCites, setOpenCites] = useState<string[]>([])
  const answer = steps.find(s => s.kind === 'answer' && s.status !== 'pending')

  return (
    <div className="flex-1 flex min-h-0 gap-0">
      {/* 左：Agent 流水线 */}
      <aside className="w-[340px] shrink-0 border-r border-stone-200 bg-[#faf8f5] overflow-y-auto p-5">
        <div className="text-[10px] uppercase tracking-wider text-stone-400 mb-4">Agent 执行流水线</div>
        {steps.length === 0 ? (
          <div className="text-[12px] text-stone-400 italic">点击「重播 Agent 流程」查看动效</div>
        ) : steps.map((s, i) => <StepCard key={s.id} step={s} index={i} />)}
      </aside>

      {/* 右：对话 */}
      <main className="flex-1 overflow-y-auto p-6">
        <div className="max-w-xl mx-auto space-y-6">
          <UserBubble text={DEMO_USER_QUESTION} />
          {answer && answer.kind === 'answer' && (
            <div className="proto-fade-in space-y-2">
              <div className="text-[10px] text-stone-400 flex items-center gap-2">
                <StepIcon kind="answer" />映知 · 回答
              </div>
              <AnswerTypewriter
                content={answer.content}
                active
                cites={answer.cites}
                openCites={openCites}
                onToggleCite={id => setOpenCites(o => o.includes(id) ? o.filter(x => x !== id) : [...o, id])}
              />
            </div>
          )}
        </div>
      </main>
    </div>
  )
}
