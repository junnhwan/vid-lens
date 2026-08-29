'use client'

import { useState } from 'react'
import type { AgentStep } from '@/components/prototype/agent/types'
import { DEMO_USER_QUESTION } from '@/components/prototype/agent/types'
import { StepIcon, StatusDot, UserBubble, CiteChips, Collapse, AnswerTypewriter } from '@/components/prototype/agent/shared'

function InlineStep({ step }: { step: AgentStep }) {
  const active = step.status === 'running'
  const done = step.status === 'done'
  if (step.status === 'pending') return null

  return (
    <div className={`flex items-start gap-2 py-1.5 proto-fade-in ${active ? 'proto-agent-glow rounded-lg px-2 -mx-2' : ''}`}>
      <div className="mt-0.5"><StatusDot status={step.status} /></div>
      <div className="flex-1 min-w-0">
        <div className="flex items-center gap-2 text-[11px] text-stone-600">
          <StepIcon kind={step.kind} className="w-3 h-3" />
          <span className="font-medium">{step.label}</span>
          {active && <span className="text-amber-600 proto-agent-shimmer text-[10px]">…</span>}
        </div>
        {step.kind === 'think' && done && (
          <p className="text-[11px] text-stone-500 mt-0.5 line-clamp-2">{step.detail}</p>
        )}
        {step.kind === 'retrieve' && (active || done) && (
          <div className="mt-1">
            {active && <div className="h-1 rounded-full bg-stone-100 overflow-hidden"><div className="h-full w-1/3 bg-violet-500 proto-agent-scan" /></div>}
            {done && <span className="text-[10px] text-violet-600">{step.hits} 片段 · {step.sources.join('、')}</span>}
          </div>
        )}
        {step.kind === 'tool' && done && (
          <div className="text-[10px] font-mono text-stone-500 mt-0.5">{step.tool} → {step.output}</div>
        )}
      </div>
    </div>
  )
}

export default function AgentVariantB({ steps }: { steps: AgentStep[] }) {
  const [openCites, setOpenCites] = useState<string[]>([])
  const [showTrace, setShowTrace] = useState(true)
  const [answerTyping, setAnswerTyping] = useState(false)
  const answer = steps.find(s => s.kind === 'answer')
  const visibleSteps = steps.filter(s => s.kind !== 'answer')
  const hasActivity = visibleSteps.some(s => s.status !== 'pending')

  return (
    <main className="flex-1 overflow-y-auto p-6">
      <div className="max-w-2xl mx-auto space-y-6">
        <UserBubble text={DEMO_USER_QUESTION} />

        <div className="space-y-3 proto-fade-in">
          <div className="flex items-center gap-2 text-[10px] text-stone-400">
            <StepIcon kind="answer" />映知 Agent
            {steps.some(s => s.status === 'running') && (
              <span className="flex items-center gap-1 text-amber-700">
                <span className="w-1.5 h-1.5 rounded-full bg-amber-500 proto-agent-pulse" />执行中
              </span>
            )}
          </div>

          {hasActivity && (
            <Collapse title={`思考与工具轨迹 (${visibleSteps.filter(s => s.status === 'done').length}/${visibleSteps.length})`} open={showTrace} onToggle={() => setShowTrace(s => !s)} accent="border-stone-200 bg-stone-50/80">
              <div className="space-y-0.5 pt-1">
                {visibleSteps.map(s => <InlineStep key={s.id} step={s} />)}
              </div>
            </Collapse>
          )}

          {answer?.kind === 'answer' && answer.status !== 'pending' && (
            <div className={`rounded-2xl border p-4 transition-all duration-300 ${
              answer.status === 'running' || answerTyping ? 'border-amber-200 bg-amber-50/30' : 'border-stone-200 bg-white'
            }`}>
              <AnswerTypewriter
                content={answer.content}
                active={answer.status === 'running' || answer.status === 'done'}
                cites={answer.cites}
                openCites={openCites}
                onToggleCite={id => setOpenCites(o => o.includes(id) ? o.filter(x => x !== id) : [...o, id])}
                onTypingChange={setAnswerTyping}
              />
            </div>
          )}
        </div>
      </div>
    </main>
  )
}
