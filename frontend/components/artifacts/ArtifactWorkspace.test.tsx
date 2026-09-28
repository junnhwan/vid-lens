// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, expect, test, vi } from 'vitest'
import { studyFixture } from '@/dev/productFixtures'
import { ApiError } from '@/lib/api'
import { ArtifactWorkspace } from './ArtifactWorkspace'

vi.mock('@/components/ui/useMediaQuery', () => ({ useMediaQuery: () => false }))
afterEach(() => { cleanup(); vi.restoreAllMocks(); vi.unstubAllGlobals() })

test('discarding an edited and deleted draft also discards its deletion undo snapshot', () => {
  vi.spyOn(window, 'confirm').mockReturnValue(true)
  render(<ArtifactWorkspace artifact={studyFixture} evidencePanel={() => null} onEvidence={() => {}} onSave={vi.fn()} onReload={vi.fn()} />)
  fireEvent.click(screen.getByRole('button', { name: '编辑笔记' }))
  fireEvent.change(screen.getByLabelText('第 1 块正文'), { target: { value: '应被放弃的本地修改' } })
  fireEvent.click(screen.getAllByRole('button', { name: '删除…' })[0])
  fireEvent.click(screen.getByRole('button', { name: '删除并保留撤销' }))
  expect(screen.getByRole('button', { name: '撤销删除' })).toBeTruthy()
  fireEvent.click(screen.getByRole('button', { name: '取消编辑' }))
  expect(screen.queryByRole('button', { name: '撤销删除' })).toBeNull()
  expect(screen.queryByText('应被放弃的本地修改')).toBeNull()
  expect(screen.getByText(studyFixture.version!.body.blocks[0].content)).toBeTruthy()
  expect(screen.queryByText('有未保存的修改')).toBeNull()
})

test('resuming a digit-prefixed imported block scrolls to its literal DOM identity', async () => {
  const artifact = structuredClone(studyFixture)
  const block = artifact.version!.body.blocks.at(-1)!
  block.block_id = '9fcfc5f3-6b29-40a6-a211-8ea71b7ddaa5'
  const scroll = vi.fn()
  Object.defineProperty(HTMLElement.prototype, 'scrollIntoView', { configurable: true, value: scroll })
  // Emulate the browser escaping a leading digit. getElementById must not use it.
  vi.stubGlobal('CSS', { escape: (id: string) => `\\39 ${id.slice(1)}` })
  render(<ArtifactWorkspace artifact={artifact} initialBlock={block.block_id} evidencePanel={() => null} onEvidence={() => {}} onSave={vi.fn()} onReload={vi.fn()} />)
  await waitFor(() => expect(scroll).toHaveBeenCalledWith({ block: 'center' }))
  expect(scroll.mock.instances[0]).toBe(document.getElementById(`block-${block.block_id}`))
  delete (HTMLElement.prototype as Partial<HTMLElement>).scrollIntoView
})

test('loading the server version after a save conflict does not restore stale page props', async () => {
  vi.spyOn(window, 'confirm').mockReturnValue(true)
  const next = structuredClone(studyFixture)
  next.head_version = 2
  next.version!.version = 2
  next.version!.body.blocks[0].content = '服务器已经保存的新正文'
  const save = vi.fn().mockRejectedValue(new ApiError(409, '版本冲突', 'version_conflict'))
  render(<ArtifactWorkspace artifact={studyFixture} evidencePanel={() => null} onEvidence={() => {}} onSave={save} onReload={vi.fn().mockResolvedValue(next)} />)
  fireEvent.click(screen.getByRole('button', { name: '编辑笔记' }))
  fireEvent.change(screen.getByLabelText('第 1 块正文'), { target: { value: '本地草稿' } })
  fireEvent.click(screen.getByRole('button', { name: '保存修改' }))
  fireEvent.click(await screen.findByRole('button', { name: '加载服务器版本' }))
  expect(screen.getByText('服务器已经保存的新正文')).toBeTruthy()
  expect(screen.queryByText('本地草稿')).toBeNull()
  expect(screen.getByText('v2 · 待核对')).toBeTruthy()
})

test('URL block restoration selects its evidence and follows URL changes without saving position again', async () => {
  const artifact = structuredClone(studyFixture)
  const first = artifact.version!.body.blocks[0]
  const target = artifact.version!.body.blocks.at(-1)!
  target.evidence_refs = [{ evidence_id: 'restored-evidence', relation: 'context' }]
  const onEvidence = vi.fn()
  const onStudyBlock = vi.fn()
  const panel = vi.fn(() => null)
  const scroll = vi.fn()
  Object.defineProperty(HTMLElement.prototype, 'scrollIntoView', { configurable: true, value: scroll })
  const props = { artifact, selectedEvidence: first.evidence_refs[0]?.evidence_id, evidencePanel: panel, onEvidence, onStudyBlock, onSave: vi.fn(), onReload: vi.fn() }
  const { rerender } = render(<ArtifactWorkspace {...props} initialBlock={target.block_id} />)
  await waitFor(() => expect(panel).toHaveBeenLastCalledWith(target.evidence_refs, 'restored-evidence', onEvidence))
  expect(onEvidence).toHaveBeenLastCalledWith('restored-evidence')
  expect(document.getElementById(`block-${target.block_id}`)?.classList.contains('selected')).toBe(true)
  rerender(<ArtifactWorkspace {...props} initialBlock={first.block_id} />)
  await waitFor(() => expect(panel).toHaveBeenLastCalledWith(first.evidence_refs, first.evidence_refs[0]?.evidence_id, onEvidence))
  expect(onEvidence).toHaveBeenCalledTimes(2)
  rerender(<ArtifactWorkspace {...props} artifact={structuredClone(artifact)} initialBlock={first.block_id} />)
  expect(onEvidence).toHaveBeenCalledTimes(2)
  expect(onStudyBlock).not.toHaveBeenCalled()
  delete (HTMLElement.prototype as Partial<HTMLElement>).scrollIntoView
})
