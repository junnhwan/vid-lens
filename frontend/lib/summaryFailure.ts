import { TaskStatusEnum, type VideoTask } from './types.ts'

type SummaryTask = Pick<VideoTask, 'status' | 'stage' | 'last_job_type' | 'last_error_code' | 'last_error_msg' | 'error_msg' | 'retry_count' | 'max_retries' | 'next_retry_at' | 'trace_id' | 'id'>

export interface SummaryFailureView {
  category: string
  advice: string
  retry: string
  diagnosticId: string
  scheduled: boolean
}

export function summaryFailureView(task: SummaryTask): SummaryFailureView | null {
  if (task.last_job_type !== 'analyze' || task.stage !== 'summarizing' ||
      (task.status !== TaskStatusEnum.Failed && task.status !== TaskStatusEnum.Dead)) return null

  // Older rows stored only retryable_error/retry_exhausted. Read their known
  // provider envelope without ever showing the raw message to users.
  const raw = task.last_error_msg || task.error_msg || ''
  const code = ['retryable_error', 'retry_exhausted', 'non_retryable_error', ''].includes(task.last_error_code)
    ? raw.match(/failed:\s*(network|timeout|provider_5xx|rate_limited|auth|invalid_request|non_retryable)\s*\(/)?.[1] || task.last_error_code
    : task.last_error_code
  const status = raw.match(/status=(\d{3})/)?.[1]
  const [category, advice] = (() => {
    switch (code) {
      case 'network': return ['网络连接失败', '检查本机到模型服务的网络连接和服务地址；若同一模型连续失败，请先确认链路恢复。']
      case 'timeout': return ['请求超时', '检查模型服务或中间网关的超时设置与运行状态，稍后再试。']
      case 'provider_5xx': return [status === '504' ? '模型服务或网关超时（504）' : '模型服务暂时不可用', '查看模型服务或中间网关状态；若多个视频持续失败，先暂停手动重试。']
      case 'rate_limited': return ['模型服务限流（429）', '等待自动重试或供应商额度恢复；请勿连续手动提交。']
      case 'auth': return ['模型服务鉴权失败', '检查模型配置中的密钥和权限，然后重新提交。']
      case 'invalid_request': return ['模型服务拒绝请求', '检查模型配置与所选模型是否匹配；可凭诊断编号排查请求。']
      default: return ['摘要生成失败', '查看模型配置与后台日志；凭诊断编号排查后再重试。']
    }
  })()
  const scheduled = task.status === TaskStatusEnum.Failed && !!task.next_retry_at
  const retry = task.status === TaskStatusEnum.Dead
    ? `自动重试已耗尽（已失败 ${task.retry_count} 次，最多重试 ${task.max_retries} 次）。`
    : scheduled
      ? `已安排第 ${task.retry_count}/${task.max_retries} 次自动重试；下次：${new Date(task.next_retry_at!).toLocaleString()}。`
      : '自动重试未安排，请检查原因后手动重新提交。'
  return { category, advice, retry, diagnosticId: task.trace_id || `task-${task.id}`, scheduled }
}
