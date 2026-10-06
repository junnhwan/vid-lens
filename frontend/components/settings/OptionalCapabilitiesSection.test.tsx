// @vitest-environment jsdom
import { afterEach, expect, test, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { OptionalCapabilitiesSection } from './OptionalCapabilitiesSection'
import { api } from '@/lib/api'

vi.mock('@/lib/api', () => ({ api: { optionalCapabilities: vi.fn(), setRerankEnabled: vi.fn() }, ApiError: class extends Error {} }))
vi.mock('@/components/Toast', () => ({ useToast: () => ({ success: vi.fn(), error: vi.fn() }) }))
afterEach(() => { cleanup(); vi.clearAllMocks() })
const base = { rerank_enabled: false, rerank_available: true, rerank_mode: 'model' as const, rerank_model: 'configured-ranker', alignment_configured: true }

test('user can enable then disable rerank and see alignment as a per-video action', async () => {
  vi.mocked(api.optionalCapabilities).mockResolvedValue(base)
  vi.mocked(api.setRerankEnabled).mockImplementation(async enabled => ({ ...base, rerank_enabled: enabled }))
  render(<OptionalCapabilitiesSection readOnly={false} />)
  const toggle = await screen.findByRole('switch', { name: '检索重排' })
  expect(toggle.getAttribute('aria-checked')).toBe('false')
  expect(screen.getByText(/普通转写不会运行本地对齐模型/)).toBeTruthy()
  fireEvent.click(toggle)
  await waitFor(() => expect(toggle.getAttribute('aria-checked')).toBe('true'))
  fireEvent.click(toggle)
  await waitFor(() => expect(toggle.getAttribute('aria-checked')).toBe('false'))
  expect(vi.mocked(api.setRerankEnabled).mock.calls).toEqual([[true], [false]])
})

test('failed save keeps the actual saved preference', async () => {
  vi.mocked(api.optionalCapabilities).mockResolvedValue(base)
  vi.mocked(api.setRerankEnabled).mockRejectedValue(new Error('offline'))
  render(<OptionalCapabilitiesSection readOnly={false} />)
  const toggle = await screen.findByRole('switch', { name: '检索重排' })
  fireEvent.click(toggle)
  await waitFor(() => expect((toggle as HTMLButtonElement).disabled).toBe(false))
  expect(toggle.getAttribute('aria-checked')).toBe('false')
})

test('unconfigured capabilities and demo accounts cannot enable rerank', async () => {
  vi.mocked(api.optionalCapabilities).mockResolvedValue({ ...base, rerank_available: false, rerank_reason: 'ai_profile_required', alignment_configured: false })
  const { rerender } = render(<OptionalCapabilitiesSection readOnly={false} />)
  const toggle = await screen.findByRole('switch', { name: '检索重排' })
  expect((toggle as HTMLButtonElement).disabled).toBe(true)
  expect(screen.getByText(/尚未配置对齐器/)).toBeTruthy()
  rerender(<OptionalCapabilitiesSection readOnly />)
  expect(screen.getByText('演示账号仅可查看这些设置。')).toBeTruthy()
  expect(api.setRerankEnabled).not.toHaveBeenCalled()
})
