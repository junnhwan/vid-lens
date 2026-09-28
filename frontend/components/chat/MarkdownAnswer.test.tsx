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

test('summary Markdown keeps hard breaks, images and task checkboxes as void elements', () => {
  const { container } = render(<MarkdownAnswer domainTags content={'教程名称  \n下一行 [C1]\n\n| 名称 | 操作 |\n| --- | --- |\n| **Pi** | 运行 `pi` |\n\n- [x] 安装完成 [C2]\n\n![步骤图](https://example.com/step.png)'} onCite={() => {}} />)
  expect(container.querySelector('br')).toBeTruthy()
  expect(screen.getByRole('table')).toBeTruthy()
  expect(screen.getByRole('checkbox')).toHaveProperty('checked', true)
  expect(screen.getByRole('img', { name: '步骤图' })).toBeTruthy()
  expect(screen.getByRole('button', { name: 'C1' })).toBeTruthy()
  expect(screen.getByRole('button', { name: 'C2' })).toBeTruthy()
})
