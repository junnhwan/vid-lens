import test from 'node:test'
import assert from 'node:assert/strict'
import React from 'react'
import { renderToStaticMarkup } from 'react-dom/server'
import { renderCitationNodes } from './citationNodes.ts'

test('nested custom Markdown paragraphs render once with inline citations', () => {
  let renders = 0
  function Paragraph({ children }: { children?: React.ReactNode }) {
    assert.ok(++renders < 5, 'citation traversal recursively rewraps a renderer')
    return React.createElement('p', {}, renderCitationNodes(children, () => {}))
  }
  const paragraph = React.createElement(Paragraph, {}, React.createElement('strong', {}, '条件[C1]'))
  const html = renderToStaticMarkup(React.createElement('li', {}, renderCitationNodes(paragraph, () => {})))
  assert.equal(renders, 1)
  assert.match(html, /<strong>条件<button/)
  assert.match(html, /C1<\/button>/)
})

test('code and links retain literal citation syntax', () => {
  const code = React.createElement('code', {}, '[C1]')
  const link = React.createElement('a', { href: 'https://example.com' }, '[C2]')
  const html = renderToStaticMarkup(React.createElement('p', {}, renderCitationNodes([code, link], () => {})))
  assert.match(html, /<code>\[C1\]<\/code>/)
  assert.doesNotMatch(html, /<button/)
})
