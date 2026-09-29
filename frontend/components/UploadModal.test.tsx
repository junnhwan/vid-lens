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
  vi.mocked(api.importOptions).mockResolvedValue({ url_import_enabled: false, slow_upload_notice: false })
  vi.mocked(api.checkUpload).mockResolvedValue({ status: 'uploading', uploaded: [] })
  vi.mocked(api.mergeChunks).mockResolvedValue({ task_id: 42, status: 0 } as never)
})
afterEach(() => { cleanup(); vi.resetAllMocks() })

test('enabled link import is preferred unless a user already selected files',async()=>{
  vi.mocked(api.importOptions).mockResolvedValue({url_import_enabled:true,slow_upload_notice:false})
  const view=render(<UploadModal onClose={vi.fn()} />)
  expect(await screen.findByPlaceholderText(/https:\/\/www.bilibili/)).toBeTruthy()
  view.unmount()
  let resolve!:(value:{url_import_enabled:boolean;slow_upload_notice:boolean})=>void
  vi.mocked(api.importOptions).mockImplementation(()=>new Promise(done=>{resolve=done}))
  render(<UploadModal onClose={vi.fn()} />)
  fireEvent.click(screen.getByRole('button',{name:'本地文件'}))
  await act(async()=>resolve({url_import_enabled:true,slow_upload_notice:false}))
  expect(screen.getByRole('button',{name:'选择本地视频'})).toBeTruthy()
  expect(screen.queryByPlaceholderText(/https:\/\/www.bilibili/)).toBeNull()
})

test('online notice keeps file upload visible and links to Bilibili import', async () => {
  vi.mocked(api.importOptions).mockResolvedValue({ url_import_enabled: true, slow_upload_notice: true })
  render(<UploadModal onClose={vi.fn()} />)
  expect(await screen.findByText('线上文件上传较慢')).toBeTruthy()
  expect(screen.getByRole('button', { name: '选择本地视频' })).toBeTruthy()
  expect(screen.getByText(/B 站视频建议优先通过链接导入/)).toBeTruthy()
  fireEvent.click(screen.getByRole('button', { name: '使用 B 站链接' }))
  expect(screen.getByPlaceholderText(/https:\/\/www.bilibili/)).toBeTruthy()
  expect(screen.queryByText('线上文件上传较慢')).toBeNull()
})

test('online notice does not recommend an unavailable link import', async () => {
  vi.mocked(api.importOptions).mockResolvedValue({ url_import_enabled: false, slow_upload_notice: true })
  render(<UploadModal onClose={vi.fn()} />)
  expect(await screen.findByText('线上文件上传较慢')).toBeTruthy()
  expect(screen.queryByText(/B 站视频建议/)).toBeNull()
  expect(screen.queryByRole('button', { name: '使用 B 站链接' })).toBeNull()
  expect(screen.getByRole('button', { name: '选择本地视频' })).toBeTruthy()
})

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
