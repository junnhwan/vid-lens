// @vitest-environment jsdom
import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, expect, test } from 'vitest'
import { MarkdownAnswer } from './MarkdownAnswer'

afterEach(cleanup)

test('renders nested Markdown lists and citation controls without recursively wrapping itself', () => {
  render(<MarkdownAnswer content={'1. **安装方式**：使用 `Homebrew`。[C1]\n2. **模型切换**：\n   - 使用 `Ctrl+P`。[C2]'} onCite={() => {}} />)
  expect(screen.getByText(/安装方式/)).toBeTruthy()
  expect(screen.getByText(/Ctrl\+P/)).toBeTruthy()
  expect(screen.getByRole('button', { name: 'C1' })).toBeTruthy()
  expect(screen.getByRole('button', { name: 'C2' })).toBeTruthy()
})
