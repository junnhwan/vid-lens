// @vitest-environment jsdom
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, cleanup, fireEvent, render as testingRender, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, expect, test, vi } from 'vitest'
import type { TranscriptionProgress, VideoTask } from '@/lib/types'
import VideoWorkbenchPage from './page'

const mock = vi.hoisted(() => ({
  artifactList: vi.fn(), getVisualProgress: vi.fn(), getToken: () => 'fixture-token', getTask: vi.fn(), getTimeline: vi.fn(), getRagIndex: vi.fn(), playbackSrc: vi.fn(), downloadMedia: vi.fn(),
  transcribe: vi.fn(), alignTranscript: vi.fn(), getTranscriptionProgress: vi.fn(), seek: vi.fn(), preflight: vi.fn(), aiReady: false,
  onPlayhead: undefined as ((ms: number, playing: boolean) => void) | undefined,
  toast: { info: vi.fn(), error: vi.fn(), success: vi.fn() },
}))
vi.mock('@/lib/api', () => ({ api: mock, getToken: mock.getToken, ApiError: class ApiError extends Error {} }))
vi.mock('@/lib/router', () => ({ default: ({ href, children }: { href: string; children: React.ReactNode }) => <a href={href}>{children}</a>, useRouter: () => ({ push: vi.fn() }) }))
vi.mock('@/components/shell/AppShell', () => ({ useShell: () => ({ user: { role: 'USER' } }), useCrumb: () => {} }))
vi.mock('@/components/Toast', () => ({ useToast: () => mock.toast }))
vi.mock('@/components/settings/useAIAvailability', () => ({ useAIAvailability: () => ({ ready: mock.aiReady, reason: 'AI 暂不可用' }) }))
vi.mock('@/components/settings/VideoAIPreflight', () => ({ useVideoAIPreflight: () => ({ request: (label: string, run: () => void, action: string) => { mock.preflight(label, action); run() }, dialog: null }) }))
vi.mock('@/lib/artifacts/useStudyPosition', () => ({ useStudyPosition: () => ({ error: '', record: vi.fn(), flush: vi.fn() }) }))
vi.mock('@/lib/artifacts/api', () => ({ artifactApi: { list: mock.artifactList }, artifactError: () => 'error' }))

vi.mock('@/components/player/VideoPlayer', async () => { const { forwardRef, useImperativeHandle } = await import('react'); return { VideoPlayer: forwardRef((props: { onPlayhead?: (ms: number, playing: boolean) => void }, ref) => { mock.onPlayhead = props.onPlayhead; useImperativeHandle(ref, () => ({ seek: mock.seek })); return <div>player</div> }) } })

const task: VideoTask = {
  id: 42, user_id: 7, file_md5: 'a'.repeat(32), filename: 'lesson.mp4', file_url: 'stored', file_size: 100,
  status: 3, stage: 'none', trace_id: 'test', source_type: 'upload', visual_disabled: true, retry_count: 0, max_retries: 3,
  last_error_code: '', last_error_msg: '', last_job_type: '', error_msg: '', created_at: '2026-09-29T00:00:00Z', updated_at: '2026-09-29T00:00:00Z',
  has_transcription: false, has_summary: false, has_rag_index: false, visual_status: '',
}
const unfinishedProgress: TranscriptionProgress = {
  task_id: 42, status: 3, stage: 'none', job_status: 3, job_retry_count: 0, job_max_retries: 3,
  updated_at: task.updated_at, video_concurrency: 5, chunk_concurrency: 3,
  total: 18, completed: 17, pending: 0, running: 0, retry_waiting: 0, failed: 1,
  chunks: Array.from({ length: 18 }, (_, i) => ({ index: i + 1, status: i === 14 ? 'failed' : 'completed',
    start_ms: i * 20000, end_ms: (i + 1) * 20000, retry_count: 0, updated_at: task.updated_at })),
}
function render(ui: React.ReactNode) {
 const client = new QueryClient({ defaultOptions: { queries: { retry:false, gcTime:0 } } })
 return testingRender(ui, { wrapper: ({ children }) => <QueryClientProvider client={client}>{children}</QueryClientProvider> })
}
beforeEach(() => {
 mock.artifactList.mockResolvedValue({list:[],total:0})
 mock.getVisualProgress.mockResolvedValue(null)
  vi.stubGlobal('ResizeObserver', class { observe() {} disconnect() {} })
  HTMLElement.prototype.scrollIntoView = vi.fn()
})
afterEach(() => { cleanup(); vi.resetAllMocks(); vi.restoreAllMocks(); vi.unstubAllGlobals(); mock.aiReady = false; mock.onPlayhead = undefined })

test('sentence alignment confirms reuse and invokes alignment without forced ASR', async () => {
  mock.aiReady = true
  mock.getTask.mockResolvedValue({ ...task, has_transcription: true })
  mock.getTimeline.mockResolvedValue({ task_id: 42, alignment_available: true, atoms: [{ id: 'window', modality: 'transcript', content: '已有的转写文字。', start_ms: 0, end_ms: 22000, time_range_status: 'coarse' }] })
  mock.getRagIndex.mockResolvedValue({ task_id: 42, status: 'indexed', indexed: true, chunks: 1 })
  mock.playbackSrc.mockResolvedValue('/playback')
  mock.alignTranscript.mockResolvedValue({ task_id: 42 })
  render(<VideoWorkbenchPage params={{ id: '42' }} />)
  fireEvent.click(await screen.findByRole('button', { name: '对齐句子时间' }))
  expect(screen.getByText(/对齐不会再次调用语音识别/)).toBeTruthy()
  expect(mock.alignTranscript).not.toHaveBeenCalled()
  fireEvent.click(screen.getByRole('button', { name: '开始对齐' }))
  await waitFor(() => expect(mock.alignTranscript).toHaveBeenCalledWith(42))
  expect(mock.transcribe).not.toHaveBeenCalled()
})

test('aligned words across a window form one reading sentence and seek its observed start', async () => {
  mock.getTask.mockResolvedValue({ ...task, has_transcription: true })
  mock.getTimeline.mockResolvedValue({ task_id: 42, atoms: [
    { id: 'first', modality: 'transcript', content: '这是', start_ms: 19040, end_ms: 19760, time_range_status: 'exact' },
    { id: 'second', modality: 'transcript', content: '完整的一句话。', start_ms: 19840, end_ms: 22160, time_range_status: 'exact' },
  ] })
  mock.getRagIndex.mockResolvedValue({ task_id: 42, status: 'indexed', indexed: true, chunks: 1 })
  mock.playbackSrc.mockResolvedValue('/playback')
  render(<VideoWorkbenchPage params={{ id: '42' }} />)
  expect(await screen.findByText('这是完整的一句话。')).toBeTruthy()
  fireEvent.click(screen.getByRole('button', { name: '回放 00:19' }))
  expect(mock.seek).toHaveBeenCalledWith(19040, true, undefined)
  expect(screen.queryByText('约定位')).toBeNull()
})

test('failed alignment retry stays alignment-only and retains previous text', async () => {
  mock.aiReady = true
  mock.getTask.mockResolvedValue({ ...task, has_transcription: true, status: 4, stage: 'aligning', last_job_type: 'transcribe' })
  mock.getTimeline.mockResolvedValue({ task_id: 42, atoms: [{ id: 'window', modality: 'transcript', content: '原先保存的转写。', start_ms: 0, end_ms: 22000, time_range_status: 'coarse' }] })
  mock.getRagIndex.mockResolvedValue({ task_id: 42, status: 'indexed', indexed: true, chunks: 1 })
  mock.playbackSrc.mockResolvedValue('/playback')
  mock.getTranscriptionProgress.mockResolvedValue({ ...unfinishedProgress, alignment_only: true, job_status: 4 })
  mock.alignTranscript.mockResolvedValue({ task_id: 42 })
  render(<VideoWorkbenchPage params={{ id: '42' }} />)
  expect(await screen.findByText('句子时间对齐未完成，保留之前保存的转写与定位。')).toBeTruthy()
  expect(screen.queryByRole('button', { name: '重试补齐引用定位' })).toBeNull()
  expect(screen.getByText('原先保存的转写。')).toBeTruthy()
  fireEvent.click(screen.getByRole('button', { name: /^重试$/ }))
  fireEvent.click(screen.getAllByRole('button', { name: /^重试$/ })[1])
  await waitFor(() => expect(mock.alignTranscript).toHaveBeenCalledWith(42))
  expect(mock.transcribe).not.toHaveBeenCalled()
})

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

test('retained transcript does not hide running replacement transcription progress', async () => {
  mock.getTask.mockResolvedValue({ ...task, has_transcription: true, status: 2, stage: 'transcribing', last_job_type: 'transcribe' })
  mock.getTimeline.mockResolvedValue({ task_id: 42, atoms: [{ id: 'old', modality: 'transcript', content: '旧转写。', start_ms: 0, end_ms: 305000, time_range_status: 'coarse' }] })
  mock.getRagIndex.mockResolvedValue({ task_id: 42, status: 'indexed', indexed: true, chunks: 1 })
  mock.playbackSrc.mockResolvedValue('/playback')
  mock.getTranscriptionProgress.mockResolvedValue({ ...unfinishedProgress, status: 2, stage: 'transcribing', job_status: 2, failed: 0, running: 1,
    chunks: unfinishedProgress.chunks.map(c => c.status === 'failed' ? { ...c, status: 'running' } : c) })
  render(<VideoWorkbenchPage params={{ id: '42' }} />)
  expect(await screen.findByText(/已完成 17\/18 个分片/)).toBeTruthy()
  expect(screen.queryByText('转写与检索已就绪，可提问并核对引用。')).toBeNull()
  expect(screen.getByText(/完成前仍使用之前保存的内容/)).toBeTruthy()
})

test.each([3, 4] as const)('failed replacement remains visible with old transcript and task status %s, and citation retry retains completed chunks', async status => {
  mock.aiReady = true
  mock.getTask.mockResolvedValue({ ...task, has_transcription: true, status, last_job_type: 'transcribe' })
  mock.getTimeline.mockResolvedValue({ task_id: 42, atoms: [{ id: 'old', modality: 'transcript', content: '旧转写。', start_ms: 0, end_ms: 305000, time_range_status: 'coarse' }] })
  mock.getRagIndex.mockResolvedValue({ task_id: 42, status: 'indexed', indexed: true, chunks: 1 })
  mock.playbackSrc.mockResolvedValue('/playback')
  mock.getTranscriptionProgress.mockResolvedValue({ ...unfinishedProgress, status, job_status: status })
  mock.transcribe.mockResolvedValue({ task_id: 42 })
  render(<VideoWorkbenchPage params={{ id: '42' }} />)
  expect(await screen.findByText(/已完成 17\/18 个分片/)).toBeTruthy()
  expect(screen.getByText(/失败 1/)).toBeTruthy()
  expect(screen.getByText(/当前仍使用之前保存的转写与引用定位/)).toBeTruthy()
  expect(screen.queryByText('转写与检索已就绪，可提问并核对引用。')).toBeNull()
  fireEvent.click(screen.getByRole('button', { name: '重试补齐引用定位' }))
  expect(screen.getByText(/保留已完成的转写分片/)).toBeTruthy()
  fireEvent.click(screen.getAllByRole('button', { name: '重试补齐引用定位' })[1])
  await waitFor(() => expect(mock.transcribe).toHaveBeenCalledWith(42, false))
  expect(mock.transcribe).not.toHaveBeenCalledWith(42, true)
})

test('more operations resumes unfinished transcription without deleting successful chunks', async () => {
  mock.aiReady = true
  mock.getTask.mockResolvedValue({ ...task, has_transcription: true, last_job_type: 'transcribe' })
  mock.getTimeline.mockResolvedValue({ task_id: 42, atoms: [{ id: 'old', modality: 'transcript', content: '旧转写。', start_ms: 0, end_ms: 305000, time_range_status: 'coarse' }] })
  mock.getRagIndex.mockResolvedValue({ task_id: 42, status: 'indexed', indexed: true, chunks: 1 })
  mock.playbackSrc.mockResolvedValue('/playback')
  mock.getTranscriptionProgress.mockResolvedValue(unfinishedProgress)
  mock.transcribe.mockResolvedValue({ task_id: 42 })
  render(<VideoWorkbenchPage params={{ id: '42' }} />)
  await screen.findByText(/已完成 17\/18 个分片/)
  fireEvent.click(screen.getByRole('button', { name: /更多操作/ }))
  fireEvent.click(screen.getByRole('button', { name: /继续转写/ }))
  expect(screen.getByText(/保留已完成的转写分片/)).toBeTruthy()
  fireEvent.click(screen.getByRole('button', { name: '继续转写' }))
  await waitFor(() => expect(mock.transcribe).toHaveBeenCalledWith(42, false))
  expect(mock.transcribe).not.toHaveBeenCalledWith(42, true)
})

test('citation recovery remains available when partial short chunks replaced the timeline but the transcript and index are still old', async () => {
  mock.aiReady = true
  mock.getTask.mockResolvedValue({ ...task, has_transcription: true, last_job_type: 'transcribe' })
  mock.getTimeline.mockResolvedValue({ task_id: 42, atoms: unfinishedProgress.chunks.filter(c => c.status === 'completed').map(c => ({
    id: `short-${c.index}`, modality: 'transcript', content: `新短窗 ${c.index}`, start_ms: c.start_ms, end_ms: c.end_ms, time_range_status: 'coarse',
  })) })
  mock.getRagIndex.mockResolvedValue({ task_id: 42, status: 'indexed', indexed: true, chunks: 1 })
  mock.playbackSrc.mockResolvedValue('/playback')
  mock.getTranscriptionProgress.mockResolvedValue(unfinishedProgress)
  mock.transcribe.mockResolvedValue({ task_id: 42 })
  render(<VideoWorkbenchPage params={{ id: '42' }} />)
  await screen.findByText(/已完成 17\/18 个分片/)
  expect(screen.queryByText('旧转写的引用定位可以补齐')).toBeNull()
  fireEvent.click(screen.getByRole('button', { name: '重试补齐引用定位' }))
  expect(screen.getByText(/保留已完成的转写分片/)).toBeTruthy()
  fireEvent.click(screen.getAllByRole('button', { name: '重试补齐引用定位' })[1])
  await waitFor(() => expect(mock.transcribe).toHaveBeenCalledWith(42, false))
})

test('unavailable progress prevents a destructive upgrade until the saved chunks can be checked again', async () => {
  mock.aiReady = true
  mock.getTask.mockResolvedValue({ ...task, has_transcription: true, last_job_type: 'transcribe' })
  mock.getTimeline.mockResolvedValue({ task_id: 42, atoms: [{ id: 'old', modality: 'transcript', content: '旧转写。', start_ms: 0, end_ms: 305000, time_range_status: 'coarse' }] })
  mock.getRagIndex.mockResolvedValue({ task_id: 42, status: 'indexed', indexed: true, chunks: 1 })
  mock.playbackSrc.mockResolvedValue('/playback')
  mock.getTranscriptionProgress.mockRejectedValueOnce(new Error('unavailable')).mockResolvedValue(unfinishedProgress)
  render(<VideoWorkbenchPage params={{ id: '42' }} />)
  await screen.findByText(/转写进度暂不可用/)
  expect((screen.getByRole('button', { name: '补齐引用定位' }) as HTMLButtonElement).disabled).toBe(true)
  expect(mock.transcribe).not.toHaveBeenCalled()
  fireEvent.click(screen.getByRole('button', { name: '重新读取转写进度' }))
  const resume = await screen.findByRole('button', { name: '重试补齐引用定位' })
  expect((resume as HTMLButtonElement).disabled).toBe(false)
})

test('legacy paragraphs share one time and one replay button, while native intervals stay distinct', async () => {
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
  expect(rows[0].querySelector('.ts')?.textContent).toBe('00:12 – 00:15')
  fireEvent.click(rows[0].querySelector('button')!)
  expect(mock.seek).toHaveBeenLastCalledWith(12000, true, undefined)
  expect(rows).toHaveLength(2)
  expect(rows[1].querySelector('.ts')?.textContent).toBe('00:30 – 05:35原片段')
  expect(rows[1].querySelectorAll('.transcript-paragraphs p').length).toBeGreaterThan(1)
  expect(rows[1].querySelector('.transcript-paragraphs')?.textContent).toBe(longText)
  expect(rows[1].querySelectorAll('.transcript-time')).toHaveLength(1)
  fireEvent.click(rows[1].querySelector('button')!)
  expect(mock.seek).toHaveBeenLastCalledWith(30000, true, undefined)
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


test('OCR build preflight checks the local OCR action instead of chat models', async () => {
  mock.aiReady = true
  mock.getTask.mockResolvedValue({ ...task, has_transcription: true })
  mock.getTimeline.mockResolvedValue({ task_id: 42, atoms: [], visual_build_available: true })
  mock.getRagIndex.mockResolvedValue({ task_id: 42, status: 'not_indexed', indexed: false, chunks: 0 })
  mock.playbackSrc.mockResolvedValue('/playback')
  render(<VideoWorkbenchPage params={{ id: '42' }} />)
  fireEvent.click(await screen.findByRole('button', { name: /画面分析 关闭/ }))
  fireEvent.click(screen.getByRole('radio', { name: /文字识别 OCR/ }))
  fireEvent.click(screen.getByRole('button', { name: '保存并生成画面' }))
  expect(mock.preflight).toHaveBeenCalledWith('生成画面证据', 'ocr')
})


test('switching videos aborts old reads and rejects late data', async () => {
 let finishOld!: (task:VideoTask)=>void
 mock.getTask.mockImplementation((id:number) => id===42 ? new Promise(resolve=>{finishOld=resolve}) : Promise.resolve({...task,id:43,filename:'new-video.mp4'}))
 mock.getTimeline.mockResolvedValue({task_id:43,atoms:[]})
 mock.getRagIndex.mockResolvedValue({status:'not_indexed',indexed:false,chunks:0})
 mock.playbackSrc.mockResolvedValue('/new-playback')
 const view=render(<VideoWorkbenchPage params={{id:'42'}} />)
 await waitFor(()=>expect(mock.getTask).toHaveBeenCalledTimes(1))
 const oldSignal=mock.getTask.mock.calls[0][1] as AbortSignal
 view.rerender(<VideoWorkbenchPage params={{id:'43'}} />)
 expect(await screen.findByText('new-video.mp4')).toBeTruthy()
 expect(oldSignal.aborted).toBe(true)
 await act(async()=>finishOld({...task,filename:'late-old-video.mp4'}))
 expect(screen.queryByText('late-old-video.mp4')).toBeNull()
 expect(screen.getByText('new-video.mp4')).toBeTruthy()
 view.unmount()
})

test('repeated transcription clicks submit one job while the request is pending', async () => {
 mock.aiReady=true
 mock.getTask.mockResolvedValue(task)
 mock.getTimeline.mockResolvedValue({task_id:42,atoms:[]})
 mock.getRagIndex.mockResolvedValue({status:'not_indexed',indexed:false,chunks:0})
 mock.playbackSrc.mockResolvedValue('/playback')
 let finish!: (v:{task_id:number})=>void
 mock.transcribe.mockImplementation(()=>new Promise(resolve=>{finish=resolve}))
 render(<VideoWorkbenchPage params={{id:'42'}} />)
 const button=await screen.findByRole('button',{name:'开始转写'})
 fireEvent.click(button);fireEvent.click(button)
 expect(mock.transcribe).toHaveBeenCalledTimes(1)
 await act(async()=>finish({task_id:42}))
})
