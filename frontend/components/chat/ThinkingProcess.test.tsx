// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, expect, test } from 'vitest'
import type { ChatMsg } from './chatUtils'
import { ThinkingProcess } from './ThinkingProcess'

afterEach(cleanup)
const message: ChatMsg = { role: 'assistant', content: '', streaming: true, trace: [{ id: 'retrieve', kind: 'retrieve', label: '查找来源', status: 'done', detail: '两条来源仍可查看' }] }
test('completion keeps a live process open at the same reading position', () => {
  const { rerender } = render(<ThinkingProcess message={message} />)
  rerender(<ThinkingProcess message={{ ...message, streaming: false }} />)
  expect(screen.getByRole('button').getAttribute('aria-expanded')).toBe('true')
  expect(screen.getByText('两条来源仍可查看').closest('[hidden]')).toBeNull()
  expect(screen.getByLabelText('思考与执行过程').getAttribute('data-state')).toBe('completed')
})
test('an explicit fold is preserved when the answer completes', () => {
  const { rerender } = render(<ThinkingProcess message={message} />)
  fireEvent.click(screen.getByRole('button'))
  rerender(<ThinkingProcess message={{ ...message, streaming: false }} />)
  expect(screen.getByRole('button').getAttribute('aria-expanded')).toBe('false')
})
test('a disconnected stream with a still-running server does not show a completed status', () => {
  render(<ThinkingProcess message={{ ...message, streaming: false, runStatus: 'running' }} />)
  expect(screen.getByText('服务端仍在执行')).toBeTruthy()
  expect(screen.getByLabelText('思考与执行过程').getAttribute('data-state')).toBe('running')
})
