// @vitest-environment jsdom
import type { ReactNode } from 'react'
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, expect, test, vi } from 'vitest'
import { artifactApi } from '@/lib/artifacts/api'
import * as canvas from '@/lib/artifacts/canvas'
import { studyFixture } from '@/dev/productFixtures'
import type { CanvasLayoutView } from '@/lib/artifacts/schema'
import { KnowledgeCanvas } from './KnowledgeCanvas'

vi.mock('@xyflow/react', () => ({
  ReactFlowProvider: ({ children }: { children: ReactNode }) => children,
  ReactFlow: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  Background: () => null, Controls: () => null, Handle: () => null,
  MarkerType: { ArrowClosed: 'arrow' }, Position: { Left: 'left', Right: 'right', Top: 'top', Bottom: 'bottom' },
  useReactFlow: () => ({ fitView: vi.fn() }), applyNodeChanges: vi.fn(),
}))

afterEach(() => { cleanup(); vi.restoreAllMocks() })
function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (reason: Error) => void
  const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no })
  return { promise, resolve, reject }
}
const body = studyFixture.version!.body
const initial: CanvasLayoutView = { content_version_id: 'v1', view_id: 'knowledge', revision: 1, layout: canvas.fillCanvasPositions(body, canvas.emptyCanvasLayout()) }
const props = { artifactId: studyFixture.id, versionId: 'v1', headVersion: 1, body, readOnly: false, selectedBlock: body.blocks[0].block_id, onSelect: vi.fn(), onBodyChange: vi.fn(), onEvidence: vi.fn() }

test('late automatic layout cannot overwrite a manual layout whose save has not returned', async () => {
  vi.spyOn(artifactApi, 'canvasLayout').mockResolvedValue(initial)
  const pendingSave = deferred<CanvasLayoutView>()
  const save = vi.spyOn(artifactApi, 'saveCanvasLayout').mockReturnValue(pendingSave.promise)
  const pendingLayout = deferred<Awaited<ReturnType<typeof canvas.computeCanvasLayout>>>()
  vi.spyOn(canvas, 'computeCanvasLayout').mockReturnValue(pendingLayout.promise)
  render(<KnowledgeCanvas {...props} />)
  await waitFor(() => expect((screen.getByRole('button', { name: '横向排版' }) as HTMLButtonElement).disabled).toBe(false))
  fireEvent.click(screen.getByRole('button', { name: '横向排版' }))
  fireEvent.click(screen.getByRole('button', { name: '固定位置' }))
  await waitFor(() => expect(save).toHaveBeenCalledTimes(1))
  await act(async () => { pendingLayout.resolve({ layout: { ...initial.layout, direction: 'DOWN' }, collisions: [] }); await pendingLayout.promise })
  expect(screen.getByRole('button', { name: '取消固定' })).toBeTruthy()
  await act(async () => { pendingSave.resolve({ ...initial, revision: 2, layout: save.mock.calls[0][3] }); await pendingSave.promise })
  expect(save).toHaveBeenCalledTimes(1)
})

test('late AI layout plan is discarded when the body changes before its response', async () => {
  vi.spyOn(artifactApi, 'canvasLayout').mockResolvedValue(initial)
  const pending = deferred<Awaited<ReturnType<typeof artifactApi.suggestCanvasLayout>>>()
  vi.spyOn(artifactApi, 'suggestCanvasLayout').mockReturnValue(pending.promise)
  const compute = vi.spyOn(canvas, 'computeCanvasLayout')
  const { rerender } = render(<KnowledgeCanvas {...props} />)
  await waitFor(() => expect((screen.getByRole('button', { name: '横向排版' }) as HTMLButtonElement).disabled).toBe(false))
  fireEvent.change(screen.getByLabelText('AI 局部排版'), { target: { value: '紧凑排列' } })
  fireEvent.click(screen.getByRole('button', { name: '生成并应用排版' }))
  rerender(<KnowledgeCanvas {...props} body={{ ...body, title: '新的人工草稿' }} readOnly />)
  await act(async () => { pending.resolve({ direction: 'DOWN', scope: 'all', density: 'compact', summary: '紧凑' }); await pending.promise })
  expect(compute).not.toHaveBeenCalled()
})

test('a previous content version save failure does not block the new version layout', async () => {
  vi.spyOn(artifactApi, 'canvasLayout').mockResolvedValueOnce(initial).mockResolvedValue({ ...initial, content_version_id: 'v2' })
  const pending = deferred<CanvasLayoutView>()
  const save = vi.spyOn(artifactApi, 'saveCanvasLayout').mockReturnValueOnce(pending.promise).mockImplementation(async (_id, version, revision, layout) => ({ ...initial, content_version_id: version, revision: revision + 1, layout }))
  const { rerender } = render(<KnowledgeCanvas {...props} />)
  await waitFor(() => expect((screen.getByRole('button', { name: '横向排版' }) as HTMLButtonElement).disabled).toBe(false))
  fireEvent.click(screen.getByRole('button', { name: '固定位置' }))
  await waitFor(() => expect(save).toHaveBeenCalledTimes(1))
  rerender(<KnowledgeCanvas {...props} versionId="v2" headVersion={2} />)
  await waitFor(() => expect((screen.getByRole('button', { name: '横向排版' }) as HTMLButtonElement).disabled).toBe(false))
  await act(async () => { pending.reject(new Error('旧布局保存失败')); await pending.promise.catch(() => {}) })
  expect(screen.queryByRole('alert')).toBeNull()
  fireEvent.click(screen.getByRole('button', { name: '固定位置' }))
  await waitFor(() => expect(save).toHaveBeenCalledTimes(2))
  expect(save.mock.calls[1][1]).toBe('v2')
})

test('late historical layout undo cannot replace a newer manual layout', async () => {
  const pending = deferred<CanvasLayoutView>()
  vi.spyOn(artifactApi, 'canvasLayout').mockResolvedValueOnce({ ...initial, revision: 2 }).mockReturnValueOnce(pending.promise)
  const save = vi.spyOn(artifactApi, 'saveCanvasLayout').mockImplementation(async (_id, version, revision, layout) => ({ ...initial, content_version_id: version, revision: revision + 1, layout }))
  render(<KnowledgeCanvas {...props} />)
  await waitFor(() => expect((screen.getByRole('button', { name: '撤销布局' }) as HTMLButtonElement).disabled).toBe(false))
  fireEvent.click(screen.getByRole('button', { name: '撤销布局' }))
  fireEvent.click(screen.getByRole('button', { name: '固定位置' }))
  await waitFor(() => expect(save).toHaveBeenCalledTimes(1))
  await act(async () => { pending.resolve(initial); await pending.promise })
  expect(save).toHaveBeenCalledTimes(1)
  expect(screen.getByRole('button', { name: '取消固定' })).toBeTruthy()
})
