// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, expect, test, vi } from 'vitest'
import type { ReactNode } from 'react'
import { api } from '@/lib/api'
import ChatEntryPage from './page'

vi.mock('@/lib/router', () => ({ default: ({ href, children, className }: { href: string; children: ReactNode; className?: string }) => <a href={href} className={className}>{children}</a> }))
vi.mock('@/components/shell/AppShell', () => ({ useCrumb: () => {} }))
afterEach(() => { cleanup(); vi.restoreAllMocks() })

test('knowledge libraries stay in a searchable paginated picker', async () => {
  vi.spyOn(api, 'listKBs').mockResolvedValue(Array.from({ length: 7 }, (_, i) => ({ id: i + 1, name: `资料库 ${i + 1}`, description: '', member_count: 2, created_at: '2026-09-29T00:00:00Z', updated_at: '2026-09-29T00:00:00Z' })))
  vi.spyOn(api, 'listSessions').mockResolvedValue([])
  vi.spyOn(api, 'listTasks').mockResolvedValue({ list: [], total: 0, page: 1, page_size: 30 })
  render(<ChatEntryPage />)
  const select = await screen.findByRole('button', { name: /选择知识库 · 7 个/ })
  expect(screen.queryByRole('link', { name: /资料库 1/ })).toBeNull()
  fireEvent.click(select)
  expect(screen.getByRole('dialog')).toBeTruthy()
  expect(screen.getAllByRole('link', { name: /资料库/ })).toHaveLength(6)
  fireEvent.click(screen.getByRole('button', { name: '下一页' }))
  expect(screen.getByRole('link', { name: /资料库 7/ }).getAttribute('href')).toBe('/chat/kb/7')
  fireEvent.change(screen.getByRole('textbox', { name: '搜索问答知识库' }), { target: { value: '资料库 2' } })
  await waitFor(() => expect(screen.getAllByRole('link', { name: /资料库/ })).toHaveLength(1))
  expect(screen.getByRole('link', { name: /资料库 2/ }).getAttribute('href')).toBe('/chat/kb/2')
})
