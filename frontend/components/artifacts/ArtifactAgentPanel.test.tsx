// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, expect, test, vi } from 'vitest'
import { studyFixture } from '@/dev/productFixtures'
import { artifactApi } from '@/lib/artifacts/api'
import type { ArtifactEditOperation } from '@/lib/artifacts/schema'
import { ArtifactAgentPanel } from './ArtifactAgentPanel'

afterEach(() => { cleanup(); vi.restoreAllMocks(); vi.useRealTimers() })

const timestamp = '2026-09-28T02:00:00Z'
const run = {
  id: 'edit-run-1', artifact_id: studyFixture.id, instruction: '把安装部分拆成步骤', expected_head_version: 1,
  base_version_id: studyFixture.version!.id, selected_block_ids: ['build'], mode: 'apply' as const,
  status: 'completed' as const, stage: 'completed', cancel_requested: false, can_cancel: false,
  result: { kind: 'committed' as const, operation_id: 'operation-1', result_version_id: 'version-2' },
  error_code: null, created_at: timestamp, started_at: timestamp, finished_at: timestamp, last_seq: 4,
  usage: { llm_calls: 1, prompt_tokens: 100, completion_tokens: 40, token_source: 'actual' as const },
}
const operation: ArtifactEditOperation = {
  id: 'operation-1', artifact_id: studyFixture.id, status: 'committed' as const, base_version: 1,
  base_version_id: studyFixture.version!.id, result_version_id: 'version-2', undo_version_id: null,
  basis: 'evidence_supported' as const, evidence_ids: ['preview-e1'], summary: '把构建说明拆成可执行步骤',
  counts: { added: 1, updated: 1, deleted: 0, moved: 0 },
  changes: [{ kind: 'updated', block_id: 'build', before: studyFixture.version!.body.blocks[0], after: { ...studyFixture.version!.body.blocks[0], content: '第一步：构建镜像。' } }],
  block_mappings: [], can_apply: false, can_undo: true, created_at: timestamp, updated_at: timestamp, committed_at: timestamp,
}

test('direct apply binds the saved base and renders the durable server diff with undo', async () => {
  const refreshed = structuredClone(studyFixture)
  refreshed.head_version = 2
  refreshed.version!.version = 2
  refreshed.version!.id = 'version-2'
  vi.spyOn(artifactApi, 'submitEdit').mockResolvedValue(run)
  vi.spyOn(artifactApi, 'editOperation').mockResolvedValue(operation)
  vi.spyOn(artifactApi, 'undoEdit').mockResolvedValue({ ...operation, status: 'committed', undo_version_id: 'version-3', can_undo: false })
  const onArtifactChanged = vi.fn().mockResolvedValue(refreshed)

  render(<ArtifactAgentPanel artifact={studyFixture} initialScope="build" onClose={() => {}} onArtifactChanged={onArtifactChanged} onOpenEvidence={() => {}} />)
  fireEvent.change(screen.getByLabelText('修改要求'), { target: { value: '把安装部分拆成步骤' } })
  fireEvent.click(screen.getByRole('button', { name: '直接保存修改' }))

  await waitFor(() => expect(artifactApi.submitEdit).toHaveBeenCalledWith(studyFixture.id, {
    instruction: '把安装部分拆成步骤', expected_head_version: 1, selected_block_ids: ['build'], mode: 'apply',
  }, expect.any(String)))
  expect(await screen.findByText('把构建说明拆成可执行步骤')).toBeTruthy()
  expect(screen.getByText('第一步：构建镜像。')).toBeTruthy()
  expect(screen.getByText('依据支持')).toBeTruthy()

  fireEvent.click(screen.getByRole('button', { name: '撤销这次修改' }))
  await waitFor(() => expect(artifactApi.undoEdit).toHaveBeenCalledWith('operation-1', 2, expect.any(String)))
  expect(screen.getByText('UNDONE · ORIGINAL EDIT v2')).toBeTruthy()
  expect(screen.queryByText('UNDONE · v3')).toBeNull()
})

test('closing an active edit panel never turns observation disconnect into cancellation', () => {
  const active = { ...run, status: 'running' as const, stage: 'planning', can_cancel: true, result: null, finished_at: null }
  vi.spyOn(artifactApi, 'editRun').mockResolvedValue(active)
  const cancel = vi.spyOn(artifactApi, 'cancelEdit').mockResolvedValue(active)
  const onClose = vi.fn()
  render(<ArtifactAgentPanel artifact={studyFixture} initialScope={null} initialRun={active} onClose={onClose} onArtifactChanged={vi.fn()} onOpenEvidence={() => {}} />)

  fireEvent.click(screen.getByRole('button', { name: '关闭' }))
  expect(onClose).toHaveBeenCalledOnce()
  expect(cancel).not.toHaveBeenCalled()
})

test('preview persists a proposal and applies it only after an explicit second action', async () => {
  const proposalRun = { ...run, mode: 'preview' as const, result: { kind: 'proposal' as const, operation_id: 'operation-1' } }
  const proposal = { ...operation, status: 'proposed' as const, result_version_id: null, committed_at: null, can_apply: true, can_undo: false }
  vi.spyOn(artifactApi, 'submitEdit').mockResolvedValue(proposalRun)
  vi.spyOn(artifactApi, 'editOperation').mockResolvedValue(proposal)
  vi.spyOn(artifactApi, 'applyEdit').mockResolvedValue(operation)
  const onArtifactChanged = vi.fn().mockResolvedValue({ ...studyFixture, head_version: 2 })

  render(<ArtifactAgentPanel artifact={studyFixture} initialScope={null} onClose={() => {}} onArtifactChanged={onArtifactChanged} onOpenEvidence={() => {}} />)
  fireEvent.change(screen.getByLabelText('修改要求'), { target: { value: '合并全文重复概念' } })
  fireEvent.click(screen.getByRole('button', { name: '先看修改方案' }))
  expect(await screen.findByRole('button', { name: '应用这份方案' })).toBeTruthy()
  expect(artifactApi.applyEdit).not.toHaveBeenCalled()

  fireEvent.click(screen.getByRole('button', { name: '应用这份方案' }))
  await waitFor(() => expect(artifactApi.applyEdit).toHaveBeenCalledWith('operation-1', 1, expect.any(String)))
})

test('an active run retries a transient observation failure without submitting the edit again', async () => {
  vi.useFakeTimers()
  const active = { ...run, status: 'running' as const, stage: 'planning', can_cancel: true, result: null, finished_at: null }
  const completed = { ...active, status: 'completed' as const, stage: 'completed', can_cancel: false, result: { kind: 'answer' as const, message: '核对完成，没有写入。', evidence_ids: [] }, finished_at: timestamp }
  const observe = vi.spyOn(artifactApi, 'editRun').mockRejectedValueOnce(new Error('temporary network failure')).mockResolvedValue(completed)
  const submit = vi.spyOn(artifactApi, 'submitEdit')

  render(<ArtifactAgentPanel artifact={studyFixture} initialScope={null} initialRun={active} onClose={() => {}} onArtifactChanged={vi.fn()} onOpenEvidence={() => {}} />)
  await act(async () => { await vi.advanceTimersByTimeAsync(1200) })
  expect(observe).toHaveBeenCalledTimes(1)
  await act(async () => { await vi.advanceTimersByTimeAsync(1200) })
  expect(observe).toHaveBeenCalledTimes(2)
  expect(screen.getByText('核对完成，没有写入。')).toBeTruthy()
  expect(submit).not.toHaveBeenCalled()
})

test('operation lookup retries a transient read failure and exposes type, hierarchy, position, and references', async () => {
  vi.useFakeTimers()
  const before = studyFixture.version!.body.blocks.find(block => block.block_id === 'stages')!
  const after = { ...before, parent_id: 'config', type: 'note' as const, evidence_refs: [{ evidence_id: 'preview-e3', relation: 'context' as const }] }
  const moved: ArtifactEditOperation = {
    ...operation,
    summary: '移动多阶段构建并更新依据',
    counts: { added: 0, updated: 0, deleted: 0, moved: 1 },
    changes: [{ kind: 'moved', block_id: before.block_id, before, after, before_index: 2, after_index: 4 }],
  }
  const refreshed = structuredClone(studyFixture)
  refreshed.head_version = 2
  const lookup = vi.spyOn(artifactApi, 'editOperation').mockRejectedValueOnce(new Error('temporary network failure')).mockResolvedValue(moved)

  render(<ArtifactAgentPanel artifact={studyFixture} initialScope={null} initialRun={run} onClose={() => {}} onArtifactChanged={vi.fn().mockResolvedValue(refreshed)} onOpenEvidence={() => {}} />)
  await act(async () => {})
  expect(lookup).toHaveBeenCalledTimes(1)
  await act(async () => { await vi.advanceTimersByTimeAsync(1200) })
  expect(lookup).toHaveBeenCalledTimes(2)
  expect(screen.getByText('SAVED VERSION · v2')).toBeTruthy()
  expect(screen.getByText('构建与交付')).toBeTruthy()
  expect(screen.getByText('配置与边界')).toBeTruthy()
  expect(screen.getByText('第 3 项')).toBeTruthy()
  expect(screen.getByText('第 5 项')).toBeTruthy()
  expect(screen.getByText('概念')).toBeTruthy()
  expect(screen.getByText('笔记')).toBeTruthy()
  expect(screen.getByText('支持 · preview-e2')).toBeTruthy()
  expect(screen.getByText('背景 · preview-e3')).toBeTruthy()
})

test('semantic relation diff names its blocks, direction, source, and evidence state', async () => {
  const [source, target] = studyFixture.version!.body.blocks
  const relation = { id: 'relation-1', source_block_id: source.block_id, target_block_id: target.block_id, type: 'related_to' as const, origin: 'user' as const, evidence_refs: [] }
  vi.spyOn(artifactApi, 'editOperation').mockResolvedValue({ ...operation, counts: { added: 1, updated: 0, deleted: 0, moved: 0 }, changes: [{ kind: 'relation_added', block_id: source.block_id, after_relation: relation }] })
  render(<ArtifactAgentPanel artifact={studyFixture} initialScope={null} initialRun={run} onClose={() => {}} onArtifactChanged={vi.fn().mockResolvedValue(studyFixture)} onOpenEvidence={() => {}} />)
  expect(await screen.findByText(`${source.title} ↔ ${target.title} · 相关 · 人工整理 · 未附依据`)).toBeTruthy()
})

test('read-only result is announced and delegates evidence opening to the workspace bridge', () => {
  const onOpenEvidence = vi.fn()
  const answer = { ...run, result: { kind: 'answer' as const, message: '名称与视频一致。', evidence_ids: ['preview-e1'] } }
  render(<ArtifactAgentPanel artifact={studyFixture} initialScope={null} initialRun={answer} onClose={() => {}} onArtifactChanged={vi.fn()} onOpenEvidence={onOpenEvidence} />)

  expect(screen.getByText('名称与视频一致。').closest('[role="status"]')?.getAttribute('aria-live')).toBe('polite')
  fireEvent.click(screen.getByRole('button', { name: '依据 1' }))
  expect(onOpenEvidence).toHaveBeenCalledWith('preview-e1')
})
