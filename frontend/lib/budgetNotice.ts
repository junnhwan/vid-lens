import type { ProgressEvent } from './conversationStream.ts'

export interface BudgetNotice { dimension: string; used: number; estimated_next: number; reserve: number; limit: number; usage_source: string }
export function budgetProgress(stopReason?: string, notice?: BudgetNotice): ProgressEvent | undefined {
  if (!notice && !stopReason?.includes('budget')) return undefined
  const dimensions: Record<string, string> = { tool_calls: '工具调用', llm_calls: '模型调用', input_tokens: '输入 Token', output_tokens: '输出 Token', prompt_tokens: '输入 Token', completion_tokens: '输出 Token', duration_ms: '执行时间（毫秒）', context_chars: '上下文字符' }
  const sources: Record<string, string> = { actual: '模型接口返回', estimated: '估算', mixed: '包含估算', unknown: '用量未知' }
  return { id: 'budget', kind: 'budget', label: '预算限制收尾', status: 'done', detail: notice ? `${dimensions[notice.dimension] || '执行额度'}：已用 ${notice.used}，下一步估算 ${notice.estimated_next}，收尾预留 ${notice.reserve}，上限 ${notice.limit}（${sources[notice.usage_source] || '用量未知'}）` : '本轮已达到预算限制，请留意回答中的缺失信息。' }
}
