// @vitest-environment jsdom
import type { ReactNode } from 'react'
import { cleanup, renderHook, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { afterEach, expect, test, vi } from 'vitest'
import { useAIAvailability } from './useAIAvailability'

const api = vi.hoisted(() => ({ listProfiles: vi.fn(), hostedAI: vi.fn() }))
vi.mock('@/lib/api', () => ({ api }))
afterEach(() => { cleanup(); vi.resetAllMocks() })

test('self-owned default AI stays available when hosted status cannot be read', async () => {
  api.listProfiles.mockResolvedValue([{ id: 8, source: 'user', is_default: true, name: 'My AI' }])
  api.hostedAI.mockRejectedValue(new Error('hosted status unavailable'))
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  const wrapper = ({ children }: { children: ReactNode }) => <QueryClientProvider client={client}>{children}</QueryClientProvider>
  const hook = renderHook(() => useAIAvailability(), { wrapper })
  await waitFor(() => expect(hook.result.current.isSuccess).toBe(true))
  expect(hook.result.current.ready).toBe(true)
  expect(api.hostedAI).not.toHaveBeenCalled()
  client.clear()
})
