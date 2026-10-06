// @vitest-environment jsdom
import { act, cleanup, render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router'
import { afterEach, expect, test, vi } from 'vitest'
import { api } from '@/lib/api'
import type { TranscriptionProgress, VideoTask } from '@/lib/types'
import { TranscriptionProgressPanel } from './TranscriptionProgressPanel'
import { VideoCard } from './VideoCard'

vi.mock('@/lib/api', () => ({ api: { getTranscriptionProgress: vi.fn(), getVisualProgress: vi.fn() } }))
vi.mock('@/components/VideoPoster', () => ({ VideoStill: () => null }))

const task: VideoTask = {
  id: 60, user_id: 25, file_md5: 'test', filename: 'lesson.mp4', file_url: '', file_size: 1,
  status: 2, stage: 'transcribing', trace_id: '', source_type: 'upload', visual_disabled: false,
  retry_count: 0, max_retries: 3, last_error_code: '', last_error_msg: '', last_job_type: 'transcribe',
  error_msg: '', created_at: '2026-09-29T00:02:00Z', updated_at: '2026-09-29T00:02:00Z',
  has_transcription: true, has_summary: false, has_rag_index: false, visual_status: 'running',
}
const progress: TranscriptionProgress = {
  task_id: 60, status: 2, stage: 'transcribing', job_status: 2, job_retry_count: 0, job_max_retries: 3,
  started_at: task.created_at, updated_at: '2026-09-29T00:02:55Z', video_concurrency: 5, chunk_concurrency: 3,
  total: 3, completed: 3, pending: 0, running: 0, retry_waiting: 0, failed: 0,
  chunks: [1, 2, 3].map(index => ({ index, status: 'completed', start_ms: (index - 1) * 300000,
    end_ms: index * 300000, retry_count: 0, content: `分片 ${index}`, updated_at: '2026-09-29T00:02:55Z' })),
}

afterEach(() => { cleanup(); vi.restoreAllMocks(); vi.useRealTimers() })

test('completed ASR windows do not make a running sentence alignment look finished', async () => {
  vi.mocked(api.getTranscriptionProgress).mockResolvedValue({ ...progress, alignment_only: true, stage: 'aligning' })
  render(<TranscriptionProgressPanel task={{ ...task, stage: 'aligning' }} compact />)
  expect(await screen.findByText(/正在将已识别的文字与音频对齐/)).toBeTruthy()
  expect(screen.queryByText(/转写分片已完成/)).toBeNull()
  expect(screen.queryByText(/句子时间对齐已完成/)).toBeNull()
})

test('failed sentence alignment preserves completed ASR and reports the actual failed step', async () => {
  vi.mocked(api.getTranscriptionProgress).mockResolvedValue({ ...progress, alignment_only: true, stage: 'aligning', job_status: 4 })
  render(<TranscriptionProgressPanel task={{ ...task, status: 4, stage: 'aligning' }} compact />)
  expect(await screen.findByText(/句子时间对齐未完成，保留之前保存的转写与定位/)).toBeTruthy()
  expect(screen.queryByText(/转写分片已完成/)).toBeNull()
  expect(screen.queryByText(/转写失败/)).toBeNull()
})

test('completed ASR chunks are not reported as stalled while visual processing continues', async () => {
  vi.spyOn(Date, 'now').mockReturnValue(Date.parse('2026-09-29T00:07:00Z'))
  vi.mocked(api.getTranscriptionProgress).mockResolvedValue(progress)
  render(<TranscriptionProgressPanel task={task} compact />)
  await screen.findByText(/已完成 3\/3 个分片/)
  expect(screen.queryByText(/可能停滞/)).toBeNull()
  expect(screen.queryByText(/已等待\/运行/)).toBeNull()
  expect(screen.getByText(/转写分片已完成/)).toBeTruthy()
})

test('unfinished ASR still reports stale progress even when an older transcript exists', async () => {
  vi.spyOn(Date, 'now').mockReturnValue(Date.parse('2026-09-29T00:07:00Z'))
  vi.mocked(api.getTranscriptionProgress).mockResolvedValue({ ...progress, completed: 2, running: 1,
    chunks: progress.chunks.map((chunk, i) => i === 2 ? { ...chunk, status: 'running', content: undefined } : chunk) })
  render(<TranscriptionProgressPanel task={task} compact />)
  expect(await screen.findByText(/可能停滞/)).toBeTruthy()
})

test('failed chunks remain visible even when an older transcript made the task appear completed', async () => {
  vi.mocked(api.getTranscriptionProgress).mockResolvedValue({ ...progress, completed: 2, failed: 1,
    chunks: progress.chunks.map((chunk, i) => i === 2 ? { ...chunk, status: 'failed', content: undefined } : chunk) })
  render(<TranscriptionProgressPanel task={{ ...task, status: 3, stage: 'none' }} compact />)
  expect(await screen.findByText(/失败 1/)).toBeTruthy()
  expect(screen.getByText(/第 3\/3 段.*转写失败/)).toBeTruthy()
  expect(screen.getByText(/本次转写未完成/)).toBeTruthy()
  expect(screen.queryByText(/转写分片已完成/)).toBeNull()
})

test('queued transcription remains active before the worker sets a stage', async () => {
  vi.useFakeTimers()
  vi.mocked(api.getTranscriptionProgress).mockResolvedValue({ ...progress, completed: 2, pending: 1,
    chunks: progress.chunks.map((chunk, i) => i === 2 ? { ...chunk, status: 'pending', content: undefined } : chunk) })
  const { unmount } = render(<TranscriptionProgressPanel task={{ ...task, status: 1, stage: 'none' }} compact />)
  await act(async () => {})
  expect(screen.getByText(/等待任务启动或转写并发名额/)).toBeTruthy()
  expect(screen.queryByText(/本次转写未完成/)).toBeNull()
  const requests = vi.mocked(api.getTranscriptionProgress).mock.calls.length
  await act(async () => { await vi.advanceTimersByTimeAsync(5000) })
  expect(vi.mocked(api.getTranscriptionProgress).mock.calls.length).toBeGreaterThan(requests)
  unmount()
  expect(vi.getTimerCount()).toBe(0)
})

test('video card keeps visual progress visible and polling after leaving the ASR stage', async () => {
  vi.useFakeTimers()
  vi.setSystemTime(new Date('2026-09-29T00:07:00Z'))
  vi.mocked(api.getTranscriptionProgress).mockResolvedValue(progress)
  const visual = { task_id: 60, status: 'running' as const, phase: 'observing_frames', total_frames: 25,
    processed_frames: 19, failed_frames: 0, ocr_failed_frames: 0, vision_failed_frames: 0 }
  vi.mocked(api.getVisualProgress).mockResolvedValue(visual)
  const { rerender, unmount } = render(<MemoryRouter><VideoCard task={task} /></MemoryRouter>)
  await act(async () => {})
  expect(screen.getByText('ASR 转写中')).toBeTruthy()

  rerender(<MemoryRouter><VideoCard task={{ ...task, stage: 'visual_indexing' }} /></MemoryRouter>)
  await act(async () => {})
  expect(screen.getByText('画面分析中')).toBeTruthy()
  expect(screen.queryByText('ASR 转写中')).toBeNull()
  expect(screen.getByText(/已处理 19\/25 帧/)).toBeTruthy()
  expect(screen.queryByText(/可能停滞/)).toBeNull()

  vi.mocked(api.getVisualProgress).mockResolvedValue({ ...visual, processed_frames: 23 })
  await act(async () => { await vi.advanceTimersByTimeAsync(5000) })
  expect(screen.getByText(/已处理 23\/25 帧/)).toBeTruthy()
  unmount()
  expect(vi.getTimerCount()).toBe(0)
})
