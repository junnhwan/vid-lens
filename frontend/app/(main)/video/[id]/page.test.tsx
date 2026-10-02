// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, expect, test, vi } from 'vitest'
import type { VideoTask } from '@/lib/types'
import VideoWorkbenchPage from './page'

const mock = vi.hoisted(() => ({
  getTask: vi.fn(), getTimeline: vi.fn(), getRagIndex: vi.fn(), playbackSrc: vi.fn(), downloadMedia: vi.fn(),
  transcribe: vi.fn(), seek: vi.fn(), aiReady: false,
  onPlayhead: undefined as ((ms: number, playing: boolean) => void) | undefined,
  toast: { info: vi.fn(), error: vi.fn(), success: vi.fn() },
}))
vi.mock('@/lib/api', () => ({ api: mock, ApiError: class ApiError extends Error {} }))
vi.mock('@/lib/router', () => ({ default: ({ href, children }: { href: string; children: React.ReactNode }) => <a href={href}>{children}</a>, useRouter: () => ({ push: vi.fn() }) }))
vi.mock('@/components/shell/AppShell', () => ({ useShell: () => ({ user: { role: 'USER' } }), useCrumb: () => {} }))
vi.mock('@/components/Toast', () => ({ useToast: () => mock.toast }))
vi.mock('@/components/settings/useAIAvailability', () => ({ useAIAvailability: () => ({ ready: mock.aiReady, reason: 'AI 暂不可用' }) }))
vi.mock('@/components/settings/VideoAIPreflight', () => ({ useVideoAIPreflight: () => ({ request: (_label: string, run: () => void) => run(), dialog: null }) }))
vi.mock('@/lib/artifacts/useStudyPosition', () => ({ useStudyPosition: () => ({ error: '', record: vi.fn(), flush: vi.fn() }) }))
vi.mock('@/lib/artifacts/api', () => ({ artifactApi: { list: vi.fn().mockResolvedValue({ list: [], total: 0 }) }, artifactError: () => 'error' }))
vi.mock('@tanstack/react-query', () => ({ useQuery: () => ({ data: { list: [], total: 0 }, error: null, refetch: vi.fn() }) }))
vi.mock('@/components/player/VideoPlayer', async () => { const { forwardRef, useImperativeHandle } = await import('react'); return { VideoPlayer: forwardRef((props: { onPlayhead?: (ms: number, playing: boolean) => void }, ref) => { mock.onPlayhead = props.onPlayhead; useImperativeHandle(ref, () => ({ seek: mock.seek })); return <div>player</div> }) } })

const task: VideoTask = {
  id: 42, user_id: 7, file_md5: 'a'.repeat(32), filename: 'lesson.mp4', file_url: 'stored', file_size: 100,
  status: 3, stage: 'none', trace_id: 'test', source_type: 'upload', visual_disabled: true, retry_count: 0, max_retries: 3,
  last_error_code: '', last_error_msg: '', last_job_type: '', error_msg: '', created_at: '2026-09-29T00:00:00Z', updated_at: '2026-09-29T00:00:00Z',
  has_transcription: false, has_summary: false, has_rag_index: false, visual_status: '',
}
beforeEach(() => {
  vi.stubGlobal('ResizeObserver', class { observe() {} disconnect() {} })
  HTMLElement.prototype.scrollIntoView = vi.fn()
})
afterEach(() => { cleanup(); vi.resetAllMocks(); vi.restoreAllMocks(); vi.unstubAllGlobals(); mock.aiReady = false; mock.onPlayhead = undefined })

test('download of an existing video works when AI is unavailable', async () => {
  mock.getTask.mockResolvedValue(task)
  mock.getTimeline.mockResolvedValue(null)
  mock.getRagIndex.mockResolvedValue({ task_id: 42, status: 'not_indexed', indexed: false, chunks: 0 })
  mock.playbackSrc.mockResolvedValue('/playback')
  mock.downloadMedia.mockResolvedValue({ download_url: '/media-download', filename: 'lesson.mp4' })
  const click = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => {})
  render(<VideoWorkbenchPage params={{ id: '42' }} />)
  fireEvent.click(await screen.findByRole('button', { name: /更多操作/ }))
  fireEvent.click(screen.getByRole('button', { name: /下载视频/ }))
  await waitFor(() => expect(mock.downloadMedia).toHaveBeenCalledWith(42))
  expect(click).toHaveBeenCalledTimes(1)
  expect(mock.toast.info).not.toHaveBeenCalledWith('AI 暂不可用')
})

test('legacy citation upgrade requires a cost confirmation and requests a forced transcription', async () => {
  mock.aiReady = true
  mock.getTask.mockResolvedValue({ ...task, has_transcription: true })
  mock.getTimeline.mockResolvedValue({ task_id: 42, atoms: [{ id: 'old', modality: 'transcript', content: '旧转写只有整个原片段的时间。', start_ms: 0, end_ms: 305000, time_range_status: 'coarse' }] })
  mock.getRagIndex.mockResolvedValue({ task_id: 42, status: 'indexed', indexed: true, chunks: 1 })
  mock.playbackSrc.mockResolvedValue('/playback')
  mock.transcribe.mockResolvedValue({ task_id: 42 })
  render(<VideoWorkbenchPage params={{ id: '42' }} searchParams={{ citations: 'upgrade' }} />)
  const upgrade = await screen.findByRole('button', { name: '补齐引用定位' })
  expect(mock.transcribe).not.toHaveBeenCalled()
  fireEvent.click(upgrade)
  expect(screen.getByText(/可能产生新的 ASR 和 Embedding 费用/)).toBeTruthy()
  expect(mock.transcribe).not.toHaveBeenCalled()
  fireEvent.click(screen.getAllByRole('button', { name: '补齐引用定位' })[1])
  await waitFor(() => expect(mock.transcribe).toHaveBeenCalledWith(42, true))
})

test('native transcript rows use their source timestamps while legacy reading rows share the original window', async () => {
  const longText = Array.from({ length: 8 }, (_, i) => `这是第${i + 1}段旧转写，其中没有独立句子时间，只能保留真实来源片段。`).join('')
  mock.getTask.mockResolvedValue({ ...task, has_transcription: true })
  mock.getTimeline.mockResolvedValue({ task_id: 42, atoms: [
    { id: 'native', modality: 'transcript', content: '有时间戳的短句。', start_ms: 12000, end_ms: 15500, time_range_status: 'exact' },
    { id: 'old', modality: 'transcript', content: longText, start_ms: 30000, end_ms: 335000, time_range_status: 'coarse' },
  ] })
  mock.getRagIndex.mockResolvedValue({ task_id: 42, status: 'indexed', indexed: true, chunks: 2 })
  mock.playbackSrc.mockResolvedValue('/playback')
  const { container } = render(<VideoWorkbenchPage params={{ id: '42' }} />)
  await screen.findByText('有时间戳的短句。')
  const rows = [...container.querySelectorAll('.t-row')]
  expect(rows[0].querySelector('.ts')?.textContent).toBe('00:12')
  fireEvent.click(rows[0])
  expect(mock.seek).toHaveBeenLastCalledWith(12000, true, undefined)
  expect(rows.length).toBeGreaterThan(2)
  for (const row of rows.slice(1)) {
    expect(row.querySelector('.ts')?.textContent).toBe('00:30 – 05:35原片段')
    fireEvent.click(row)
    expect(mock.seek).toHaveBeenLastCalledWith(30000, true, undefined)
  }
})

test('native sentence takes live highlight precedence over an overlapping coarse window and unknown rows stay inactive', async () => {
  mock.getTask.mockResolvedValue({ ...task, has_transcription: true })
  mock.getTimeline.mockResolvedValue({ task_id: 42, atoms: [
    { id: 'gap', modality: 'transcript', content: '这部分文字只有原片段时间。', start_ms: 0, end_ms: 20000, time_range_status: 'coarse' },
    { id: 'native', modality: 'transcript', content: '这是五到七秒的原生句子。', start_ms: 5000, end_ms: 7000, time_range_status: 'exact' },
    { id: 'unknown', modality: 'transcript', content: '时间未知的文字。', start_ms: 0, end_ms: 40000, time_range_status: 'unknown' },
  ] })
  mock.getRagIndex.mockResolvedValue({ task_id: 42, status: 'indexed', indexed: true, chunks: 3 })
  mock.playbackSrc.mockResolvedValue('/playback')
  const { container } = render(<VideoWorkbenchPage params={{ id: '42' }} />)
  await screen.findByText('这是五到七秒的原生句子。')
  act(() => mock.onPlayhead?.(6000, false))
  expect(container.querySelectorAll('.t-row.live')).toHaveLength(1)
  expect(container.querySelector('.t-row.live')?.textContent).toContain('这是五到七秒的原生句子。')
  act(() => mock.onPlayhead?.(10000, false))
  expect(container.querySelector('.t-row.live')?.textContent).toContain('这部分文字只有原片段时间。')
  act(() => mock.onPlayhead?.(30000, false))
  expect(container.querySelector('.t-row.live')).toBeNull()
})
