// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, expect, test, vi } from 'vitest'
import { api } from '@/lib/api'
import { ProfileForm } from './ProfileForm'

vi.mock('@/components/shell/AppShell', () => ({useShell:() => ({registerLeaveGuard:vi.fn()})}))
vi.mock('@/components/Toast', () => ({useToast:() => ({success:vi.fn(),error:vi.fn()})}))
vi.mock('./CapabilityProbe', () => ({CapabilityProbe:() => null}))
afterEach(() => {cleanup();vi.restoreAllMocks()})

test('reused connections follow dialogue edits while capability models remain independent', async () => {
  vi.spyOn(api,'budgetOptions').mockRejectedValue(new Error('unused'))
  const save=vi.spyOn(api,'createProfile').mockResolvedValue({} as never)
  render(<ProfileForm imported={{name:'共用连接',llm_provider:'openai',llm_base_url:'https://example.com/v1',llm_model:'dialogue',asr_provider:'openai',asr_base_url:'https://asr.example.com/v1',asr_model:'speech',embedding_provider:'openai',embedding_endpoint:'https://embed.example.com/v1/embeddings',embedding_model:'vectors',embedding_dim:1024,is_default:false}} onClose={vi.fn()} onSaved={vi.fn()} />)
  const dialogue=within(screen.getByRole('region',{name:'对话模型'}))
  const speech=within(screen.getByRole('region',{name:'语音识别'}))
  const vectors=within(screen.getByRole('region',{name:'向量模型'}))
  expect((speech.getByRole('button',{name:'复用对话连接'}) as HTMLButtonElement).disabled).toBe(true)
  fireEvent.change(dialogue.getByLabelText('API Key'),{target:{value:'specimen-key'}})
  fireEvent.click(speech.getByRole('button',{name:'复用对话连接'}))
  fireEvent.click(vectors.getByRole('button',{name:'复用对话连接'}))
  fireEvent.change(dialogue.getByLabelText('服务 Base URL'),{target:{value:'https://next.example.com/v1/'}})
  fireEvent.click(screen.getByRole('button',{name:'创建配置'}))
  await waitFor(() => expect(save).toHaveBeenCalledWith(expect.objectContaining({asr_base_url:'https://next.example.com/v1',asr_api_key:'specimen-key',asr_model:'speech',embedding_endpoint:'https://next.example.com/v1/embeddings',embedding_api_key:'specimen-key',embedding_model:'vectors'})))
})
