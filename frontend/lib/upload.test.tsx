// @vitest-environment jsdom
import { afterEach, beforeEach, expect, test, vi } from 'vitest'

class UploadXHR {
  static requests: UploadXHR[] = []
  upload = { onprogress: null as ((event: { lengthComputable: boolean; loaded: number; total: number }) => void) | null }
  onload?: () => void
  onerror?: () => void
  ontimeout?: () => void
  onabort?: () => void
  timeout = 0
  status = 200
  statusText = 'OK'
  responseText = '{"code":200,"data":{"chunk_number":0}}'
  headers: Record<string, string> = {}
  url = ''
  body?: FormData
  constructor() { UploadXHR.requests.push(this) }
  open(_method: string, url: string) { this.url = url }
  setRequestHeader(name: string, value: string) { this.headers[name] = value }
  send(body: FormData) { this.body = body }
  abort() { this.onabort?.() }
}

beforeEach(() => {
  vi.resetModules()
  const storage = new Map<string, string>()
  vi.stubGlobal('localStorage', { getItem: (key: string) => storage.get(key) ?? null, setItem: (key: string, value: string) => storage.set(key, value), removeItem: (key: string) => storage.delete(key), clear: () => storage.clear() })
  UploadXHR.requests = []
  vi.stubGlobal('XMLHttpRequest', UploadXHR)
  vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true, status: 200, json: async () => ({ code: 200, data: {} }) }))
  vi.stubEnv('VITE_UPLOAD_API_BASE', 'https://upload.example.com/api/v1/')
  localStorage.setItem('vidlens-token', 'test-session')
})
afterEach(() => { localStorage.clear(); vi.unstubAllGlobals(); vi.unstubAllEnvs() })

test('media downloads use the same-origin API prefix even with a separate upload host', async () => {
  const { api } = await import('./api')
  vi.mocked(fetch).mockResolvedValueOnce({ ok: true, status: 200, json: async () => ({ code: 200, data: { download_url: '/media/task/31/download?token=task-credential', filename: '视频.mp4' } }) } as Response)
  const result = await api.downloadMedia(31)
  expect(fetch).toHaveBeenCalledWith('/api/v1/media/download-audio/31', expect.objectContaining({ headers: expect.objectContaining({ Authorization: 'Bearer test-session' }) }))
  expect(result).toEqual({ download_url: '/api/v1/media/task/31/download?token=task-credential', filename: '视频.mp4' })
})

test('only chunk-upload routes use the configured origin and keep session authentication', async () => {
  const { api } = await import('./api')
  await api.checkUpload('md5', 10, 10, 1)
  await api.mergeChunks({ file_md5: 'md5', filename: 'video.mp4', total_chunks: 1, file_size: 10, chunk_size: 10 })
  await api.profile()
  expect(vi.mocked(fetch).mock.calls.map(call => call[0])).toEqual([
    'https://upload.example.com/api/v1/media/check-upload?file_md5=md5&file_size=10&chunk_size=10&total_chunks=1',
    'https://upload.example.com/api/v1/media/merge-chunks', '/api/v1/user/profile',
  ])
  const pending = api.uploadChunk('md5', 0, new Blob(['0123456789']))
  const xhr = UploadXHR.requests[0]
  expect(xhr.url).toBe('https://upload.example.com/api/v1/media/upload-chunk')
  expect(xhr.headers).toEqual({ Authorization: 'Bearer test-session' })
  expect(xhr.body?.get('file_md5')).toBe('md5')
  expect(xhr.body?.get('chunk_number')).toBe('0')
  xhr.onload?.()
  await expect(pending).resolves.toEqual({ chunk_number: 0 })
})

test('without upload configuration local development stays same-origin', async () => {
  vi.stubEnv('VITE_UPLOAD_API_BASE', '')
  const { api } = await import('./api')
  const pending = api.uploadChunk('md5', 0, new Blob(['123']))
  expect(UploadXHR.requests[0].url).toBe('/api/v1/media/upload-chunk')
  UploadXHR.requests[0].onload?.()
  await pending
})

test('progress updates during transmission but success waits for the server', async () => {
  const { api } = await import('./api')
  const progress = vi.fn()
  const resolved = vi.fn()
  const pending = api.uploadChunk('md5', 0, new Blob(['0123456789']), { onProgress: progress }).then(resolved)
  const xhr = UploadXHR.requests[0]
  xhr.upload.onprogress?.({ lengthComputable: true, loaded: 600, total: 1200 })
  expect(progress).toHaveBeenCalledWith(5)
  xhr.upload.onprogress?.({ lengthComputable: true, loaded: 1200, total: 1200 })
  await Promise.resolve()
  expect(resolved).not.toHaveBeenCalled()
  xhr.onload?.()
  await pending
  expect(resolved).toHaveBeenCalledWith({ chunk_number: 0 })
})

test.each(['onerror', 'ontimeout'] as const)('%s produces a recoverable upload error', async event => {
  const { api } = await import('./api')
  const pending = api.uploadChunk('md5', 0, new Blob(['123']))
  expect(UploadXHR.requests[0].timeout).toBe(120_000)
  UploadXHR.requests[0][event]?.()
  await expect(pending).rejects.toThrow('同一文件续传')
})

test('abort cancels the request and already-aborted signals never send', async () => {
  const { api } = await import('./api')
  const controller = new AbortController()
  const pending = api.uploadChunk('md5', 0, new Blob(['123']), { signal: controller.signal })
  controller.abort()
  await expect(pending).rejects.toMatchObject({ name: 'AbortError' })
  await expect(api.uploadChunk('md5', 1, new Blob(['123']), { signal: controller.signal })).rejects.toMatchObject({ name: 'AbortError' })
  expect(UploadXHR.requests).toHaveLength(1)
})

test('server failures never count as successful chunks', async () => {
  const { api } = await import('./api')
  const pending = api.uploadChunk('md5', 0, new Blob(['123']))
  Object.assign(UploadXHR.requests[0], { status: 500, responseText: '{"code":500,"message":"分片落盘失败"}' })
  UploadXHR.requests[0].onload?.()
  await expect(pending).rejects.toThrow('分片落盘失败')
})
