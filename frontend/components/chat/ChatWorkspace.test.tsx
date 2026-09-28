// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, expect, test, vi } from 'vitest'
import { studyFixture } from '@/dev/productFixtures'
import { ApiError } from '@/lib/api'
import { artifactApi } from '@/lib/artifacts/api'
import { ChatWorkspace } from './ChatWorkspace'

vi.mock('@/lib/router', () => ({ useRouter: () => ({ push: vi.fn() }) }))
vi.mock('@/components/Toast', () => ({ useToast: () => ({ success: vi.fn(), error: vi.fn(), info: vi.fn() }) }))
vi.mock('@/components/chat/useConversationSession', () => ({ useConversationSession: () => ({
  session: undefined, sessions: [], messages: [{ messageId: 108, role: 'assistant', content: '已持久化回答 [C1]' }],
  ragTrace: [], agentTrace: { runId: null, steps: [] }, streaming: false, sessionReady: true,
  send: vi.fn(), stop: vi.fn(), newSession: vi.fn(), switchSession: vi.fn(), loadSessions: vi.fn(),
}) }))
afterEach(() => { cleanup(); vi.restoreAllMocks() })

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
