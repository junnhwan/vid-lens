import assert from 'node:assert/strict'
import { describe, it } from 'node:test'
import { parseInline, parseMarkdown, peelDomainTags, unwrapMarkdownFence } from './markdown.ts'

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

  it('unwraps a wrapping markdown fence so headings still parse', () => {
    const wrapped = unwrapMarkdownFence('```markdown\n# 分析报告\n\n## 核心摘要\n- **要点**\n```')
    assert.equal(wrapped.startsWith('# 分析报告'), true)
    const blocks = parseMarkdown('```markdown\n# 分析报告\n\n## 核心摘要\n- 要点\n```')
    assert.equal(blocks[0].type, 'h')
    assert.equal(blocks[1].type, 'h')
    assert.equal(blocks[2].type, 'ul')
  })

  it('does not unwrap a real code fence with leftover content', () => {
    const blocks = parseMarkdown('```ts\nconst x = 1\n```\n后文')
    assert.equal(blocks[0].type, 'pre')
    assert.equal(blocks[1].type, 'p')
  })
})

describe('peelDomainTags', () => {
  it('lifts a trailing 领域标签 heading into tags', () => {
    const { blocks, tags } = peelDomainTags(parseMarkdown('# 报告\n\n## 领域标签\n- Agent\n- 后端架构'))
    assert.equal(blocks.filter(b => b.type === 'h').length, 1)
    assert.deepEqual(tags, ['Agent', '后端架构'])
  })

  it('splits an inline 领域标签 line', () => {
    const { tags } = peelDomainTags(parseMarkdown('一段话\n\n**领域标签**：大模型、后端、Agent'))
    assert.deepEqual(tags, ['大模型', '后端', 'Agent'])
  })
})

describe('parseInline', () => {
  it('keeps cite chips, code and bold', () => {
    const nodes = parseInline('结论[C1]用 `foo` 和 **粗体**')
    assert.deepEqual(nodes.map(n => n.type), ['text', 'cite', 'text', 'code', 'text', 'strong'])
  })
})
