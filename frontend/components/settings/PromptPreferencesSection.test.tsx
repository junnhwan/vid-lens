// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, expect, test, vi } from 'vitest'
import { api } from '@/lib/api'
import type { PromptPreferenceView } from '@/lib/types'
import { PromptPreferencesSection } from './PromptPreferencesSection'

vi.mock('@/lib/api', () => ({
  api: { promptPreferences: vi.fn(), setPromptPreference: vi.fn() },
  ApiError: class extends Error {},
}))
vi.mock('@/components/shell/AppShell', () => ({ useShell: () => ({ registerLeaveGuard: vi.fn() }) }))

const editable: PromptPreferenceView = {
  function: 'chat', label: '普通问答', scope: '用于新问答', product_instruction: '基于证据回答。',
  user_instruction: '', effective_preview: '基于证据回答。', editable: true,
}
const fixed: PromptPreferenceView = {
  function: 'title', label: '自动标题', scope: '产品固定规则', product_instruction: '生成简洁标题。',
  user_instruction: '', effective_preview: '生成简洁标题。', editable: false,
}

afterEach(() => { cleanup(); vi.resetAllMocks() })

test('keeps a failed draft for retry and refreshes the saved preference', async () => {
  let rows = [editable, fixed]
  vi.mocked(api.promptPreferences).mockImplementation(async () => rows)
  vi.mocked(api.setPromptPreference).mockRejectedValueOnce(new Error('network unavailable'))
    .mockImplementationOnce(async (_functionName, text) => { rows = [{ ...editable, user_instruction: text }, fixed]; return { saved: true } })

  render(<PromptPreferencesSection readOnly={false} />)
  const editor = await screen.findByRole('textbox', { name: /我的表达偏好/ })
  fireEvent.change(editor, { target: { value: '先给出结论' } })
  expect(screen.getAllByText('先给出结论')).toHaveLength(2)
  fireEvent.click(screen.getByRole('button', { name: '保存此偏好' }))
  await waitFor(() => expect(screen.getByRole('button', { name: '重试保存' })).toBeTruthy())
  expect((editor as HTMLTextAreaElement).value).toBe('先给出结论')
  fireEvent.click(screen.getByRole('button', { name: '重试保存' }))
  await waitFor(() => expect(screen.getByText('已保存，对之后的新请求生效')).toBeTruthy())
  expect(vi.mocked(api.setPromptPreference).mock.calls).toEqual([['chat', '先给出结论'], ['chat', '先给出结论']])
  fireEvent.click(screen.getByRole('button', { name: '恢复默认' }))
  expect((editor as HTMLTextAreaElement).value).toBe('')
  fireEvent.click(screen.getByRole('button', { name: '撤销修改' }))
  expect((editor as HTMLTextAreaElement).value).toBe('先给出结论')
})
