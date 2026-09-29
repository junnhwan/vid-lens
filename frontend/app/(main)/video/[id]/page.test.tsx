// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, expect, test, vi } from 'vitest'
import type { VideoTask } from '@/lib/types'
import VideoWorkbenchPage from './page'

const mock = vi.hoisted(() => ({
  getTask: vi.fn(), getTimeline: vi.fn(), getRagIndex: vi.fn(), playbackSrc: vi.fn(), downloadMedia: vi.fn(),
  toast: { info: vi.fn(), error: vi.fn(), success: vi.fn() },
}))
vi.mock('@/lib/api', () => ({ api: mock, ApiError: class ApiError extends Error {} }))
vi.mock('@/lib/router', () => ({ default: ({ href, children }: { href: string; children: React.ReactNode }) => <a href={href}>{children}</a>, useRouter: () => ({ push: vi.fn() }) }))
vi.mock('@/components/shell/AppShell', () => ({ useShell: () => ({ user: { role: 'USER' } }), useCrumb: () => {} }))
vi.mock('@/components/Toast', () => ({ useToast: () => mock.toast }))
vi.mock('@/components/settings/useAIAvailability', () => ({ useAIAvailability: () => ({ ready: false, reason: 'AI 暂不可用' }) }))
vi.mock('@/lib/artifacts/useStudyPosition', () => ({ useStudyPosition: () => ({ error: '', record: vi.fn(), flush: vi.fn() }) }))
vi.mock('@/lib/artifacts/api', () => ({ artifactApi: { list: vi.fn().mockResolvedValue({ list: [], total: 0 }) }, artifactError: () => 'error' }))
vi.mock('@tanstack/react-query', () => ({ useQuery: () => ({ data: { list: [], total: 0 }, error: null, refetch: vi.fn() }) }))
vi.mock('@/components/player/VideoPlayer', async () => { const { forwardRef } = await import('react'); return { VideoPlayer: forwardRef(() => <div>player</div>) } })

const task: VideoTask = {
  id: 42, user_id: 7, file_md5: 'a'.repeat(32), filename: 'lesson.mp4', file_url: 'stored', file_size: 100,
  status: 3, stage: 'none', trace_id: 'test', source_type: 'upload', visual_disabled: true, retry_count: 0, max_retries: 3,
  last_error_code: '', last_error_msg: '', last_job_type: '', error_msg: '', created_at: '2026-09-29T00:00:00Z', updated_at: '2026-09-29T00:00:00Z',
  has_transcription: false, has_summary: false, has_rag_index: false, visual_status: '',
}
afterEach(() => { cleanup(); vi.resetAllMocks(); vi.restoreAllMocks() })

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
