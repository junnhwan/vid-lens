// @vitest-environment jsdom
import type { ReactNode } from 'react'
import { cleanup, renderHook, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { afterEach, expect, test, vi } from 'vitest'
import { useAIAvailability } from './useAIAvailability'
import type { CapabilityActionKey } from '@/lib/types'

const api = vi.hoisted(() => ({ optionalCapabilities: vi.fn() }))
vi.mock('@/lib/api', () => ({ api, getToken: () => 'current-user' }))
afterEach(() => { cleanup(); vi.resetAllMocks() })

function show(action: CapabilityActionKey = 'chat') {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  const wrapper = ({ children }: { children: ReactNode }) => <QueryClientProvider client={client}>{children}</QueryClientProvider>
  return { hook: renderHook(() => useAIAvailability(false, action), { wrapper }), client }
}
test('chat consumes server admission without requiring Vision or ASR', async () => {
  api.optionalCapabilities.mockResolvedValue({ capabilities: [{ key: 'llm', effective_enabled: true }, { key: 'embedding', effective_enabled: true }], actions: { chat: { allowed: true, required_capabilities: ['llm', 'embedding'] } } })
  const { hook, client } = show()
  await waitFor(() => expect(hook.result.current.isSuccess).toBe(true))
  expect(hook.result.current.ready).toBe(true)
  expect(hook.result.current.capabilities.map(c => c.key)).toEqual(['llm', 'embedding'])
  client.clear()
})
test('missing required capability and paused hosted API have distinct reasons', async () => {
  api.optionalCapabilities.mockResolvedValue({ capabilities: [{ key: 'vision', effective_enabled: false }], actions: { caption: { allowed: false, required_capabilities: ['vision'], reason_code: 'missing_configuration' } } })
  const { hook, client } = show('caption')
  await waitFor(() => expect(hook.result.current.isSuccess).toBe(true))
  expect(hook.result.current.ready).toBe(false)
  expect(hook.result.current.missing).toEqual(['视觉理解'])
  api.optionalCapabilities.mockResolvedValue({ actions: { caption: { allowed: false, required_capabilities: ['vision'], reason_code: 'hosted_paused' } } })
  await hook.result.current.refetch()
  await waitFor(() => expect(hook.result.current.reason).toContain('Free API 暂停'))
  client.clear()
})
test('upload requires no AI state request and unknown server projections fail closed', async () => {
  const upload = show('upload')
  expect(upload.hook.result.current.ready).toBe(true)
  expect(api.optionalCapabilities).not.toHaveBeenCalled()
  upload.client.clear()
  api.optionalCapabilities.mockResolvedValue({})
  const summary = show('summary')
  await waitFor(() => expect(summary.hook.result.current.isSuccess).toBe(true))
  expect(summary.hook.result.current.ready).toBe(false)
  summary.client.clear()
})
