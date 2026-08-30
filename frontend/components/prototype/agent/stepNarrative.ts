import type { AgentStep } from '@/components/prototype/agent/types'

/** 把技术步骤翻译成一句人话（叙事/低语用） */
export function stepToWhisper(step: AgentStep): string {
  if (step.kind === 'think') return '理解你的问题…'
  if (step.kind === 'plan') {
    return step.replan ? '换个思路再搜一轮…' : '想好从哪几段转写入手…'
  }
  if (step.kind === 'retrieve') return `在读转写，找「${step.query}」…`
  if (step.kind === 'observe') {
    if (step.status === 'running') return '核对找到的片段够不够…'
    return step.detail.includes('不足') ? '发布会那段有了，还差访谈里的内容…' : '材料够了，可以写结论了'
  }
  if (step.kind === 'tool') return '把各段内容对齐比较…'
  if (step.kind === 'answer') return '整理成回答…'
  return '处理中…'
}

export function stepToNarrative(step: AgentStep): string {
  if (step.kind === 'think') return '这个问题需要跨几段转写对比，我先理一下思路。'
  if (step.kind === 'plan') {
    return step.replan
      ? '第一轮只覆盖了发布会，访谈里关于工具链的说法还没捞到——我换关键词再搜一次。'
      : '我先从发布会和访谈里，把所有提到「产品」和「卖点」的段落捞出来。'
  }
  if (step.kind === 'retrieve') {
    const where = step.sources?.join('、') || '转写'
    return `刚翻了「${where}」，用「${step.query}」搜到 ${step.hits} 段相关内容。`
  }
  if (step.kind === 'observe') {
    if ('detail' in step && step.detail.includes('不足')) {
      return '目前只有发布会的说法，访谈视角还缺一块——证据不够，不能急着下结论。'
    }
    return '三款产品在两个来源里都有覆盖了，可以开始写对比。'
  }
  if (step.kind === 'tool') return '我把各段按产品归了类，对比它们在不同场合被强调的侧重点。'
  return ''
}

export function visibleNarratives(steps: AgentStep[]): { id: string; text: string; running?: boolean }[] {
  const out: { id: string; text: string; running?: boolean }[] = []
  for (const step of steps) {
    if (step.kind === 'answer') continue
    if (step.status === 'pending') continue
    const text = stepToNarrative(step)
    if (!text) continue
    out.push({ id: step.id, text, running: step.status === 'running' })
  }
  return out
}
