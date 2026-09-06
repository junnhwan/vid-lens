import assert from 'node:assert/strict'
import { describe, it } from 'node:test'
import { parseInline, parseMarkdown } from './markdown.ts'

describe('parseMarkdown', () => {
  it('splits headings, lists and paragraphs', () => {
    const blocks = parseMarkdown('# 标题\n\n一段话\n\n- a\n- b\n\n1. one\n2. two')
    assert.equal(blocks[0].type, 'h')
    assert.equal(blocks[1].type, 'p')
    assert.equal(blocks[2].type, 'ul')
    assert.equal(blocks[3].type, 'ol')
  })

  it('captures fenced code including unclosed stream', () => {
    const closed = parseMarkdown('前文\n```ts\nconst x = 1\n```\n后')
    assert.equal(closed[1].type, 'pre')
    if (closed[1].type === 'pre') {
      assert.equal(closed[1].lang, 'ts')
      assert.equal(closed[1].code, 'const x = 1')
    }
    const open = parseMarkdown('```js\nconst a')
    assert.equal(open[0].type, 'pre')
    if (open[0].type === 'pre') assert.equal(open[0].code, 'const a')
  })
})

describe('parseInline', () => {
  it('keeps cite chips, code and bold', () => {
    const nodes = parseInline('结论[C1]用 `foo` 和 **粗体**')
    assert.deepEqual(nodes.map(n => n.type), ['text', 'cite', 'text', 'code', 'text', 'strong'])
  })
})
