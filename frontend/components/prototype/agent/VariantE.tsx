'use client'

import { useEffect, useMemo, useState } from 'react'
import type { AgentStep } from '@/components/prototype/agent/types'
import { DEMO_USER_QUESTION } from '@/components/prototype/agent/types'
import { StepIcon, StatusDot, UserBubble, CiteChips, AnswerTypewriter } from '@/components/prototype/agent/shared'

const PHASES: { key: string; label: string; kinds: AgentStep['kind'][] }[] = [
  { key: 'think', label: '理解', kinds: ['think'] },
  { key: 'retrieve', label: '检索', kinds: ['retrieve'] },
  { key: 'tool', label: '工具', kinds: ['tool'] },
  { key: 'answer', label: '回答', kinds: ['answer'] },
]

function phaseStatus(steps: AgentStep[], kinds: AgentStep['kind'][]) {
  const related = steps.filter(s => kinds.includes(s.kind))
  if (related.some(s => s.status === 'running')) return 'running'
  if (related.some(s => s.status === 'done')) return 'done'
  return 'pending'
}

/** E：底部阶段地铁条 + 动效（进度填充、节点脉冲、详情滑入、自动跟随当前步骤） */
export function AgentVariantE({ steps }: { steps: AgentStep[] }) {
  const [openCites, setOpenCites] = useState<string[]>([])
  const [expandedPhase, setExpandedPhase] = useState<string | null>(null)
  const [userPicked, setUserPicked] = useState(false)

  const answer = steps.find(s => s.kind === 'answer' && s.status !== 'pending')
  const runningStep = steps.find(s => s.status === 'running')

  const runningPhaseKey = useMemo(() => {
    if (!runningStep) return null
    return PHASES.find(p => p.kinds.includes(runningStep.kind))?.key ?? null
  }, [runningStep])

  // 自动展开当前进行中的阶段（用户手动点后不再抢焦点）
  useEffect(() => {
    if (userPicked || !runningPhaseKey) return
    setExpandedPhase(runningPhaseKey)
  }, [runningPhaseKey, userPicked])

  const doneCount = PHASES.filter(p => phaseStatus(steps, p.kinds) === 'done').length
  const progressPct = Math.min(100, (doneCount / PHASES.length) * 100 + (runningStep ? 8 : 0))

  const activeStep = expandedPhase
    ? steps.find(s => PHASES.find(p => p.key === expandedPhase)?.kinds.includes(s.kind))
    : null

  return (
    <div className="flex-1 flex flex-col min-h-0">
      <main className="flex-1 overflow-y-auto p-6">
        <div className="max-w-2xl mx-auto space-y-6">
          <UserBubble text={DEMO_USER_QUESTION} />

          {steps.some(s => s.status === 'running') && !answer && (
            <div className="flex items-center gap-2 text-[12px] text-stone-500 proto-fade-in">
              <span className="flex gap-1">
                {[0, 1, 2].map(i => (
                  <span key={i} className="w-1.5 h-1.5 rounded-full bg-amber-500 proto-agent-bounce" style={{ animationDelay: `${i * 0.12}s` }} />
                ))}
              </span>
              Agent 正在{runningStep?.label ?? '处理'}…
            </div>
          )}

          {answer && answer.kind === 'answer' && (
            <div className="proto-fade-in">
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

      <div className="shrink-0 border-t border-stone-200 bg-gradient-to-t from-[#faf8f5] to-[#f7f4ef] px-6 py-4 proto-metro-bar-in">
        <div className="max-w-2xl mx-auto">
          {/* 总进度条 */}
          <div className="h-1 rounded-full bg-stone-200 overflow-hidden mb-4">
            <div
              className="h-full bg-gradient-to-r from-amber-400 to-amber-600 proto-metro-progress transition-all duration-500 ease-out"
              style={{ width: `${progressPct}%` }}
            />
          </div>

          <div className="flex items-start justify-between mb-2">
            {PHASES.map((p, i) => {
              const st = phaseStatus(steps, p.kinds)
              const on = expandedPhase === p.key
              const isRunning = st === 'running'
              const isDone = st === 'done'
              const prevDone = i === 0 || phaseStatus(steps, PHASES[i - 1].kinds) === 'done' || phaseStatus(steps, PHASES[i - 1].kinds) === 'running'

              return (
                <button
                  key={p.key}
                  onClick={() => {
                    setUserPicked(true)
                    setExpandedPhase(on ? null : p.key)
                  }}
                  className="flex-1 flex flex-col items-center gap-1.5 group min-w-0"
                >
                  <div className="flex items-center w-full px-0.5">
                    {/* 左连接线 */}
                    {i > 0 ? (
                      <div className="flex-1 h-0.5 bg-stone-200 overflow-hidden rounded-full mx-0.5">
                        <div
                          className={`h-full bg-amber-400 transition-all duration-700 ease-out ${
                            prevDone || isDone || isRunning ? 'w-full proto-metro-line-fill' : 'w-0'
                          }`}
                        />
                      </div>
                    ) : (
                      <div className="flex-1" />
                    )}

                    <div className="relative shrink-0">
                      {isRunning && <span className="absolute inset-0 rounded-full bg-amber-400/30 proto-metro-ripple" />}
                      <div
                        className={`relative w-9 h-9 rounded-full border-2 flex items-center justify-center text-[10px] font-semibold transition-all duration-300 ${
                          isRunning ? 'border-amber-500 bg-amber-50 text-amber-900 scale-110 proto-agent-glow proto-metro-node-pop' :
                          isDone ? 'border-emerald-500 bg-emerald-50 text-emerald-800 proto-metro-node-pop' :
                          on ? 'border-stone-500 bg-white text-stone-700 scale-105' :
                          'border-stone-200 bg-white text-stone-400 group-hover:border-stone-300'
                        }`}
                      >
                        {isDone ? '✓' : i + 1}
                      </div>
                    </div>

                    {/* 右连接线 */}
                    {i < PHASES.length - 1 ? (
                      <div className="flex-1 h-0.5 bg-stone-200 overflow-hidden rounded-full mx-0.5">
                        <div
                          className={`h-full bg-amber-400 transition-all duration-700 ease-out ${
                            isDone ? 'w-full proto-metro-line-fill' : 'w-0'
                          }`}
                          style={{ transitionDelay: isDone ? '0.15s' : '0s' }}
                        />
                      </div>
                    ) : (
                      <div className="flex-1" />
                    )}
                  </div>
                  <span className={`text-[10px] truncate transition-colors ${on || isRunning ? 'text-stone-900 font-medium' : 'text-stone-400'}`}>
                    {p.label}
                  </span>
                </button>
              )
            })}
          </div>

          {activeStep && activeStep.status !== 'pending' && (
            <div key={activeStep.id + activeStep.status} className="mt-3 p-3 rounded-xl border border-stone-200 bg-white text-[12px] text-stone-600 shadow-sm proto-metro-panel-in">
              <div className="flex items-center gap-2 text-[11px] font-medium text-stone-700 mb-2">
                <StepIcon kind={activeStep.kind} />
                {activeStep.label}
                <StatusDot status={activeStep.status} />
                {activeStep.status === 'running' && <span className="text-[10px] text-amber-600 proto-agent-shimmer ml-1">进行中</span>}
              </div>
              {activeStep.kind === 'think' && (
                <p className={activeStep.status === 'running' ? 'proto-agent-type' : ''}>{activeStep.detail}</p>
              )}
              {activeStep.kind === 'retrieve' && (
                <div className="space-y-2">
                  <p className="font-mono text-[10px] text-violet-600">query: {activeStep.query}</p>
                  {activeStep.status === 'running' && (
                    <div className="h-1.5 rounded-full bg-stone-100 overflow-hidden">
                      <div className="h-full bg-violet-500 proto-agent-scan" />
                    </div>
                  )}
                  {activeStep.status === 'done' && (
                    <p className="text-emerald-700 proto-fade-in">命中 {activeStep.hits} 片段 · {activeStep.sources.join('、')}</p>
                  )}
                </div>
              )}
              {activeStep.kind === 'tool' && (
                <div className="font-mono text-[11px] space-y-1">
                  <div className="text-stone-500">{activeStep.tool}({activeStep.input})</div>
                  {activeStep.output && <div className="text-emerald-700 bg-emerald-50 px-2 py-1 rounded proto-fade-in">{activeStep.output}</div>}
                </div>
              )}
              {activeStep.kind === 'answer' && activeStep.status === 'running' && (
                <div className="flex items-center gap-2 text-stone-500">
                  <span className="proto-agent-shimmer">正在生成回答</span>
                  <span className="flex gap-1">
                    {[0, 1, 2].map(i => <span key={i} className="w-1 h-1 rounded-full bg-stone-400 proto-agent-bounce" style={{ animationDelay: `${i * 0.1}s` }} />)}
                  </span>
                </div>
              )}
              {activeStep.kind === 'answer' && activeStep.status === 'done' && (
                <p className="text-emerald-700">回答已生成 ↑</p>
              )}
            </div>
          )}
        </div>
      </div>
    </div>
  )
}
