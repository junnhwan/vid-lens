// @vitest-environment jsdom
import type { ReactNode } from 'react'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { afterEach, expect, test, vi } from 'vitest'
import { AnswerFeedback } from './AnswerFeedback'

const f = vi.hoisted(() => ({ get: vi.fn(), save: vi.fn(), clear: vi.fn() }))
vi.mock('@/lib/api', () => ({ getToken: () => 'user-a', api: { getAnswerFeedback: f.get, saveAnswerFeedback: f.save, clearAnswerFeedback: f.clear } }))
afterEach(() => { cleanup(); vi.resetAllMocks() })
function show() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  const wrapper = ({ children }: { children: ReactNode }) => <QueryClientProvider client={client}>{children}</QueryClientProvider>
  return { ...render(<AnswerFeedback sessionId={1} messageId={2} />, { wrapper }), client }
}
const open = () => fireEvent.click(screen.getByText('评价回答'))

test('lazy feedback loads, updates, clears and reloads persisted assessments', async () => {
  f.get.mockResolvedValue(null)
  f.save.mockImplementation(async (_s, _m, input) => ({ ...input, message_id: 2 }))
  f.clear.mockResolvedValue({})
  const view = show()
  expect(f.get).not.toHaveBeenCalled()
  open()
  const helpful = await screen.findByRole('button', { name: '有帮助' })
  await waitFor(() => expect((helpful as HTMLButtonElement).disabled).toBe(false))
  fireEvent.click(screen.getByRole('button', { name: '有问题' }))
  fireEvent.change(screen.getByLabelText('问题类型'), { target: { value: 'citation' } })
  fireEvent.change(screen.getByLabelText('反馈补充说明'), { target: { value: '请核对视频来源' } })
  fireEvent.click(screen.getByRole('button', { name: '保存反馈' }))
  await screen.findByRole('button', { name: '修改问题反馈' })
  expect(f.save).toHaveBeenCalledWith(1, 2, { rating: 'problem', category: 'citation', note: '请核对视频来源' })
  fireEvent.click(helpful)
  await screen.findByRole('button', { name: '已标记有帮助' })
  fireEvent.click(screen.getByRole('button', { name: '清除反馈' }))
  await waitFor(() => expect(screen.queryByRole('button', { name: '清除反馈' })).toBeNull())
  expect(f.clear).toHaveBeenCalledWith(1, 2)
  view.client.clear()
})

test('failed reads retry and changing message never reuses previous edit or error state', async () => {
  f.get.mockRejectedValueOnce(new Error('network')).mockResolvedValueOnce({ rating: 'problem', category: 'slow', note: 'prior note' }).mockResolvedValueOnce(null)
  const view = show(); open()
  fireEvent.click(await screen.findByRole('button', { name: '重试读取' }))
  fireEvent.click(await screen.findByRole('button', { name: '修改问题反馈' }))
  expect((screen.getByLabelText('反馈补充说明') as HTMLTextAreaElement).value).toBe('prior note')
  view.rerender(<AnswerFeedback sessionId={1} messageId={3} />)
  expect(screen.queryByText('prior note')).toBeNull()
  expect(screen.queryByLabelText('反馈补充说明')).toBeNull()
  open()
  await waitFor(() => expect(f.get).toHaveBeenCalledWith(1, 3, expect.anything()))
  await waitFor(() => expect((screen.getByRole('button', { name: '有帮助' }) as HTMLButtonElement).disabled).toBe(false))
  view.client.clear()
})

test('failed save keeps draft and retries the same assessment', async () => {
  f.get.mockResolvedValue(null)
  f.save.mockRejectedValueOnce(new Error('temporary')).mockResolvedValueOnce({ rating: 'problem', category: 'content', note: 'draft' })
  const view = show(); open()
  await waitFor(() => expect((screen.getByRole('button', { name: '有问题' }) as HTMLButtonElement).disabled).toBe(false))
  fireEvent.click(screen.getByRole('button', { name: '有问题' }))
  fireEvent.change(screen.getByLabelText('反馈补充说明'), { target: { value: 'draft' } })
  fireEvent.click(screen.getByRole('button', { name: '保存反馈' }))
  await screen.findByRole('alert')
  expect((screen.getByLabelText('反馈补充说明') as HTMLTextAreaElement).value).toBe('draft')
  fireEvent.click(screen.getByRole('button', { name: '保存反馈' }))
  await screen.findByRole('button', { name: '修改问题反馈' })
  expect(f.save).toHaveBeenCalledTimes(2)
  view.client.clear()
})
