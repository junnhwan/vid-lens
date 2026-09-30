// @vitest-environment jsdom
import type { ReactNode } from 'react'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { afterEach, expect, test, vi } from 'vitest'
import { useVideoAIPreflight } from './VideoAIPreflight'

const api = vi.hoisted(() => ({ listProfiles: vi.fn(), hostedAI: vi.fn() }))
vi.mock('@/lib/api', () => ({ api }))

afterEach(() => { cleanup(); vi.resetAllMocks() })

function Harness({ onRun }: { onRun: () => void }) {
  const preflight = useVideoAIPreflight()
  return <><button onClick={() => preflight.request('转写视频', onRun)}>开始</button>{preflight.dialog}</>
}

function show(onRun = vi.fn()) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  const wrapper = ({ children }: { children: ReactNode }) => <QueryClientProvider client={client}>{children}</QueryClientProvider>
  return { ...render(<Harness onRun={onRun} />, { wrapper }), onRun, client }
}

test('missing visual model blocks the video action and offers the AI settings in a new tab', async () => {
  api.listProfiles.mockResolvedValue([{
    id: 9, source: 'user', is_default: true, name: 'Partial AI',
    llm_model: 'chat-model', llm_base_url: 'https://chat.example/v1',
    asr_model: 'asr-model', asr_base_url: 'https://asr.example/v1',
    embedding_model: 'embed-model', embedding_endpoint: 'https://embed.example/v1/embeddings', embedding_dim: 1024,
    vision_model: '', vision_base_url: '',
  }])
  const view = show()
  fireEvent.click(screen.getByText('开始'))
  expect(await screen.findByText('先确认 AI 能力，再开始处理')).toBeTruthy()
  expect(await screen.findByText('chat-model')).toBeTruthy()
  expect(screen.getByText('视觉理解')).toBeTruthy()
  expect(screen.getByText('待配置')).toBeTruthy()
  expect((screen.getByRole('link', { name: '配置 AI' }) as HTMLAnchorElement).target).toBe('_blank')
  expect(view.onRun).not.toHaveBeenCalled()
  view.client.clear()
})

test('a complete default profile enables the explicit continue action', async () => {
  api.listProfiles.mockResolvedValue([{
    id: 8, source: 'user', is_default: true, name: 'Ready AI',
    llm_model: 'chat-model', llm_base_url: 'https://chat.example/v1',
    asr_model: 'asr-model', asr_base_url: 'https://asr.example/v1',
    embedding_model: 'embed-model', embedding_endpoint: 'https://embed.example/v1/embeddings', embedding_dim: 1024,
    vision_model: 'vision-model', vision_base_url: 'https://vision.example/v1',
  }])
  const view = show()
  fireEvent.click(screen.getByText('开始'))
  const proceed = await screen.findByRole('button', { name: '继续转写视频' })
  await waitFor(() => expect((proceed as HTMLButtonElement).disabled).toBe(false))
  fireEvent.click(proceed)
  expect(view.onRun).toHaveBeenCalledOnce()
  view.client.clear()
})
