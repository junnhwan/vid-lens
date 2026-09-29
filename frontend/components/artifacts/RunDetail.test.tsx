// @vitest-environment jsdom
import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, expect, test } from 'vitest'
import { runSchema } from '@/lib/artifacts/schema'
import { RunProgress } from './RunDetail'

afterEach(cleanup)
test('a budget failure retains completed segments and identifies output exhaustion', () => {
  const run = runSchema.parse({ id: 'run', artifact_id: 'artifact', source_task_id: 62, parent_run_id: null, status: 'budget_exhausted', stage: 'budget_exhausted', cancel_requested: false, can_cancel: false, can_retry: true, can_resume: false, result: null, error_code: 'budget_exhausted', created_at: '', started_at: null, finished_at: '', last_seq: 9,
    progress: { stage: 'generating', covered_segments: 2, total_segments: 4 }, budget: { stop_reason: 'output_tokens', max_llm_calls: 8, max_input_tokens: 65536, max_output_tokens: 24576 },
    usage: { llm_calls: 4, prompt_tokens: 38470, completion_tokens: 26636, token_source: 'actual' },
  })
  const { container } = render(<RunProgress run={run} />)
  expect(screen.getByText('已完成 2/4 段材料')).toBeTruthy()
  expect(screen.getByRole('alert').textContent).toContain('输出 Token 预算不足')
  expect(container.querySelector('[aria-current="step"]')?.textContent).toContain('生成学习笔记')
  expect(container.querySelectorAll('.done').length).toBe(2)
  expect(screen.getByText(/4\/8 次模型调用/)).toBeTruthy()
})
