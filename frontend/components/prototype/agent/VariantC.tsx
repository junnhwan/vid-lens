'use client'

import { useState } from 'react'
import type { AgentStep } from '@/components/prototype/agent/types'
import { DEMO_USER_QUESTION } from '@/components/prototype/agent/types'
import { StepIcon, StatusDot, UserBubble, CiteChips, AnswerTypewriter } from '@/components/prototype/agent/shared'

function FeedItem({ step }: { step: AgentStep }) {
  if (step.status === 'pending') return null
  const active = step.status === 'running'

  const bg = step.kind === 'think' ? 'from-amber-50 to-orange-50 border-amber-200'
    : step.kind === 'retrieve' ? 'from-violet-50 to-indigo-50 border-violet-200'
    : step.kind === 'tool' ? 'from-sky-50 to-cyan-50 border-sky-200'
    : 'from-emerald-50 to-teal-50 border-emerald-200'

  return (
    <div className={`rounded-xl border bg-gradient-to-br p-3 proto-fade-in proto-slide-in-right ${bg} ${active ? 'proto-agent-glow' : ''}`}>
      <div className="flex items-center justify-between mb-2">
        <div className="flex items-center gap-2 text-[11px] font-medium text-stone-700">
          <StepIcon kind={step.kind} />
          {step.label}
        </div>
        <StatusDot status={step.status} />
      </div>

      {step.kind === 'think' && (
        <p className={`text-[12px] text-stone-600 leading-relaxed ${active ? 'proto-agent-type' : ''}`}>{step.detail}</p>
      )}
      {step.kind === 'retrieve' && (
        <div className="space-y-2">
          <div className="text-[10px] font-mono text-violet-600 bg-white/60 px-2 py-1 rounded">{step.query}</div>
          {active ? (
            <div className="space-y-1">
              {[1, 2, 3].map(i => (
                <div key={i} className="h-6 rounded bg-white/50 proto-agent-shimmer" style={{ animationDelay: `${i * 0.2}s`, width: `${90 - i * 15}%` }} />
              ))}
            </div>
          ) : (
            <div className="grid grid-cols-2 gap-1">
              {step.sources.map(s => (
                <div key={s} className="text-[10px] px-2 py-1 rounded bg-white/70 text-violet-800 truncate">{s}</div>
              ))}
            </div>
          )}
        </div>
      )}
      {step.kind === 'tool' && (
        <div className="font-mono text-[10px] space-y-1">
          <div className="text-stone-500">$ {step.tool} {step.input}</div>
          {step.output && <div className="text-emerald-800 bg-white/60 px-2 py-1 rounded">← {step.output}</div>}
        </div>
      )}
      {step.kind === 'answer' && step.status === 'done' && (
        <div className="text-[12px] text-stone-700">回答已生成，见左侧对话区</div>
      )}
    </div>
  )
}

export default function AgentVariantC({ steps }: { steps: AgentStep[] }) {
  const [openCites, setOpenCites] = useState<string[]>([])
  const answer = steps.find(s => s.kind === 'answer' && s.status !== 'pending')

  return (
    <div className="flex-1 flex min-h-0">
      <main className="w-[52%] border-r border-stone-200 overflow-y-auto p-5">
        <div className="max-w-lg mx-auto space-y-5">
          <UserBubble text={DEMO_USER_QUESTION} />
          {answer && answer.kind === 'answer' && (
            <div className="proto-fade-in">
              <div className="text-[10px] text-stone-400 mb-2">映知 · 严格 RAG</div>
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

      <aside className="flex-1 bg-gradient-to-b from-[#faf8f5] to-stone-100 overflow-y-auto p-5">
        <div className="text-[10px] uppercase tracking-wider text-stone-400 mb-3 flex items-center justify-between">
          <span>Agent 工作区</span>
          {steps.some(s => s.status === 'running') && (
            <span className="text-amber-600 flex items-center gap-1 normal-case">
              <span className="w-1.5 h-1.5 rounded-full bg-amber-500 proto-agent-pulse" />Live
            </span>
          )}
        </div>
        <div className="space-y-3">
          {steps.length === 0 ? (
            <div className="text-[12px] text-stone-400 italic py-8 text-center border border-dashed border-stone-300 rounded-xl">
              工作区将实时展示思考、检索与工具调用
            </div>
          ) : steps.map(s => <FeedItem key={s.id} step={s} />)}
        </div>
      </aside>
    </div>
  )
}
