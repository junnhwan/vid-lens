import { expect, test } from 'vitest'
import { TaskStatusEnum, type VideoTask } from '@/lib/types'
import { taskCategory, taskStateView, taskNeedsPolling } from '@/lib/taskStatus'

const base = {
  status: TaskStatusEnum.Completed, stage: 'none', has_transcription: true, visual_status: 'off',
  summary_job: { id: 10, job_type: 'summary', status: TaskStatusEnum.Queued, stage: 'summarizing', retry_count: 0, max_retries: 3, last_error_code: '', last_error_msg: '', trace_id: 'summary-run' },
} as VideoTask

test('completed parent with independent summary work shows active and failed child state', () => {
  expect(taskCategory(base)).toBe('processing')
  expect(taskStateView(base).text).toContain('摘要')
  expect(taskNeedsPolling(base)).toBe(true)
  const failed = { ...base, summary_job: { ...base.summary_job!, status: TaskStatusEnum.Failed, last_error_code: 'non_retryable_error' } }
  expect(taskCategory(failed)).toBe('failed')
  expect(taskStateView(failed).text).toContain('摘要')
  expect(taskNeedsPolling(failed)).toBe(false)
})
