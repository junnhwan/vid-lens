// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, expect, test, vi } from 'vitest'
import { studyFixture } from '@/dev/productFixtures'
import { ApiError } from '@/lib/api'
import type { StudyBlock } from '@/lib/artifacts/schema'
import { ArtifactWorkspace } from './ArtifactWorkspace'

const mediaQueryState = vi.hoisted(() => ({ mobile: false }))
vi.mock('@/components/ui/useMediaQuery', () => ({ useMediaQuery: () => mediaQueryState.mobile }))
afterEach(() => { cleanup(); vi.restoreAllMocks(); vi.unstubAllGlobals(); mediaQueryState.mobile = false })

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

test('offers explicit full and block agent edit actions without replacing read-only ask', () => {
  const onAgentEdit = vi.fn()
  const onAskBlock = vi.fn()
  render(<ArtifactWorkspace artifact={studyFixture} evidencePanel={() => null} onEvidence={() => {}} onSave={vi.fn()} onReload={vi.fn()} onAgentEdit={onAgentEdit} onAskBlock={onAskBlock} />)

  fireEvent.click(screen.getByRole('button', { name: '让 Agent 修改全文' }))
  expect(onAgentEdit).toHaveBeenLastCalledWith(null, studyFixture)

  fireEvent.click(screen.getAllByRole('button', { name: '让 Agent 修改这段' })[0])
  expect(onAgentEdit).toHaveBeenLastCalledWith(studyFixture.version!.body.blocks[0].block_id, studyFixture)
  expect(onAskBlock).not.toHaveBeenCalled()
})

test('saves a dirty manual draft before opening a scoped agent edit', async () => {
  const saved = structuredClone(studyFixture)
  saved.head_version = 2
  saved.version!.id = 'preview-version-2'
  saved.version!.version = 2
  saved.version!.body.blocks[0].content = '人工先保存的正文'
  const onSave = vi.fn().mockResolvedValue(saved)
  const onAgentEdit = vi.fn()
  render(<ArtifactWorkspace artifact={studyFixture} evidencePanel={() => null} onEvidence={() => {}} onSave={onSave} onReload={vi.fn()} onAgentEdit={onAgentEdit} />)

  fireEvent.click(screen.getByRole('button', { name: '编辑笔记' }))
  fireEvent.change(screen.getByLabelText('第 1 块正文'), { target: { value: '人工先保存的正文' } })
  fireEvent.click(screen.getAllByRole('button', { name: '让 Agent 修改这段' })[0])
  expect(screen.getByRole('dialog', { name: '先保存修改，再让 Agent 修改' })).toBeTruthy()
  fireEvent.click(screen.getByRole('button', { name: '保存并交给 Agent' }))

  await waitFor(() => expect(onAgentEdit).toHaveBeenCalledWith(studyFixture.version!.body.blocks[0].block_id, saved))
  expect(onSave).toHaveBeenCalledWith(1, expect.objectContaining({ blocks: expect.arrayContaining([expect.objectContaining({ content: '人工先保存的正文' })]) }))
})

test('keeps a failed dirty-save error visible inside the agent prerequisite modal', async () => {
  const onAgentEdit = vi.fn()
  const onSave = vi.fn().mockRejectedValue(new ApiError(500, '保存服务暂时不可用'))
  render(<ArtifactWorkspace artifact={studyFixture} evidencePanel={() => null} onEvidence={() => {}} onSave={onSave} onReload={vi.fn()} onAgentEdit={onAgentEdit} />)

  fireEvent.click(screen.getByRole('button', { name: '编辑笔记' }))
  fireEvent.change(screen.getByLabelText('第 1 块正文'), { target: { value: '尚未保存的正文' } })
  fireEvent.click(screen.getAllByRole('button', { name: '让 Agent 修改这段' })[0])
  const dialog = screen.getByRole('dialog', { name: '先保存修改，再让 Agent 修改' })
  fireEvent.click(within(dialog).getByRole('button', { name: '保存并交给 Agent' }))

  expect((await within(dialog).findByRole('alert')).textContent).toContain('保存服务暂时不可用')
  expect(within(dialog).getByText('Agent 尚未启动，本地草稿仍保留在当前页面。')).toBeTruthy()
  expect(onAgentEdit).not.toHaveBeenCalled()
})

test('an agent evidence request explicitly opens the narrow evidence drawer and selects its block', async () => {
  mediaQueryState.mobile = true
  const onEvidence = vi.fn()
  const panel = vi.fn((refs: StudyBlock['evidence_refs'], selectedId: string | undefined) => <div data-testid="requested-evidence">{selectedId} · {refs.map(ref => ref.evidence_id).join(',')}</div>)
  const { rerender } = render(<ArtifactWorkspace artifact={studyFixture} evidencePanel={panel} onEvidence={onEvidence} onSave={vi.fn()} onReload={vi.fn()} evidenceOpenRequest={{ id: 'preview-e3', nonce: 1 }} />)

  const drawer = await screen.findByRole('dialog', { name: '回到原视频' })
  expect(within(drawer).getByTestId('requested-evidence').textContent).toContain('preview-e3 · preview-e3')
  expect(document.getElementById('block-config')?.classList.contains('selected')).toBe(true)
  expect(onEvidence).toHaveBeenCalledTimes(1)

  rerender(<ArtifactWorkspace artifact={studyFixture} evidencePanel={panel} onEvidence={onEvidence} onSave={vi.fn()} onReload={vi.fn()} evidenceOpenRequest={{ id: 'preview-e3', nonce: 2 }} />)
  await waitFor(() => expect(onEvidence).toHaveBeenCalledTimes(2))

  rerender(<ArtifactWorkspace artifact={studyFixture} selectedEvidence="operation-only-evidence" evidencePanel={panel} onEvidence={onEvidence} onSave={vi.fn()} onReload={vi.fn()} evidenceOpenRequest={{ id: 'operation-only-evidence', nonce: 3 }} />)
  await waitFor(() => expect(screen.getByTestId('requested-evidence').textContent).toContain('operation-only-evidence'))
  expect(onEvidence).toHaveBeenCalledTimes(3)
})
