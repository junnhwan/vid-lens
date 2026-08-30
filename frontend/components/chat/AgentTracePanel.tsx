'use client'

import { Brain, Database, Wrench, Sparkles } from 'lucide-react'
import type { ChatTraceStep, TracePanelSource } from '@/components/chat/traceTypes'

function StepIcon({ kind }: { kind: ChatTraceStep['kind'] }) {
  const cls = 'w-3.5 h-3.5'
  if (kind === 'think') return <Brain className={cls} />
  if (kind === 'retrieve') return <Database className={cls} />
  if (kind === 'tool' || kind === 'plan' || kind === 'observe') return <Wrench className={cls} />
  return <Sparkles className={cls} />
}

function StatusDot({ status }: { status: ChatTraceStep['status'] }) {
  if (status === 'running') return <span className="w-2 h-2 rounded-full bg-sienna-500 ui-agent-pulse-opacity" />
  if (status === 'done') return <span className="w-2 h-2 rounded-full bg-moss" />
  if (status === 'error') return <span className="w-2 h-2 rounded-full bg-rust" />
  return <span className="w-2 h-2 rounded-full bg-paper-3" />
}

function TimelineStep({ step }: { step: ChatTraceStep }) {
  if (step.status === 'pending') return null
  const active = step.status === 'running'
  return (
    <div className={`relative pl-6 pb-4 ui-fade-in ${active ? 'ui-agent-glow-lens rounded-lg -mx-1 px-1' : ''}`}>
      <div className={`absolute left-0 top-1 w-4 h-4 rounded-full border-2 flex items-center justify-center transition-colors duration-150 ${
        active ? 'border-sienna-500 bg-sienna-500/10' :
        step.status === 'done' ? 'border-moss bg-moss/10' :
        step.status === 'error' ? 'border-rust bg-rust/10' : 'border-ink-0/15'
      }`}>
        <StatusDot status={step.status} />
      </div>
      <div className="flex items-center gap-2 text-[11px] font-medium text-ink-1">
        <StepIcon kind={step.kind} />
        {step.label}
        {active && <span className="text-[10px] text-sienna-700 ui-agent-shimmer">进行中</span>}
      </div>
      {step.detail && (
        <p className={`text-[11px] mt-1 leading-relaxed ${step.status === 'error' ? 'text-rust' : 'text-ink-3'}`}>
          {step.detail}
        </p>
      )}
      {step.tool && (
        <p className="text-[10px] font-mono text-ink-4 mt-1 truncate" title={step.tool}>
          {step.tool}
          {step.toolInput ? ` · ${step.toolInput}` : ''}
        </p>
      )}
      {step.toolOutput && step.status === 'done' && (
        <p className="text-[10px] text-moss mt-1 line-clamp-2">{step.toolOutput}</p>
      )}
      {step.durationMs != null && step.status === 'done' && (
        <p className="text-[9px] text-ink-4 mt-0.5">{step.durationMs}ms</p>
      )}
      {step.kind === 'retrieve' && step.status === 'running' && (
        <div className="mt-2 h-1 rounded-full bg-paper-2 overflow-hidden">
          <div className="h-full bg-sienna-500 ui-agent-scan" />
        </div>
      )}
    </div>
  )
}

const SOURCE_HINT: Record<TracePanelSource, string> = {
  agent: '步骤来自 Agent 流式事件（工具调用与检索摘要）。',
  inferred: '步骤根据 RAG 流式事件推断，仅含检索与生成两步。',
  legacy: '历史消息从快照恢复的执行摘要。',
}

export default function AgentTracePanel({
  steps,
  streaming,
  source = 'inferred',
  error,
  emptyHint,
}: {
  steps: ChatTraceStep[]
  streaming?: boolean
  source?: TracePanelSource
  error?: string
  emptyHint?: string
}) {
  const visible = steps.filter(s => s.status !== 'pending')

  return (
    <div className="p-4 h-full bg-paper-0/70">
      <div className="flex items-center justify-between mb-3 gap-2">
        <div className="text-[10px] uppercase tracking-wider text-ink-4">执行流水线</div>
        {streaming && (
          <span className="text-[10px] text-sienna-700 flex items-center gap-1 shrink-0">
            <span className="w-1.5 h-1.5 rounded-full bg-sienna-500 ui-agent-pulse-opacity" />
            Live
          </span>
        )}
      </div>

      {error && (
        <div className="mb-3 p-2.5 rounded-lg bg-rust/10 border border-rust/20 text-[11px] text-rust">
          {error}
        </div>
      )}

      {visible.length === 0 ? (
        <div className="space-y-2">
          <p className="text-[12px] text-ink-4 italic">
            {streaming ? '等待 Agent 返回步骤…' : (emptyHint || '发送问题后，执行步骤将显示在这里。')}
          </p>
        </div>
      ) : (
        visible.map(s => <TimelineStep key={s.id} step={s} />)
      )}

      <p className="text-[10px] text-ink-4 mt-2 leading-relaxed border-t border-ink-0/8 pt-3">
        {SOURCE_HINT[source]}
      </p>
    </div>
  )
}
