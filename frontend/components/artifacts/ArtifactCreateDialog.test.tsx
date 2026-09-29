// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, expect, test, vi } from 'vitest'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { api } from '@/lib/api'
import { artifactApi } from '@/lib/artifacts/api'
import type { VideoTask, VideoTimeline } from '@/lib/types'
import { ArtifactCreateDialog } from './ArtifactCreateDialog'

vi.mock('@/lib/router', () => ({ useRouter: () => ({ push: vi.fn() }) }))
vi.mock('@/components/settings/useAIAvailability', () => ({ useAIAvailability: () => ({ ready: true }) }))
afterEach(() => { cleanup(); vi.restoreAllMocks() })
function show(task: Partial<VideoTask>, timeline: Partial<VideoTimeline>) {
  vi.spyOn(api, 'getTask').mockResolvedValue({ id: 42, status: 3, has_transcription: false, ...task } as VideoTask)
  vi.spyOn(api, 'getTimeline').mockResolvedValue({ task_id: 42, atoms: [], ...timeline })
  const client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } })
  return render(<QueryClientProvider client={client}><ArtifactCreateDialog source={{ id: 42, title: '无声课件' }} onClose={vi.fn()} /></QueryClientProvider>)
}
test('a visual-only source can generate after server confirms source readiness', async () => {
  show({}, { study_source_ready: true, atoms: [{ id: 'slide', content: '事务', modality: 'visual_ocr', start_ms: 0, end_ms: 1, time_range_status: 'exact' }] })
  const button = screen.getByRole('button', { name: '开始后台生成' })
  await waitFor(() => expect((button as HTMLButtonElement).disabled).toBe(false))
  expect(screen.getByText(/来源已就绪/)).toBeTruthy()
})
test('a queued source cannot generate even with old readable content', async () => {
  const generate = vi.spyOn(artifactApi, 'generate')
  show({ status: 1, has_transcription: true }, { study_source_ready: false, study_source_reason: 'processing' })
  await screen.findByText('视频仍在处理，完成后可生成笔记')
  fireEvent.click(screen.getByRole('button', { name: '开始后台生成' }))
  expect(generate).not.toHaveBeenCalled()
})
test('a failed capability read stays blocked and offers retry', async () => {
  show({}, { study_source_ready: false, study_source_reason: 'no_content' })
  await screen.findByText('尚无可用内容，请先转写或分析画面')
  expect((screen.getByRole('button', { name: '开始后台生成' }) as HTMLButtonElement).disabled).toBe(true)
})
