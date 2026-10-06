// @vitest-environment jsdom
import type { ReactNode } from 'react'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { afterEach, expect, test, vi } from 'vitest'
import { useVideoAIPreflight } from './VideoAIPreflight'

const api = vi.hoisted(() => ({ optionalCapabilities: vi.fn() }))
vi.mock('@/lib/api', () => ({ api, getToken: () => 'current-user' }))

afterEach(() => { cleanup(); vi.resetAllMocks() })

function Harness({ onRun }: { onRun: () => void }) {
  const preflight = useVideoAIPreflight('transcribe')
  return <><button onClick={() => preflight.request('转写视频', onRun)}>开始</button>{preflight.dialog}</>
}

function show(onRun = vi.fn()) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  const wrapper = ({ children }: { children: ReactNode }) => <QueryClientProvider client={client}>{children}</QueryClientProvider>
  return { ...render(<Harness onRun={onRun} />, { wrapper }), onRun, client }
}

test('transcription proceeds without Vision and only displays its required capability', async () => {
  api.optionalCapabilities.mockResolvedValue({ capabilities: [{ key: 'asr', model: 'speech', effective_enabled: true }], actions: { transcribe: { allowed: true, required_capabilities: ['asr'] } } })
  const view = show()
  fireEvent.click(screen.getByText('开始'))
  const proceed = await screen.findByRole('button', { name: '继续转写视频' })
  await waitFor(() => expect((proceed as HTMLButtonElement).disabled).toBe(false))
  expect(screen.queryByText('视觉理解')).toBeNull()
  fireEvent.click(proceed)
  expect(view.onRun).toHaveBeenCalledOnce()
  view.client.clear()
})

test('a missing required ASR blocks continuing even when LLM is configured', async () => {
  api.optionalCapabilities.mockResolvedValue({ capabilities: [{ key: 'llm', model: 'chat', effective_enabled: true }, { key: 'asr', effective_enabled: false }], actions: { transcribe: { allowed: false, reason_code: 'missing_configuration', required_capabilities: ['asr'] } } })
  const view = show()
  fireEvent.click(screen.getByText('开始'))
  await screen.findByText(/请补齐本次操作所需/)
  expect(screen.queryByRole('button', { name: '继续转写视频' })).toBeNull()
  expect(view.onRun).not.toHaveBeenCalled()
  view.client.clear()
})
