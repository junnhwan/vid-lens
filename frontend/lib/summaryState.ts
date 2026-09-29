import { TaskStatusEnum, type VideoTask } from './types.ts'

export function summaryRunning(task: VideoTask): boolean {
  const job = task.summary_job
  if (job) return job.status === TaskStatusEnum.Queued || job.status === TaskStatusEnum.Running
  return task.last_job_type === 'analyze' && (task.status === TaskStatusEnum.Queued || task.status === TaskStatusEnum.Running)
}

export function canGenerateSummary(task: VideoTask): boolean {
  if (summaryRunning(task)) return false
  return task.can_summarize ?? (task.has_transcription && task.status !== TaskStatusEnum.Queued && task.status !== TaskStatusEnum.Running)
}

export function summaryStatusText(task: VideoTask): string {
  const status = task.summary_job?.status ?? task.status
  return status === TaskStatusEnum.Queued ? '摘要已排队' : '正在生成摘要'
}
