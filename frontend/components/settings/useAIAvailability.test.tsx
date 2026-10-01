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
  api.listProfiles.mockResolvedValue([{
    id: 8, source: 'user', is_default: true, name: 'My AI',
    llm_model: 'chat-model', llm_base_url: 'https://chat.example/v1',
    asr_model: 'asr-model', asr_base_url: 'https://asr.example/v1',
    embedding_model: 'embed-model', embedding_endpoint: 'https://embed.example/v1/embeddings', embedding_dim: 1024,
    vision_model: 'vision-model', vision_base_url: 'https://vision.example/v1',
  }])
  api.hostedAI.mockRejectedValue(new Error('hosted status unavailable'))
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  const wrapper = ({ children }: { children: ReactNode }) => <QueryClientProvider client={client}>{children}</QueryClientProvider>
  const hook = renderHook(() => useAIAvailability(), { wrapper })
  await waitFor(() => expect(hook.result.current.isSuccess).toBe(true))
  expect(hook.result.current.ready).toBe(true)
  expect(api.hostedAI).not.toHaveBeenCalled()
  expect(hook.result.current.capabilities.every(capability => capability.ready)).toBe(true)
  client.clear()
})

test('video work is unavailable until the default profile has all four required capabilities', async () => {
  api.listProfiles.mockResolvedValue([{
    id: 9, source: 'user', is_default: true, name: 'Partial AI',
    llm_model: 'chat-model', llm_base_url: 'https://chat.example/v1',
    asr_model: 'asr-model', asr_base_url: 'https://asr.example/v1',
    embedding_model: 'embed-model', embedding_endpoint: 'https://embed.example/v1/embeddings', embedding_dim: 1024,
    vision_model: '', vision_base_url: '',
  }])
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  const wrapper = ({ children }: { children: ReactNode }) => <QueryClientProvider client={client}>{children}</QueryClientProvider>
  const hook = renderHook(() => useAIAvailability(), { wrapper })
  await waitFor(() => expect(hook.result.current.isSuccess).toBe(true))
  expect(hook.result.current.ready).toBe(false)
  expect(hook.result.current.missing).toEqual(['视觉理解'])
  expect(hook.result.current.reason).toContain('视觉理解')
  client.clear()
})

test('enabled Free API is ready even though the server hides its endpoints', async () => {
  api.listProfiles.mockResolvedValue([{
    id: 1, source: 'hosted', read_only: true, is_default: true, name: 'Free API',
    llm_model: 'qwen3.6-flash', asr_model: 'XingChenASR', embedding_model: 'BAAI/bge-m3',
    embedding_dim: 1024, vision_model: 'qwen3.6-flash',
  }])
  api.hostedAI.mockResolvedValue({ enabled: true, can_manage: false, notice: '' })
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  const wrapper = ({ children }: { children: ReactNode }) => <QueryClientProvider client={client}>{children}</QueryClientProvider>
  const hook = renderHook(() => useAIAvailability(), { wrapper })
  await waitFor(() => expect(hook.result.current.isSuccess).toBe(true))
  expect(hook.result.current.ready).toBe(true)
  expect(hook.result.current.missing).toEqual([])
  expect(hook.result.current.capabilities.every(capability => capability.ready)).toBe(true)
  client.clear()
})
