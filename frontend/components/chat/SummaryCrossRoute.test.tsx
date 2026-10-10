// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { createRef } from 'react'
import { afterEach, beforeEach, expect, test, vi } from 'vitest'
import { SummaryWorkspace } from '@/components/summary/SummaryWorkspace'
import VideoChatPage from '@/app/(main)/chat/v/[id]/page'
import { clearSummaryViewState, getSummarySessionView, patchSummarySessionView, setSummaryViewOwner } from '@/lib/summaryViewState'
import type { EffectiveSummaryView, VideoTask } from '@/lib/types'
import type { VideoPlayerHandle } from '@/components/player/VideoPlayer'

const mocks = vi.hoisted(() => ({ request: (_name: string, run: () => void) => run(), owner: 7, listSessions: vi.fn(), getMessages: vi.fn(), getRunHistory: vi.fn(), getSummary: vi.fn(), block: vi.fn(), getTask: vi.fn(), createSession: vi.fn(), streamAsk: vi.fn(), playbackSrc: vi.fn(), questions: vi.fn() }))
vi.mock('@/components/shell/AppShell', () => ({ useShell: () => ({ user: { id: mocks.owner, role: 'USER' } }), useCrumb: () => {} }))
vi.mock('@/lib/router', () => ({ useRouter: () => ({ push: vi.fn() }), default: ({ href, children }: { href: string; children: React.ReactNode }) => <a href={href}>{children}</a> }))
vi.mock('@/components/Toast', () => ({ useToast: () => ({ info: vi.fn(), error: vi.fn(), success: vi.fn() }) }))
vi.mock('@/components/settings/useAIAvailability', () => ({ useAIAvailability: () => ({ ready: true, reason: '' }) }))
vi.mock('@/components/settings/VideoAIPreflight', () => ({ useVideoAIPreflight: () => ({ request: mocks.request, dialog: null }) }))
vi.mock('@/components/summary/useSummaryGeneration', () => ({ useSummaryGeneration: () => ({ generation: { status: 'completed', text_state: 'ready', result_state: 'ready', activities: [], mindmap_enabled: false }, error: '' }) }))
vi.mock('@/components/summary/SummaryTags', () => ({ SummaryTags: () => null }))
vi.mock('@/components/player/VideoPlayer', async () => ({ VideoPlayer: (await import('react')).forwardRef(() => null) }))
vi.mock('@/components/chat/AnswerFeedback', () => ({ AnswerFeedback: () => null }))
vi.mock('@/components/chat/FollowUpQuestions', () => ({ FollowUpQuestions: () => null }))
vi.mock('@/components/knowledge/RunDetails', () => ({ RunDetails: () => null, SessionMemoryControl: () => null }))
vi.mock('@/components/summary/SummaryRevisionPanel', () => ({ SummaryRevisionPanel: ({ renderContent }: { renderContent: (summary: EffectiveSummaryView) => React.ReactNode }) => <>{renderContent(summary)}</> }))
vi.mock('@/lib/summaryExperience', async original => ({ ...await original<object>(), summaryExperienceApi: { block: mocks.block } }))
vi.mock('@/lib/api', () => ({ ApiError: class ApiError extends Error {}, api: { listSessions: mocks.listSessions, getMessages: mocks.getMessages, getRunHistory: mocks.getRunHistory, getSummary: mocks.getSummary, getTask: mocks.getTask, createSession: mocks.createSession, playbackSrc: mocks.playbackSrc, generateVideoQuestions: mocks.questions }, streamAsk: mocks.streamAsk, streamAgent: vi.fn() }))
const task = { id: 42, filename: 'lesson.mp4', has_summary: true, has_transcription: false, active_text_source_id: 'source', file_md5: 'media', source_type: 'upload' } as VideoTask
const summary: EffectiveSummaryView = { task_id: 42, revision: 0, revision_id: '', base_generated_hash: '', current_generated_hash: 'digest', source_status: 'current', has_generated: true, has_revision: false, content: '有效摘要', content_digest: 'digest', version_ref: { generated_version: 2 }, document: { schema_version: 'summary-v2', document_id: 'doc', source_id: 'source', source_digest: 'source-digest', media_revision: 'media', presentation_mode: 'text', title: '摘要', overview: '', blocks: [{ id: 'chapter', parent_id: null, order: 1, title: '章节一', body_markdown: '当前有效摘要段落', source_refs: [], figures: [] }] } }
function detail() { return <SummaryWorkspace task={task} readOnly={false} playbackUrl={null} playerRef={createRef<VideoPlayerHandle>()} onPlayhead={vi.fn()} onDuration={vi.fn()} onSeek={vi.fn()} refreshPlaybackUrl={vi.fn()} onChanged={vi.fn()} onGenerate={vi.fn()} onTechnical={vi.fn()} busy={false} indexed /> }
beforeEach(() => {
  vi.clearAllMocks(); clearSummaryViewState(); mocks.owner = 7; setSummaryViewOwner(7)
  window.history.replaceState({}, '', '/video/42?session=9')
  vi.stubGlobal('requestAnimationFrame', (run: FrameRequestCallback) => { run(0); return 1 }); vi.stubGlobal('cancelAnimationFrame', vi.fn())
  Element.prototype.scrollIntoView = vi.fn()
  vi.spyOn(Element.prototype, 'getBoundingClientRect').mockImplementation(function (this: Element) {
    const root = this.closest('.summary-reader, .video-chat-summary-reader, .chat-scroll') as HTMLElement | null
    const top = this.classList.contains('summary-chapter') ? 500 - (root?.scrollTop || 0) : this.classList.contains('msg-user') ? 200 - (root?.scrollTop || 0) : 0
    return { top, bottom: top + 40, left: 0, right: 0, height: 40, width: 0, x: 0, y: top, toJSON: () => ({}) } as DOMRect
  })
  mocks.listSessions.mockResolvedValue([{ id: 9, task_id: 42, scope_type: 'video', title: '会话九' }, { id: 10, task_id: 42, scope_type: 'video', title: '会话十' }])
  mocks.getMessages.mockResolvedValue([{ id: 100, role: 'user', content: '以前的问题' }, { id: 101, role: 'assistant', content: '以前的回答' }]); mocks.getRunHistory.mockResolvedValue([])
  mocks.getSummary.mockResolvedValue(summary); mocks.getTask.mockResolvedValue(task)
  mocks.playbackSrc.mockResolvedValue(null); mocks.questions.mockResolvedValue({ questions: [] })
  mocks.block.mockResolvedValue({ canonical_text: '当前有效摘要段落', document_digest: 'digest', block_digest: 'block-digest', figures: [] })
})
afterEach(() => { cleanup(); clearSummaryViewState(); vi.unstubAllGlobals(); vi.restoreAllMocks() })
test('detail → standalone QA → detail keeps draft, authorized refs, reader position and message anchor', async () => {
  let route = render(detail())
  fireEvent.click(screen.getByRole('button', { name: '引用本章前 700 字' }))
  await screen.findAllByText('以前的问题')
  await waitFor(() => expect(screen.getByRole('textbox')).toBeTruthy())
  fireEvent.change(screen.getByRole('textbox'), { target: { value: '未发送的问题草稿' } })
  const reader = route.container.querySelector('.summary-reader') as HTMLElement
  reader.scrollTop = 143; fireEvent.scroll(reader)
  const chatScroll = route.container.querySelector('.chat-scroll') as HTMLElement
  chatScroll.scrollTop = 80; fireEvent.scroll(chatScroll)
  expect(getSummarySessionView(7, 42, 9).contexts).toHaveLength(1)
  expect(screen.getByRole('link', { name: '独立问答' }).getAttribute('href')).toBe('/chat/v/42?session=9')
  route.unmount(); window.history.replaceState({}, '', '/chat/v/42')
  route = render(<VideoChatPage params={{ id: '42' }} />)
  await screen.findAllByText('以前的问题')
  expect(window.location.search).toBe('?session=9')
  expect(screen.getByRole('link', { name: '返回摘要详情' }).getAttribute('href')).toBe('/video/42?session=9')
  expect((screen.getByRole('textbox') as HTMLTextAreaElement).value).toBe('未发送的问题草稿')
  expect(screen.getByText('摘要选段')).toBeTruthy()
  expect((route.container.querySelector('.chat-scroll') as HTMLElement).scrollTop).toBe(80)
  fireEvent.click(screen.getByRole('button', { name: '展开当前有效摘要' }))
  await screen.findByRole('heading', { name: '章节一' })
  const qaReader = route.container.querySelector('.video-chat-summary-reader') as HTMLElement
  expect(qaReader.scrollTop).toBe(143)
  fireEvent.click(screen.getByRole('button', { name: '移除摘要选段 1' }))
  expect(getSummarySessionView(7, 42, 9).contexts).toEqual([])
  route.unmount(); window.history.replaceState({}, '', '/video/42')
  route = render(detail()); fireEvent.click(screen.getByRole('tab', { name: '问答' }))
  await screen.findAllByText('以前的问题')
  expect((screen.getByRole('textbox') as HTMLTextAreaElement).value).toBe('未发送的问题草稿')
  expect(mocks.streamAsk).not.toHaveBeenCalled()
})
test('ordinary session changes and account/task changes cannot splice another draft or references', async () => {
  const route = render(detail())
  fireEvent.click(screen.getByRole('button', { name: '引用本章前 700 字' })); await screen.findAllByText('以前的问题')
  fireEvent.change(screen.getByRole('textbox'), { target: { value: '会话九的草稿' } })
  act(() => patchSummarySessionView(7, 42, 10, { draft: '会话十的草稿' }))
  fireEvent.click(screen.getByRole('button', { name: '历史会话' }))
  fireEvent.click(screen.getByRole('button', { name: /会话十/ }))
  await waitFor(() => expect((screen.getByRole('textbox') as HTMLTextAreaElement).value).toBe('会话十的草稿'))
  expect(screen.queryByText('摘要选段')).toBeNull()
  route.unmount(); window.history.replaceState({}, '', '/chat/v/42?session=10')
  const qa = render(<VideoChatPage params={{ id: '42' }} />); await screen.findAllByText('以前的问题')
  expect((screen.getByRole('textbox') as HTMLTextAreaElement).value).toBe('会话十的草稿')
  expect(screen.queryByText('摘要选段')).toBeNull()
  mocks.owner = 8
  mocks.listSessions.mockResolvedValue([{ id: 19, task_id: 42, scope_type: 'video', title: '账号八的会话' }])
  qa.rerender(<VideoChatPage params={{ id: '42' }} />)
  await waitFor(() => expect((screen.getByRole('textbox') as HTMLTextAreaElement).value).toBe(''))
  expect(getSummarySessionView(7, 42, 9).draft).toBe('')
  expect(screen.queryAllByText('以前的问题')).toHaveLength(0)
  fireEvent.change(screen.getByRole('textbox'), { target: { value: '账号八在视频42的草稿' } })
  mocks.getTask.mockResolvedValue({ ...task, id: 43, filename: 'other.mp4' })
  mocks.listSessions.mockResolvedValue([{ id: 29, task_id: 43, scope_type: 'video' }])
  mocks.getMessages.mockResolvedValue([{ id: 301, role: 'user', content: '另一个视频的问题' }])
  window.history.replaceState({}, '', '/chat/v/43?session=29')
  qa.rerender(<VideoChatPage params={{ id: '43' }} />)
  await screen.findAllByText('另一个视频的问题')
  expect((screen.getByRole('textbox') as HTMLTextAreaElement).value).toBe('')
  expect(screen.queryByText('摘要选段')).toBeNull()
  expect(getSummarySessionView(8, 42, null).draft).toBe('账号八在视频42的草稿')
})
test('standalone single-text selection reaches the created session separately from the question and clears after saved done', async () => {
  window.history.replaceState({}, '', '/chat/v/42')
  mocks.createSession.mockResolvedValue({ id: 11, task_id: 42, scope_type: 'video' })
  mocks.streamAsk.mockImplementation(async (_sid, _question, _topK, _mode, handlers) => { handlers.onDone({ message_id: 202, answer: '已保存回答' }) })
  const route = render(<VideoChatPage params={{ id: '42' }} />)
  await screen.findByRole('button', { name: '展开当前有效摘要' })
  fireEvent.click(screen.getByRole('button', { name: '展开当前有效摘要' }))
  await screen.findByRole('heading', { name: '章节一' })
  const body = route.container.querySelector('.summary-body')!
  const text = body.querySelector('p')!.firstChild!
  const range = document.createRange(); range.setStart(text, 4); range.setEnd(text, 8)
  const selection = window.getSelection()!; selection.removeAllRanges(); selection.addRange(range)
  fireEvent.mouseUp(body)
  await screen.findByText('摘要选段')
  fireEvent.change(screen.getByRole('textbox'), { target: { value: '解释这段的前提' } })
  fireEvent.click(screen.getByRole('button', { name: '发送' }))
  await waitFor(() => expect(mocks.streamAsk).toHaveBeenCalledOnce())
  const [id, question, , , , , refs] = mocks.streamAsk.mock.calls[0]
  expect(id).toBe(11); expect(question).toBe('解释这段的前提')
  expect(refs).toEqual([expect.objectContaining({ task_id: 42, quote: '摘要段落', text_start: 4, text_end: 8, block_id: 'chapter', document_digest: 'digest' })])
  await waitFor(() => expect(getSummarySessionView(7, 42, 11).contexts).toEqual([]))
  expect(getSummarySessionView(7, 42, null).contexts).toEqual([])
})
