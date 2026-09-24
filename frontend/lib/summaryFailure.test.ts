import { test } from 'node:test'
import assert from 'node:assert/strict'
import { summaryFailureView } from './summaryFailure.ts'
import { TaskStatusEnum, type VideoTask } from './types.ts'

function task(overrides: Partial<VideoTask> = {}): VideoTask {
  return {
    id: 2, status: TaskStatusEnum.Failed, stage: 'summarizing', last_job_type: 'analyze',
    last_error_code: 'retryable_error', last_error_msg: 'AI 总结失败: provider openai_compatible chat failed: provider_5xx (status=504 request_id=)',
    error_msg: '', retry_count: 1, max_retries: 3, trace_id: 'trace-2', next_retry_at: '2026-09-24T02:00:00Z',
    ...overrides,
  } as VideoTask
}

test('legacy 504 is categorized with scheduled retry and diagnostic ID', () => {
  const view = summaryFailureView(task())!
  assert.match(view.category, /504/)
  assert.match(view.retry, /第 1\/3 次自动重试/)
  assert.equal(view.diagnosticId, 'trace-2')
  assert.equal(view.scheduled, true)
})

test('network failure after exhaustion does not advertise another automatic retry', () => {
  const view = summaryFailureView(task({ status: TaskStatusEnum.Dead, last_error_code: 'network', retry_count: 4, next_retry_at: undefined }))!
  assert.equal(view.category, '网络连接失败')
  assert.match(view.retry, /已耗尽/)
  assert.equal(view.scheduled, false)
})

test('unrelated task failures are left alone', () => {
  assert.equal(summaryFailureView(task({ last_job_type: 'transcribe' })), null)
})
