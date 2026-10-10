// @vitest-environment jsdom
import { cleanup, render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router'
import { afterEach, expect, test, vi } from 'vitest'
import type { VideoTask } from '@/lib/types'
import LibraryPage from './page'

const fixture = vi.hoisted(() => ({ useQuery: vi.fn() }))
const tagList = vi.hoisted(() => ({ listTasks:vi.fn(),list:vi.fn().mockResolvedValue({list:[],total:0}) }))
vi.mock('@/lib/summaryTags', () => ({summaryTagsApi:tagList}))
vi.mock('@tanstack/react-query', () => ({ useQuery: fixture.useQuery }))
vi.mock('@/components/shell/AppShell', () => ({ useShell: () => ({ openUpload: vi.fn(), uploadRevision: 0 }), useCrumb: () => {} }))
vi.mock('@/lib/router', () => ({ default: ({ href, children, ...props }: React.AnchorHTMLAttributes<HTMLAnchorElement> & { href: string }) => <a href={href} {...props}>{children}</a> }))
vi.mock('@/components/VideoPoster', () => ({ VideoStill: () => <div>poster</div> }))
vi.mock('@/components/TranscriptionProgressPanel', () => ({ TranscriptionProgressPanel: () => null }))
vi.mock('@/components/VisualProgressPanel', () => ({ VisualProgressPanel: () => null }))

const base = { id: 91, user_id: 7, file_md5: 'x', filename: 'lecture.mp4', title: '示例视频', file_url: 'stored', file_size: 100,
  status: 3, stage: 'none', trace_id: '', source_type: 'upload', visual_disabled: true, retry_count: 0, max_retries: 3,
  last_error_code: '', last_error_msg: '', last_job_type: '', error_msg: '', created_at: '2026-09-29T00:00:00Z', updated_at: '2026-09-29T00:00:00Z',
  has_transcription: true, has_summary: false, has_rag_index: false, visual_status: '' } as VideoTask
afterEach(() => { cleanup(); vi.clearAllMocks() })

test('library shows and polls a completed video with queued summary child', () => {
  const task: VideoTask = { ...base, summary_job: { id: 92, job_type: 'summary', status: 1, stage: 'summarizing', retry_count: 0, max_retries: 3, last_error_code: '', last_error_msg: '', trace_id: '' } }
  fixture.useQuery.mockImplementation((options: { refetchInterval: (query: { state: { data: { list: VideoTask[] } } }) => number | false }) => {
    expect(options.refetchInterval({ state: { data: { list: [task] } } })).toBe(5000)
    return { data: { list: [task], total: 1 }, isPending: false, isFetching: false, error: null }
  })
  render(<MemoryRouter initialEntries={['/library?activity=processing']}><LibraryPage /></MemoryRouter>)
  expect(screen.getAllByText('摘要排队中')).toHaveLength(2)
  expect(screen.queryByText('转写已完成')).toBeNull()
})

test('library failed filter exposes a completed video whose summary child failed', () => {
  const task: VideoTask = { ...base, summary_job: { id: 92, job_type: 'summary', status: 4, stage: 'summarizing', retry_count: 1, max_retries: 3, last_error_code: 'timeout', last_error_msg: '', trace_id: '' } }
  fixture.useQuery.mockReturnValue({ data: { list: [task], total: 1 }, isPending: false, isFetching: false, error: null })
  render(<MemoryRouter initialEntries={['/library?activity=failed&view=list']}><LibraryPage /></MemoryRouter>)
  expect(screen.getByText('摘要生成失败')).toBeTruthy()
  expect(screen.getByRole('button', { name: '失败' }).getAttribute('aria-pressed')).toBe('true')
})

test('tag all/any filters reach the paginated server query and an empty result stays empty', async () => {
  fixture.useQuery.mockImplementation((options: {queryKey:unknown[];queryFn:(context:{signal:AbortSignal})=>unknown}) => {
    expect(options.queryKey).toContain('tag-a,tag-b')
    expect(options.queryKey).toContain('any')
    void options.queryFn({signal:new AbortController().signal})
    return {data:{list:[],total:0},isPending:false,isFetching:false,error:null}
  })
  tagList.listTasks.mockResolvedValue({list:[],total:0})
  render(<MemoryRouter initialEntries={['/library?page=1&tag_ids=tag-a,tag-b&tag_match=any']}><LibraryPage /></MemoryRouter>)
  expect(tagList.listTasks).toHaveBeenCalledWith(1,24,'','all',['tag-a','tag-b'],'any',expect.any(AbortSignal))
  expect(screen.getByText('没有匹配的视频')).toBeTruthy()
  expect(screen.queryByText('还没有视频')).toBeNull()
})
