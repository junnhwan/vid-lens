// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, expect, test, vi } from 'vitest'
import { api } from '@/lib/api'
import { FollowUpQuestions } from './FollowUpQuestions'
import type { VideoQuestionResult } from '@/lib/types'

afterEach(() => { cleanup(); vi.restoreAllMocks() })
test('a pending follow-up request shows the generation status until questions arrive', async () => {
  let resolve!: (result: VideoQuestionResult) => void
  vi.spyOn(api, 'getFollowUpQuestions').mockReturnValue(new Promise(result => { resolve = result }))
  render(<FollowUpQuestions sessionId={9} messageId={108} onAsk={() => {}} />)
  expect(screen.getByRole('status').textContent).toContain('正在整理追问')
  resolve({ status: 'ready', message: '', message_id: 108, questions: [{ question: '有什么适用条件？', source: '追问', excerpt: '' }] })
  expect(await screen.findByRole('button', { name: /有什么适用条件/ })).toBeTruthy()
  expect(screen.queryByRole('status')).toBeNull()
})
test('follow-up requests use the saved message ID and clicks submit the question', async () => {
  const read = vi.spyOn(api, 'getFollowUpQuestions').mockResolvedValue({ status: 'ready', message: '', message_id: 108, questions: [{ question: '端口映射失败时怎么排查？', source: '追问', excerpt: '' }] })
  const ask = vi.fn()
  render(<FollowUpQuestions sessionId={9} messageId={108} onAsk={ask} />)
  fireEvent.click(await screen.findByRole('button', { name: /端口映射失败/ }))
  expect(read).toHaveBeenCalledWith(9, 108)
  expect(ask).toHaveBeenCalledWith('端口映射失败时怎么排查？')
})
