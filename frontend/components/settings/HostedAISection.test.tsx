// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, expect, test, vi } from 'vitest'
import { api } from '@/lib/api'
import { HostedAISection } from './HostedAISection'
import type { AIProfile } from '@/lib/types'

vi.mock('@/lib/api', () => ({ api: { hostedAI: vi.fn(), activateHostedAI: vi.fn() } }))
vi.mock('@/components/Toast', () => ({ useToast: () => ({ success: vi.fn() }) }))
vi.mock('./HostedAIAdminForm', () => ({ HostedAIAdminForm: () => <div>作者配置表单</div> }))
afterEach(() => { cleanup(); vi.resetAllMocks() })

const profile: AIProfile = { id: 1, name: 'Free API', is_default: false, llm_model: 'chat-free', asr_model: 'asr-free', embedding_model: 'embed-free', embedding_dim: 1024, vision_model: 'vision-free', rerank_model: 'rank-free', llm_provider: '', llm_base_url: 'https://private.example/v1', llm_api_key_masked: 'private-secret', asr_provider: '', asr_base_url: '', asr_api_key_masked: '', embedding_provider: '', embedding_endpoint: '', embedding_api_key_masked: '', vision_provider: '', vision_base_url: '', vision_api_key_masked: '' }

test('ordinary users see only models and activate once without credential payload', async () => {
  vi.mocked(api.hostedAI).mockResolvedValue({ enabled: true, can_manage: false, notice: 'Free API 是站点提供的 AI API 接入，不代表本站自行部署了底层模型。', profile })
  vi.mocked(api.activateHostedAI).mockResolvedValue(profile)
  const refreshed = vi.fn().mockResolvedValue(undefined)
  const { container } = render(<HostedAISection readOnly={false} active={false} onActivated={refreshed} />)
  await screen.findByText('chat-free')
  expect(screen.getByRole('heading', { name: 'Free API' })).toBeTruthy()
  expect(screen.getByText(/不代表本站自行部署了底层模型/)).toBeTruthy()
  expect(document.body.textContent).not.toContain('免费 AI')
  expect(container.textContent).not.toContain('private.example')
  expect(container.textContent).not.toContain('private-secret')
  expect(screen.queryByRole('button', { name: '管理 Free API' })).toBeNull()
  fireEvent.click(screen.getByRole('button', { name: '一键启用 Free API' }))
  await waitFor(() => expect(refreshed).toHaveBeenCalledTimes(1))
  expect(api.activateHostedAI).toHaveBeenCalledWith()
})

test('author management uses server authorization and paused service cannot activate', async () => {
  vi.mocked(api.hostedAI).mockResolvedValue({ enabled: false, can_manage: true, notice: '', profile })
  render(<HostedAISection readOnly={false} active={false} onActivated={vi.fn()} />)
  const manage = await screen.findByRole('button', { name: '管理 Free API' })
  expect(screen.getByRole('button', { name: '一键启用 Free API' }).hasAttribute('disabled')).toBe(true)
  fireEvent.click(manage)
  expect(screen.getByText('作者配置表单')).toBeTruthy()
  expect(api.activateHostedAI).not.toHaveBeenCalled()
})

test('demo users cannot activate even if the service is enabled', async () => {
  vi.mocked(api.hostedAI).mockResolvedValue({ enabled: true, can_manage: false, notice: '', profile })
  render(<HostedAISection readOnly active={false} onActivated={vi.fn()} />)
  await screen.findByText('chat-free')
  expect(screen.getByRole('button', { name: '一键启用 Free API' }).hasAttribute('disabled')).toBe(true)
})
