'use client'

import { useState } from 'react'
import { ChevronDown } from 'lucide-react'
import type { AgentStep } from '@/components/prototype/agent/types'
import { DEMO_RESEARCH_QUESTION } from '@/components/prototype/agent/types'
import { stepToWhisper } from '@/components/prototype/agent/stepNarrative'
import { StepIcon, UserBubble, AnswerTypewriter, AgentMark } from '@/components/prototype/agent/shared'

function ProcessDetail({ steps }: { steps: AgentStep[] }) {
  const visible = steps.filter(s => s.kind !== 'answer' && s.status !== 'pending')
  if (visible.length === 0) return null
  return (
    <div className="pt-3 border-t border-ink-0/8 space-y-2">
      {visible.map(step => (
        <div key={step.id} className="flex items-start gap-2 text-[11px] text-ink-3">
          <StepIcon kind={step.kind} className="w-3 h-3 mt-0.5 shrink-0 opacity-60" />
          <div className="min-w-0">
            <span className="text-ink-2">{step.label}</span>
            {step.kind === 'retrieve' && step.status === 'done' && (
              <span className="text-ink-4"> · {step.hits} 段</span>
            )}
            {step.kind === 'plan' && step.replan && (
              <span className="text-sienna-700 ml-1">replan</span>
            )}
            {(step.kind === 'plan' || step.kind === 'observe' || step.kind === 'think') && 'detail' in step && (
              <p className="text-[10px] text-ink-4 mt-0.5 line-clamp-2">{step.detail}</p>
            )}
          </div>
        </div>
      ))}
    </div>
  )
}

/**
 * L：对话主场 — 85%+ 视口给问答线程；研究过程默认隐藏，仅在气泡内按需展开。
 */
export function AgentVariantL({ steps }: { steps: AgentStep[] }) {
  const [openCites, setOpenCites] = useState<string[]>([])
  const [showProcess, setShowProcess] = useState(false)

  const running = steps.find(s => s.status === 'running')
  const answer = steps.find(s => s.kind === 'answer')
  const processSteps = steps.filter(s => s.kind !== 'answer')
  const doneProcessCount = processSteps.filter(s => s.status === 'done').length
  const isThinking = Boolean(running && running.kind !== 'answer')
  const hasAnswer = answer?.kind === 'answer' && answer.status !== 'pending'

  return (
    <div className="flex-1 flex flex-col min-h-0">
      {/* 对话主区：占满剩余高度 */}
      <div className="flex-1 overflow-y-auto">
        <div className="px-6 pt-10 pb-8">
          <div className="w-full max-w-2xl mx-auto flex flex-col gap-6">
            <UserBubble text={DEMO_RESEARCH_QUESTION} />

            <div className="w-full">
              <div className="flex items-center gap-2 mb-2 text-[10px] text-ink-4">
                <AgentMark />
                映知 Agent
              </div>

              <div
                className={`proto-chat-bubble rounded-2xl rounded-tl-md px-5 py-4 min-h-[7.2em] transition-colors duration-200 ${
                  isThinking ? 'ring-1 ring-sienna-500/25 bg-sienna-500/5' :
                  'ring-1 ring-ink-0/8 bg-paper-0'
                }`}
              >
                <div className="relative min-h-[7.2em]">
                  {isThinking && running && (
                    <p className="absolute inset-x-0 top-0 text-[14px] text-ink-3 flex items-center gap-2 z-[1]">
                      <span className="inline-flex gap-0.5">
                        {[0, 1, 2].map(i => (
                          <span
                            key={i}
                            className="w-1 h-1 rounded-full bg-ink-4 proto-agent-pulse-opacity"
                            style={{ animationDelay: `${i * 0.12}s` }}
                          />
                        ))}
                      </span>
                      {stepToWhisper(running)}
                    </p>
                  )}

                  {hasAnswer && answer.kind === 'answer' && (
                    <AnswerTypewriter
                      content={answer.content}
                      active={answer.status === 'running' || answer.status === 'done'}
                      cites={answer.cites}
                      openCites={openCites}
                      onToggleCite={id => setOpenCites(o => o.includes(id) ? o.filter(x => x !== id) : [...o, id])}
                      className="text-[16px] leading-[1.8] text-ink-0"
                    />
                  )}

                  {!isThinking && !hasAnswer && (
                    <p className="text-[14px] text-ink-5">…</p>
                  )}
                </div>

                {doneProcessCount > 0 && (
                  <div className="mt-3">
                    <button
                      type="button"
                      onClick={() => setShowProcess(v => !v)}
                      className="text-[10px] text-ink-4 hover:text-ink-2 flex items-center gap-1 transition-colors proto-btn-lift"
                    >
                      <ChevronDown className={`w-3 h-3 proto-chevron ${showProcess ? 'rotate-180' : ''}`} />
                      {showProcess ? '收起过程' : `查看过程（${doneProcessCount} 步）`}
                    </button>
                    <div className="proto-acc" data-open={showProcess ? 'true' : 'false'}>
                      <div className="proto-acc-inner">
                        <ProcessDetail steps={steps} />
                      </div>
                    </div>
                  </div>
                )}
              </div>
            </div>
          </div>
        </div>
      </div>

      {/* 输入区：与正式聊天页同比例 */}
      <div className="shrink-0 border-t border-ink-0/8 bg-paper-0/80 px-6 py-4">
        <div className="max-w-2xl mx-auto">
          <div className="rounded-xl border border-ink-0/10 bg-paper-0 px-4 py-3 text-[13px] text-ink-5">
            继续追问…（原型演示，输入不可用）
          </div>
          <p className="text-[10px] text-ink-5 mt-2 text-center">
            对话区占主视口 · 研究过程默认折叠，不抢空间
          </p>
        </div>
      </div>
    </div>
  )
}
