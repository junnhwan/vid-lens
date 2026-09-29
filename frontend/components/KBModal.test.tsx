// @vitest-environment jsdom
import { cleanup, render, screen, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { afterEach, expect, test, vi } from 'vitest'
import { api } from '@/lib/api'
import type { KnowledgeBase, VideoTask } from '@/lib/types'
import KBModal from './KBModal'

afterEach(() => { cleanup(); vi.restoreAllMocks() })
function show() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  render(<QueryClientProvider client={client}><KBModal mode="manage" kb={{ id: 1, name: '课程', videos: [{ task_id: 2 }] } as KnowledgeBase} onClose={vi.fn()} onChanged={vi.fn()} /></QueryClientProvider>)
}
test('old-model indexes cannot be added; unavailable existing members can be removed', async () => {
  vi.spyOn(api, 'listTasks').mockResolvedValue({ list: [{ id: 1, title: '旧模型索引', has_rag_index: true, retrievable: false }, { id: 2, title: '已有坏成员', retrievable: false }, { id: 3, title: '当前索引', retrievable: true }] as VideoTask[], total: 3, page: 1, page_size: 20 })
  show()
  await screen.findByText('旧模型索引')
  const checks = screen.getAllByRole('checkbox') as HTMLInputElement[]
  expect(checks.filter(item => item.disabled)).toHaveLength(1)
  expect(checks.find(item => item.checked)?.disabled).toBe(false)
})
test('list read errors stay distinct from an empty library', async () => {
  vi.spyOn(api, 'listTasks').mockRejectedValue(new Error('offline'))
  show()
  await waitFor(() => expect(screen.getByText('资料加载失败')).toBeTruthy())
  expect(screen.queryByText('没有匹配的视频。')).toBeNull()
  expect(screen.getByRole('button', { name: /重试/ })).toBeTruthy()
})
