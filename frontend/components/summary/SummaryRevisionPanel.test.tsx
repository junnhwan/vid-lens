// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, expect, test, vi } from 'vitest'
import { api } from '@/lib/api'
import { artifactApi } from '@/lib/artifacts/api'
import { studyFixture } from '@/dev/productFixtures'
import type { EffectiveSummaryView, SummaryEditOperation, VideoTermRuleSet } from '@/lib/types'
import { SummaryRevisionPanel } from './SummaryRevisionPanel'

afterEach(() => { cleanup(); vi.restoreAllMocks() })

const generated: EffectiveSummaryView = { task_id: 42, content: '安装章节：旧名。', revision: 0, revision_id: '', base_generated_hash: 'base', current_generated_hash: 'base', source_status: 'current', has_generated: true, has_revision: false }
const revised: EffectiveSummaryView = { ...generated, content: '安装章节：新名。', revision: 1, revision_id: 'revision-1', has_revision: true }
const proposal: SummaryEditOperation = { id: 'summary-op', run_id: 'summary-run', task_id: 42, instruction: '改正安装章节名称', status: 'proposed', mode: 'preview', base_version: 0, rule_version: 0, rule_digest: 'empty', edits: [{ old_text: '旧名', new_text: '新名' }] }
const emptyRules: VideoTermRuleSet = { version: 0, digest: 'empty', rules: [] }

test('only this time commits the summary without creating a remembered term rule', async () => {
  vi.spyOn(api, 'getSummary').mockResolvedValueOnce(generated).mockResolvedValue(revised)
  vi.spyOn(api, 'getTermRules').mockResolvedValue(emptyRules)
  vi.spyOn(api, 'getLatestSummaryOperation').mockResolvedValue(null)
  vi.spyOn(api, 'editSummary').mockResolvedValue(proposal)
  const applySummary = vi.spyOn(api, 'applySummaryOperation').mockResolvedValue({ ...proposal, status: 'committed', result_revision_id: 'revision-1' })
  const saveRule = vi.spyOn(api, 'saveTermRule')
  vi.spyOn(artifactApi, 'list').mockResolvedValue({ list: [], total: 0, page: 1, page_size: 20 })
  render(<SummaryRevisionPanel taskId={42} readOnly={false} onChanged={() => {}} />)
  fireEvent.change(await screen.findByLabelText('让 AI 修改这份摘要'), { target: { value: '改正安装章节名称' } })
  fireEvent.click(screen.getByRole('button', { name: '预览 AI 修改' }))
  fireEvent.click(await screen.findByRole('button', { name: '确认保存摘要' }))
  await waitFor(() => expect(applySummary).toHaveBeenCalledTimes(1))
  expect(await screen.findByText(/摘要：已保存修订/)).toBeTruthy()
  expect(saveRule).not.toHaveBeenCalled()
})

test('summary, note and remembered rule report independent outcomes and retry only failed targets', async () => {
  vi.spyOn(api, 'getSummary').mockResolvedValueOnce(generated).mockResolvedValue(revised)
  vi.spyOn(api, 'getTermRules').mockResolvedValue(emptyRules)
  vi.spyOn(api, 'getLatestSummaryOperation').mockResolvedValue(null)
  vi.spyOn(api, 'editSummary').mockResolvedValue(proposal)
  const applySummary = vi.spyOn(api, 'applySummaryOperation').mockResolvedValue({ ...proposal, status: 'committed', result_revision_id: 'revision-1' })
  const saveRule = vi.spyOn(api, 'saveTermRule').mockRejectedValueOnce(new Error('rule storage unavailable')).mockResolvedValue({ version: 1, digest: 'rule-1', rules: [] })
  vi.spyOn(artifactApi, 'list').mockResolvedValue({ list: [studyFixture], total: 1, page: 1, page_size: 20 })
  vi.spyOn(artifactApi, 'get').mockResolvedValue(studyFixture)
  const submitNote = vi.spyOn(artifactApi, 'submitEdit').mockRejectedValueOnce(new Error('note queue unavailable')).mockResolvedValue({
    id: 'note-run', artifact_id: studyFixture.id, instruction: '改正安装章节名称', expected_head_version: studyFixture.head_version,
    base_version_id: studyFixture.version!.id, selected_block_ids: [], mode: 'apply', status: 'completed', stage: 'completed',
    cancel_requested: false, can_cancel: false, result: { kind: 'committed', operation_id: 'note-op', result_version_id: 'note-v2' },
    error_code: null, created_at: '2026-09-28T00:00:00Z', started_at: '2026-09-28T00:00:00Z', finished_at: '2026-09-28T00:00:00Z', last_seq: 1,
    usage: { llm_calls: 1, prompt_tokens: 1, completion_tokens: 1, token_source: 'actual' },
  })
  render(<SummaryRevisionPanel taskId={42} readOnly={false} onChanged={() => {}} />)
  fireEvent.change(await screen.findByLabelText('让 AI 修改这份摘要'), { target: { value: '改正安装章节名称' } })
  fireEvent.click(screen.getByLabelText('此视频以后都使用这个名称'))
  fireEvent.change(screen.getByLabelText('原写法'), { target: { value: '旧名' } })
  fireEvent.change(screen.getByLabelText('使用名称'), { target: { value: '新名' } })
  fireEvent.change(screen.getByLabelText('适用上下文'), { target: { value: '安装章节' } })
  fireEvent.change(screen.getByLabelText(/同时修改一份笔记/), { target: { value: studyFixture.id } })
  fireEvent.click(screen.getByRole('button', { name: '预览 AI 修改' }))
  expect(await screen.findByText('逐处差异')).toBeTruthy()
  fireEvent.click(screen.getByRole('button', { name: '确认保存摘要并记住规则' }))
  expect(await screen.findByText(/摘要：已保存修订/)).toBeTruthy()
  expect(await screen.findByText(/笔记：提交失败/)).toBeTruthy()
  expect(await screen.findByText(/术语规则：保存失败/)).toBeTruthy()
  expect(applySummary).toHaveBeenCalledTimes(1)
  fireEvent.click(screen.getByRole('button', { name: '只重试笔记' }))
  await waitFor(() => expect(submitNote).toHaveBeenCalledTimes(2))
  expect(await screen.findByText(/笔记：已保存新版本/)).toBeTruthy()
  fireEvent.click(screen.getByRole('button', { name: '只重试保存术语规则' }))
  await waitFor(() => expect(saveRule).toHaveBeenCalledTimes(2))
  expect(applySummary).toHaveBeenCalledTimes(1)
})

test('refresh restores a pending summary operation and observes the worker without submitting again', async () => {
  vi.spyOn(api, 'getSummary').mockResolvedValue(generated)
  vi.spyOn(api, 'getTermRules').mockResolvedValue(emptyRules)
  vi.spyOn(api, 'getLatestSummaryOperation').mockResolvedValue({ ...proposal, status: 'running', edits: [] })
  const observe = vi.spyOn(api, 'getSummaryOperation').mockResolvedValue(proposal)
  const submit = vi.spyOn(api, 'editSummary')
  vi.spyOn(artifactApi, 'list').mockResolvedValue({ list: [], total: 0, page: 1, page_size: 20 })
  render(<SummaryRevisionPanel taskId={42} readOnly={false} onChanged={() => {}} />)
  expect(await screen.findByText(/离开页面或刷新不会取消后台任务/)).toBeTruthy()
  expect(await screen.findByText('逐处差异', {}, { timeout: 4000 })).toBeTruthy()
  expect(observe).toHaveBeenCalledWith(42, 'summary-op')
  expect(submit).not.toHaveBeenCalled()
})

test('a display refresh failure after commit still saves the requested remembered rule', async () => {
  vi.spyOn(api, 'getSummary').mockResolvedValueOnce(generated).mockRejectedValue(new Error('refresh unavailable'))
  vi.spyOn(api, 'getTermRules').mockResolvedValue(emptyRules)
  vi.spyOn(api, 'getLatestSummaryOperation').mockResolvedValue(null)
  vi.spyOn(api, 'editSummary').mockResolvedValue(proposal)
  const applySummary = vi.spyOn(api, 'applySummaryOperation').mockResolvedValue({ ...proposal, status: 'committed', result_revision_id: 'revision-1' })
  const saveRule = vi.spyOn(api, 'saveTermRule').mockResolvedValue({ version: 1, digest: 'rule-1', rules: [] })
  vi.spyOn(artifactApi, 'list').mockResolvedValue({ list: [], total: 0, page: 1, page_size: 20 })
  render(<SummaryRevisionPanel taskId={42} readOnly={false} onChanged={() => {}} />)
  fireEvent.change(await screen.findByLabelText('让 AI 修改这份摘要'), { target: { value: '改正安装章节名称' } })
  fireEvent.click(screen.getByLabelText('此视频以后都使用这个名称'))
  fireEvent.change(screen.getByLabelText('原写法'), { target: { value: '旧名' } })
  fireEvent.change(screen.getByLabelText('使用名称'), { target: { value: '新名' } })
  fireEvent.change(screen.getByLabelText('适用上下文'), { target: { value: '安装章节' } })
  fireEvent.click(screen.getByRole('button', { name: '预览 AI 修改' }))
  fireEvent.click(await screen.findByRole('button', { name: '确认保存摘要并记住规则' }))
  expect(await screen.findByText(/术语规则：已保存 v1/)).toBeTruthy()
  expect(await screen.findByRole('alert')).toHaveProperty('textContent', expect.stringContaining('摘要已保存，但刷新显示失败'))
  expect(screen.queryByText(/摘要：保存失败/)).toBeNull()
  expect(applySummary).toHaveBeenCalledTimes(1)
  expect(saveRule).toHaveBeenCalledTimes(1)
})

test('restores a failed term correction with readable guidance and permits a fresh preview', async () => {
  vi.spyOn(api, 'getSummary').mockResolvedValue(generated)
  vi.spyOn(api, 'getTermRules').mockResolvedValue(emptyRules)
  vi.spyOn(api, 'getLatestSummaryOperation').mockResolvedValue({ ...proposal, instruction: 'jeff是Jev', status: 'failed', error_code: 'anchor_ambiguous', edits: [] })
  const submit = vi.spyOn(api, 'editSummary').mockResolvedValue(proposal)
  vi.spyOn(artifactApi, 'list').mockResolvedValue({ list: [], total: 0, page: 1, page_size: 20 })
  render(<SummaryRevisionPanel taskId={42} readOnly={false} onChanged={() => {}} />)
  expect(await screen.findByRole('alert')).toHaveProperty('textContent', expect.stringContaining('AI 未能准确定位'))
  expect(screen.getByRole('alert').textContent).not.toContain('anchor_ambiguous')
  expect(screen.getByLabelText('让 AI 修改这份摘要')).toHaveProperty('value', 'jeff是Jev')
  fireEvent.click(screen.getByRole('button', { name: '预览 AI 修改' }))
  expect(await screen.findByText('逐处差异')).toBeTruthy()
  expect(screen.queryByRole('alert')).toBeNull()
  expect(submit).toHaveBeenCalledTimes(1)
})

test('a failed preview gives readable guidance and the next attempt uses a new request key', async () => {
  vi.spyOn(api, 'getSummary').mockResolvedValue(generated)
  vi.spyOn(api, 'getTermRules').mockResolvedValue(emptyRules)
  vi.spyOn(api, 'getLatestSummaryOperation').mockResolvedValue(null)
  const submit = vi.spyOn(api, 'editSummary').mockResolvedValueOnce({ ...proposal, status: 'failed', error_code: 'anchor_ambiguous', edits: [] }).mockResolvedValue(proposal)
  vi.spyOn(artifactApi, 'list').mockResolvedValue({ list: [], total: 0, page: 1, page_size: 20 })
  render(<SummaryRevisionPanel taskId={42} readOnly={false} onChanged={() => {}} />)
  fireEvent.change(await screen.findByLabelText('让 AI 修改这份摘要'), { target: { value: 'jeff是Jev' } })
  fireEvent.click(screen.getByRole('button', { name: '预览 AI 修改' }))
  expect(await screen.findByRole('alert')).toHaveProperty('textContent', expect.stringContaining('AI 未能准确定位'))
  fireEvent.click(screen.getByRole('button', { name: '预览 AI 修改' }))
  expect(await screen.findByText('逐处差异')).toBeTruthy()
  expect(submit.mock.calls[1][2]).not.toBe(submit.mock.calls[0][2])
})
