// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, expect, test, vi } from 'vitest'
import { api, ApiError } from '@/lib/api'
import { CapabilityProbe, type ProbeTarget } from './CapabilityProbe'

vi.mock('@/lib/api', () => ({
  api: { probeCapability: vi.fn() },
  ApiError: class extends Error {
    status: number
    constructor(status: number, message: string) { super(message); this.status = status }
  },
}))

const targets: ProbeTarget[] = [
  { purpose: 'llm', label: '对话', model: 'chat-model', base_url: 'https://example.test/v1', api_key: '', provider: 'mock', profile_id: 1 },
  { purpose: 'asr', label: '语音识别', model: 'asr-model', base_url: 'https://example.test/v1', api_key: '', provider: 'mock', profile_id: 1 },
  { purpose: 'embedding', label: '向量', model: 'embed-model', base_url: 'https://example.test/v1/embeddings', api_key: '', provider: 'mock', profile_id: 1, embedding_dim: 1024 },
  { purpose: 'vision', label: '视觉', model: 'vision-model', base_url: 'https://example.test/v1', api_key: '', provider: 'mock', profile_id: 1 },
]

afterEach(() => { cleanup(); vi.resetAllMocks() })

test('keeps sequential probing visible and retains a partial failure', async () => {
  let finishFirst!: (value: { dimension: number }) => void
  const probe = vi.mocked(api.probeCapability)
  probe.mockImplementationOnce(() => new Promise(resolve => { finishFirst = resolve }))
    .mockResolvedValueOnce({ dimension: 0 })
    .mockRejectedValueOnce(new ApiError(400, '向量维度不匹配：实际 768，配置 1024'))
    .mockResolvedValueOnce({ dimension: 0 })

  render(<CapabilityProbe targets={targets} />)
  fireEvent.click(screen.getByRole('button', { name: '开始能力检查' }))

  expect(probe).toHaveBeenCalledTimes(1)
  expect(screen.getByText('正在发送小样本请求…')).toBeTruthy()
  expect(screen.getAllByText('等待前一项完成')).toHaveLength(3)
  expect(screen.getByRole('button', { name: '逐项探测中…' }).hasAttribute('disabled')).toBe(true)

  await act(async () => { finishFirst({ dimension: 0 }) })
  await waitFor(() => expect(probe).toHaveBeenCalledTimes(4))
  await waitFor(() => expect(screen.getByText('3 项可用 · 1 项待处理')).toBeTruthy())
  expect(probe.mock.calls.map(([target]) => target.purpose)).toEqual(['llm', 'asr', 'embedding', 'vision'])
  expect(screen.getByText('向量维度不匹配：实际 768，配置 1024')).toBeTruthy()
  expect(screen.getByRole('button', { name: '重新探测全部' }).hasAttribute('disabled')).toBe(false)
})

test('invalidates results after configuration changes without starting a second charged run', async () => {
  let finishFirst!: (value: { dimension: number }) => void
  const probe = vi.mocked(api.probeCapability)
  probe.mockImplementationOnce(() => new Promise(resolve => { finishFirst = resolve }))
    .mockResolvedValue({ dimension: 0 })

  const { rerender } = render(<CapabilityProbe targets={[targets[0]]} />)
  fireEvent.click(screen.getByRole('button', { name: '开始能力检查' }))
  rerender(<CapabilityProbe targets={[{ ...targets[0], model: 'new-chat-model' }]} />)

  expect(screen.getByText('配置已变更，正在等待先前的请求结束…')).toBeTruthy()
  expect(screen.getByRole('button', { name: '逐项探测中…' }).hasAttribute('disabled')).toBe(true)
  expect(probe).toHaveBeenCalledTimes(1)

  await act(async () => { finishFirst({ dimension: 0 }) })
  await waitFor(() => expect(screen.getByRole('button', { name: '开始能力检查' }).hasAttribute('disabled')).toBe(false))
  expect(screen.getByText('尚未发送样本请求')).toBeTruthy()
  fireEvent.click(screen.getByRole('button', { name: '开始能力检查' }))
  await waitFor(() => expect(probe).toHaveBeenCalledTimes(2))
  expect(probe.mock.calls[1][0].model).toBe('new-chat-model')
})
