import type { ChatTraceStep } from '@/components/chat/traceTypes'

/** 把 Agent 步骤翻译成一句低语（气泡内 / 透镜底栏） */
export function traceStepToWhisper(step: ChatTraceStep): string {
  if (step.kind === 'think') return '理解你的问题…'
  if (step.kind === 'plan') return '规划检索策略…'
  if (step.kind === 'retrieve') {
    return step.query ? `在读转写，找「${step.query}」…` : '检索转写片段…'
  }
  if (step.kind === 'observe') {
    return step.status === 'running' ? '核对找到的片段够不够…' : (step.detail || '观察证据…')
  }
  if (step.kind === 'tool') {
    return step.tool ? `调用 ${step.tool}…` : '执行工具…'
  }
  if (step.kind === 'answer') return '整理成回答…'
  return step.label || '处理中…'
}
