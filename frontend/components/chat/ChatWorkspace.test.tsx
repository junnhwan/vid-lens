// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, expect, test, vi } from 'vitest'
import { studyFixture } from '@/dev/productFixtures'
import { ApiError } from '@/lib/api'
import type { ChatMsg } from './chatUtils'
import { artifactApi } from '@/lib/artifacts/api'
import { ChatWorkspace } from './ChatWorkspace'

vi.mock('@/lib/router', () => ({ useRouter: () => ({ push: vi.fn() }) }))
vi.mock('@/components/Toast', () => ({ useToast: () => ({ success: vi.fn(), error: vi.fn(), info: vi.fn() }) }))
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
