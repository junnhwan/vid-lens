// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, expect, test, vi } from 'vitest'
import { api } from '@/lib/api'
import UploadModal from './UploadModal'

vi.mock('@/lib/router', () => ({ useRouter: () => ({ push: vi.fn() }) }))
vi.mock('@/components/Toast', () => ({ useToast: () => ({ success: vi.fn(), error: vi.fn() }) }))
vi.mock('@/lib/api', async importOriginal => {
  const original = await importOriginal<typeof import('@/lib/api')>()
  return { ...original, api: { importOptions: vi.fn(), checkUpload: vi.fn(), uploadChunk: vi.fn(), mergeChunks: vi.fn() } }
})

beforeEach(() => {
  vi.mocked(api.importOptions).mockResolvedValue({ url_import_enabled: false })
  vi.mocked(api.checkUpload).mockResolvedValue({ status: 'uploading', uploaded: [] })
  vi.mocked(api.mergeChunks).mockResolvedValue({ task_id: 42, status: 0 } as never)
})
afterEach(() => { cleanup(); vi.resetAllMocks() })

function selectFile(size = 16) {
  const source = new Uint8Array(size)
  const file = {
    name: 'upload-test.mp4', size,
    slice: (start: number, end: number) => {
      const bytes = source.slice(start, end)
      const blob = new Blob([bytes])
      Object.defineProperty(blob, 'arrayBuffer', { value: async () => bytes.buffer })
      return blob
    },
  }
  const view = render(<UploadModal onClose={vi.fn()} />)
  fireEvent.change(view.container.querySelector('input[type="file"]')!, { target: { files: [file] } })
  return view
}

test('shows upload state before the first chunk responds and tracks in-flight bytes', async () => {
  vi.mocked(api.uploadChunk).mockImplementation(() => new Promise(() => {}))
  const { container } = selectFile()
  await waitFor(() => expect(api.uploadChunk).toHaveBeenCalledTimes(1))
  expect(screen.queryByText(/校验文件/)).toBeNull()
  expect(screen.getByText(/上传中/)).toBeTruthy()
  const options = vi.mocked(api.uploadChunk).mock.calls[0][3]
  act(() => options?.onProgress?.(8))
  expect(container.querySelector('.meter i')?.getAttribute('style')).toContain('50%')
  expect(api.mergeChunks).not.toHaveBeenCalled()
})

test('completed assets reach merge without uploading bytes again', async () => {
  vi.mocked(api.checkUpload).mockResolvedValue({ status: 'completed', uploaded: [] })
  selectFile()
  await waitFor(() => expect(api.mergeChunks).toHaveBeenCalledTimes(1))
  expect(api.uploadChunk).not.toHaveBeenCalled()
})

test('resume skips saved chunks and counts their actual size', async () => {
  const chunkSize = 5 * 1024 * 1024
  vi.mocked(api.checkUpload).mockResolvedValue({ status: 'uploading', uploaded: [0] })
  vi.mocked(api.uploadChunk).mockImplementation(() => new Promise(() => {}))
  const { container } = selectFile(chunkSize + 16)
  await waitFor(() => expect(api.uploadChunk).toHaveBeenCalledTimes(1))
  expect(vi.mocked(api.uploadChunk).mock.calls[0][1]).toBe(1)
  expect(vi.mocked(api.uploadChunk).mock.calls[0][2].size).toBe(16)
  expect(container.querySelector('.meter i')?.getAttribute('style')).toContain('96%')
})

test('closing cancels an active upload and never starts merge', async () => {
  vi.mocked(api.uploadChunk).mockImplementation(() => new Promise(() => {}))
  const view = selectFile()
  await waitFor(() => expect(api.uploadChunk).toHaveBeenCalledTimes(1))
  const signal = vi.mocked(api.uploadChunk).mock.calls[0][3]?.signal
  view.unmount()
  expect(signal?.aborted).toBe(true)
  expect(api.mergeChunks).not.toHaveBeenCalled()
})
