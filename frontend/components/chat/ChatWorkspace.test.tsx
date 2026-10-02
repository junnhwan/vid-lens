// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, expect, test, vi } from 'vitest'
import { studyFixture } from '@/dev/productFixtures'
import { ApiError } from '@/lib/api'
import type { ChatMsg } from './chatUtils'
import { artifactApi } from '@/lib/artifacts/api'
import { ChatWorkspace } from './ChatWorkspace'
import { citeFromAPI, citesFromSnapshot, hasReplayRange } from '@/components/Citation'
import type { Citation } from '@/lib/types'

vi.mock('@/lib/router', () => ({ default: ({ href, children, onClick }: { href: string; children: React.ReactNode; onClick?: () => void }) => <a href={href} onClick={onClick}>{children}</a>, useRouter: () => ({ push: vi.fn() }) }))
vi.mock('@/components/Toast', () => ({ useToast: () => ({ success: vi.fn(), error: vi.fn(), info: vi.fn() }) }))
vi.mock('@/components/shell/AppShell', () => ({ useShell: () => ({ user: {role:'USER'} }) }))
vi.mock('@/components/settings/useAIAvailability', () => ({ useAIAvailability: () => ({ready:true,reason:''}) }))
vi.mock('@/components/settings/VideoAIPreflight', () => ({ useVideoAIPreflight: () => ({ request: (_label: string, run: () => void) => run(), dialog: null }) }))
const conversation = vi.hoisted(() => ({
  session: undefined, sessions: [], messages: [{ messageId: 108, role: 'assistant', content: '已持久化回答 [C1]' }] as ChatMsg[],
  ragTrace: [], agentTrace: { runId: null, steps: [] }, streaming: false, sessionReady: true,
  send: vi.fn(), stop: vi.fn(), newSession: vi.fn(), switchSession: vi.fn(), loadSessions: vi.fn(),
}))
vi.mock('@/components/chat/useConversationSession', () => ({ useConversationSession: () => conversation }))
vi.mock('@/components/knowledge/RunDetails', () => ({ RunDetails: () => null, SessionMemoryControl: () => null }))
afterEach(() => { cleanup(); vi.restoreAllMocks(); conversation.messages = [{ messageId: 108, role: 'assistant', content: '已持久化回答 [C1]' }]; conversation.send.mockClear() })

test('initial recommendations appear once in the conversation and both context panes can collapse', () => {
  conversation.messages = []
  render(<ChatWorkspace scopeType="video" targetId={42} scopeName="教程" playbackUrl={null} suggestions={[]} videoQuestions={{ status: 'ready', message: '从这些问题开始', questions: [{ question: '为什么要使用多阶段构建？', source: '内容', excerpt: '' }] }} />)
  expect(screen.getAllByRole('button', { name: /为什么要使用多阶段构建/ })).toHaveLength(1)
  const nav = screen.getByRole('button', { name: '问题目录' })
  fireEvent.click(nav)
  expect(nav.getAttribute('aria-expanded')).toBe('true')
  fireEvent.click(nav)
  expect(nav.getAttribute('aria-expanded')).toBe('false')
  const rail = screen.getByRole('button', { name: '视频与执行过程' })
  fireEvent.click(rail)
  expect(rail.getAttribute('aria-expanded')).toBe('true')
  fireEvent.click(rail)
  expect(rail.getAttribute('aria-expanded')).toBe('false')
  fireEvent.click(screen.getByRole('button', { name: /为什么要使用多阶段构建/ }))
  expect(conversation.send).toHaveBeenCalledWith('为什么要使用多阶段构建？')
})

test('single-video citation cards omit the repeated video title', () => {
  const message: ChatMsg = { messageId: 108, role: 'assistant', content: '已保存的回答', cites: [{ id: 'C1', chunkIndex: 0, score: 1, content: '图中展示了容器的端口映射。', videoTitle: '不应重复出现的标题', modality: 'visual_caption', startMS: 120000, endMS: 120000, timeRangeStatus: 'exact' }] }
  conversation.messages = [message]
  render(<ChatWorkspace scopeType="video" targetId={42} scopeName="教程" playbackUrl={null} suggestions={[]} />)
  expect(screen.queryByText('不应重复出现的标题')).toBeNull()
  expect(screen.getByText('图中展示了容器的端口映射。')).toBeTruthy()
})

test('citation detail is an independent button and technical fields start folded', () => {
  conversation.messages = [{ role:'assistant', content:'回答', modelName:'测试模型', cites:[{id:'C1',chunkIndex:0,score:1,content:'来源正文',evidenceId:'internal-evidence'}] }]
  render(<ChatWorkspace scopeType="video" targetId={42} scopeName="教程" playbackUrl={null} suggestions={[]} />)
  fireEvent.click(screen.getByRole('button', {name:'查看证据 C1'}))
  expect(screen.getByRole('dialog', {name:'证据详情'})).toBeTruthy()
  expect(screen.getByText('internal-evidence').closest('details')?.open).toBe(false)
  expect(screen.getByText(/测试模型/).closest('details')?.open).toBe(false)
})

test('sentence evidence highlights its source words and switches between distant citations', () => {
  conversation.messages = [{ role: 'assistant', content: '两处依据 [C1] [C2]', cites: [
    { id: 'C1', taskId: 42, chunkIndex: 0, score: 1, content: '第一处来源。', anchorQuote: '不再保留应用缓存。', displayContext: '读取频繁的热数据由系统缓存。不再保留应用缓存。后面解释收益。', modality: 'transcript', startMS: 6000, endMS: 9500, timeRangeStatus: 'exact' },
    { id: 'C2', taskId: 42, chunkIndex: 1, score: 1, content: '第二处来源。', anchorQuote: '进程可以随意重启。', displayContext: '后面的另一处说明：进程可以随意重启。', modality: 'transcript', startMS: 221000, endMS: 224000, timeRangeStatus: 'exact' },
  ] }]
  const { container } = render(<ChatWorkspace scopeType="video" targetId={42} scopeName="教程" playbackUrl={null} suggestions={[]} />)
  fireEvent.click(screen.getByRole('button', { name: 'C1' }))
  expect(container.querySelector('.evidence-source-context mark')?.textContent).toBe('不再保留应用缓存。')
  const drawer = within(screen.getByRole('dialog', { name: '证据详情' }))
  fireEvent.click(drawer.getByRole('button', { name: '切换到引用 C2' }))
  expect(container.querySelector('.evidence-source-context mark')?.textContent).toBe('进程可以随意重启。')
  expect(drawer.getByText('03:41 – 03:44')).toBeTruthy()
})

test('old coarse citation snapshots retain observed time and offer an explicit upgrade link', () => {
  const citation: Citation = { task_id: 42, citation_id: 'C7', evidence_id: 'saved', chunk_id: 7, chunk_index: 7, score: 1, content: '历史原文', anchor_quote: '应用层缓存没有收益。', display_context: '开头。应用层缓存没有收益。结尾。', start_ms: 0, end_ms: 305000, time_range_status: 'coarse', modality: 'transcript' }
  const [saved] = citesFromSnapshot(JSON.stringify({ citations: [citation] }))
  expect(saved).toMatchObject(citeFromAPI(citation, 0))
  conversation.messages = [{ role: 'assistant', content: '历史回答 [C7]', cites: [saved] }]
  render(<ChatWorkspace scopeType="video" targetId={42} scopeName="教程" playbackUrl={null} suggestions={[]} />)
  expect(screen.getByText('原片段 00:00 – 05:05')).toBeTruthy()
  fireEvent.click(screen.getByRole('button', { name: 'C7' }))
  const drawer = within(screen.getByRole('dialog', { name: '证据详情' }))
  expect(drawer.getByRole('link', { name: '补齐引用定位' }).getAttribute('href')).toBe('/video/42?citations=upgrade#transcript')
  expect(drawer.getByText(/历史回答保留原有引用/)).toBeTruthy()
})

test('citation replay seeks the observed timestamp including zero and exact visual points', () => {
  conversation.messages = [{ role: 'assistant', content: '回答', cites: [
    { id: 'C1', chunkIndex: 0, score: 1, content: '开头句子', modality: 'transcript', startMS: 0, endMS: 2500, timeRangeStatus: 'exact' },
    { id: 'C2', chunkIndex: 1, score: 1, content: '后面的画面', modality: 'visual_caption', startMS: 221000, endMS: 221000, timeRangeStatus: 'exact' },
  ] }]
  const play = vi.spyOn(HTMLMediaElement.prototype, 'play').mockResolvedValue()
  const { container } = render(<ChatWorkspace scopeType="video" targetId={42} scopeName="教程" playbackUrl="/video" suggestions={[]} />)
  const video = container.querySelector('video')!
  video.currentTime = 30
  fireEvent.click(screen.getAllByRole('button', { name: '回放' })[0])
  expect(video.currentTime).toBe(0)
  fireEvent.click(screen.getAllByRole('button', { name: '回放' })[1])
  expect(video.currentTime).toBe(221)
  expect(play).toHaveBeenCalled()
  expect(hasReplayRange({ startMS: 0, endMS: 5000, timeRangeStatus: 'unknown' })).toBe(false)
  expect(hasReplayRange({ startMS: -1, endMS: 5000, timeRangeStatus: 'exact' })).toBe(false)
})

test('analysis explains only enabled capabilities', () => {
  render(<ChatWorkspace scopeType="video" targetId={42} scopeName="教程" playbackUrl={null} suggestions={[]} videoVisualMode="off" videoRetrievable={false} videoHasTranscript />)
  fireEvent.click(screen.getByRole('button', {name:'深入分析'}))
  expect(screen.getByText('按问题检索文本证据，逐步分析后回答')).toBeTruthy()
  expect(screen.queryByText('按问题调用文本与画面工具，逐步分析后回答')).toBeNull()
  expect(screen.getByText(/暂不提供检索引用/)).toBeTruthy()
})

test('preview conflict can reload a new head and choose a surviving block before confirming', async () => {
  const next = structuredClone(studyFixture)
  next.head_version = 2
  next.version!.id = 'new-version'
  next.version!.body.blocks = next.version!.body.blocks.slice(3)
  vi.spyOn(artifactApi, 'list').mockResolvedValue({ list: [studyFixture], total: 1, page: 1, page_size: 20 })
  vi.spyOn(artifactApi, 'get').mockResolvedValueOnce(studyFixture).mockResolvedValueOnce(next)
  const preview = vi.spyOn(artifactApi, 'answerPreview').mockRejectedValueOnce(new ApiError(409, '版本发生变化', 'version_conflict')).mockResolvedValueOnce({
    message_id: 108, content: '已持久化回答 [C1]', after_block_id: 'config', version_id: 'new-version', mapped: [], unmapped: [],
  })
  render(<ChatWorkspace scopeType="video" targetId={42} scopeName="教程" playbackUrl={null} suggestions={[]} />)
  fireEvent.click(screen.getByRole('button', { name: '收进笔记' }))
  fireEvent.click(await screen.findByRole('button', { name: '预览正文与引用' }))
  fireEvent.click(await screen.findByRole('button', { name: '读取新版本' }))
  await waitFor(() => expect((screen.getByLabelText('插在段落之后') as HTMLSelectElement).value).toBe('config'))
  expect(screen.queryByRole('button', { name: '确认生成新版本' })).toBeNull()
  fireEvent.click(screen.getByRole('button', { name: '预览正文与引用' }))
  await waitFor(() => expect(preview).toHaveBeenLastCalledWith(studyFixture.id, 108, 'config', 2))
  expect(await screen.findByRole('button', { name: '确认生成新版本' })).toBeTruthy()
})
