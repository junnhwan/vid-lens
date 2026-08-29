'use client'

import { Brain, Database, Wrench, Sparkles } from 'lucide-react'
import type { ChatTraceStep } from '@/components/chat/traceTypes'

function StepIcon({ kind }: { kind: ChatTraceStep['kind'] }) {
  const cls = 'w-3.5 h-3.5'
  if (kind === 'think') return <Brain className={cls} />
  if (kind === 'retrieve') return <Database className={cls} />
  if (kind === 'tool') return <Wrench className={cls} />
  return <Sparkles className={cls} />
}

function StatusDot({ status }: { status: ChatTraceStep['status'] }) {
  if (status === 'running') return <span className="w-2 h-2 rounded-full bg-amber-500 ui-pulse" />
  if (status === 'done') return <span className="w-2 h-2 rounded-full bg-emerald-500" />
  if (status === 'error') return <span className="w-2 h-2 rounded-full bg-red-500" />
  return <span className="w-2 h-2 rounded-full bg-stone-300" />
}

function TimelineStep({ step }: { step: ChatTraceStep }) {
  if (step.status === 'pending') return null
  const active = step.status === 'running'
  return (
    <div className={`relative pl-6 pb-4 ui-fade-in ${active ? 'ui-agent-glow rounded-lg -mx-1 px-1' : ''}`}>
      <div className={`absolute left-0 top-1 w-4 h-4 rounded-full border-2 flex items-center justify-center transition-all duration-300 ${
        active ? 'border-amber-500 bg-amber-50 scale-110' :
        step.status === 'done' ? 'border-emerald-500 bg-emerald-50' :
        step.status === 'error' ? 'border-red-400 bg-red-50' : 'border-stone-200'
      }`}>
        <StatusDot status={step.status} />
      </div>
      <div className="flex items-center gap-2 text-[11px] font-medium text-stone-700">
        <StepIcon kind={step.kind} />
        {step.label}
        {active && <span className="text-[10px] text-amber-600 ui-agent-shimmer">进行中</span>}
      </div>
      {step.detail && (
        <p className={`text-[11px] mt-1 leading-relaxed ${
          step.status === 'error' ? 'text-red-700' : 'text-stone-500'
        }`}>
          {step.detail}
        </p>
      )}
      {step.kind === 'retrieve' && step.status === 'running' && (
        <div className="mt-2 h-1 rounded-full bg-stone-100 overflow-hidden">
          <div className="h-full bg-violet-500 ui-agent-scan" />
        </div>
      )}
    </div>
  )
}

export default function AgentTracePanel({
  steps,
  streaming,
  hint,
}: {
  steps: ChatTraceStep[]
  streaming?: boolean
  hint?: string
}) {
  const visible = steps.filter(s => s.status !== 'pending')

  return (
    <div className="p-4 h-full">
      <div className="flex items-center justify-between mb-3">
        <div className="text-[10px] uppercase tracking-wider text-stone-400">执行流水线</div>
        {streaming && (
          <span className="text-[10px] text-amber-700 flex items-center gap-1">
            <span className="w-1.5 h-1.5 rounded-full bg-amber-500 ui-pulse" />
            Live
          </span>
        )}
      </div>

      {visible.length === 0 ? (
        <div className="space-y-2">
          <p className="text-[12px] text-stone-400 italic">发送问题后，检索与生成步骤将显示在这里。</p>
          {hint && <p className="text-[11px] text-stone-400 leading-relaxed">{hint}</p>}
        </div>
      ) : (
        <>
          {visible.map(s => <TimelineStep key={s.id} step={s} />)}
          <p className="text-[10px] text-stone-400 mt-2 leading-relaxed border-t border-stone-200 pt-3">
            当前为 RAG 流式问答的推断步骤。完整 Agent 思考/工具链需后端流式接口，见文档。
          </p>
        </>
      )}
    </div>
  )
}
