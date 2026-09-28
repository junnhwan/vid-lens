import assert from 'node:assert/strict'
import test from 'node:test'
import { createElement } from 'react'
import { renderToStaticMarkup } from 'react-dom/server'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import { studyFixture, evidenceFixtures } from '../../dev/productFixtures.ts'
import { addBlock, deleteBlock, descendantCount, mergeWithNext, moveBlock } from './edit.ts'
import { bodySchema } from './schema.ts'
import { savedMarkdown } from './markdown.ts'

const source = () => structuredClone(studyFixture.version!.body)

test('structural edits move subtrees while preserving untouched block IDs and valid parent order', () => {
  const before = source()
  const moved = moveBlock(before, 'build', 'down')
  assert.deepEqual(moved.blocks.map(block => block.block_id), ['config', 'ports', 'build', 'image', 'stages', 'verify', 'logs'])
  assert.equal(moved.blocks.find(block => block.block_id === 'image')?.parent_id, 'build')
  const nested = moveBlock(before, 'stages', 'indent')
  assert.equal(nested.blocks.find(block => block.block_id === 'stages')?.parent_id, 'image')
  const raised = moveBlock(nested, 'stages', 'outdent')
  assert.equal(raised.blocks.find(block => block.block_id === 'stages')?.parent_id, 'build')
  assert.equal(bodySchema.safeParse(raised).success, true)
  assert.deepEqual(before.blocks.map(block => block.block_id), source().blocks.map(block => block.block_id))
})

test('delete scopes a subtree and a draft snapshot restores it; add keeps old identities', () => {
  const before = source()
  assert.equal(descendantCount(before, 'build'), 3)
  const after = deleteBlock(before, 'build')
  assert.deepEqual(after.blocks.map(block => block.block_id), ['config', 'ports', 'verify', 'logs'])
  const restored = before
  assert.equal(restored.blocks.length, 7)
  assert.equal(addBlock(before, 'image', true, 'new-block').blocks.find(block => block.block_id === 'new-block')?.parent_id, 'image')
  assert.throws(() => deleteBlock({ ...before, blocks: before.blocks.slice(0, 3) }, 'build'), /至少/)
})

test('merge preserves distinct content, references and child nodes', () => {
  const before = source()
  const next = before.blocks.find(block => block.block_id === 'stages')!
  next.evidence_refs.push({ evidence_id: 'preview-e3', relation: 'context' })
  next.source_block_ids = ['source-stages']
  before.blocks.find(block => block.block_id === 'image')!.source_block_ids = ['source-image']
  const withChild = addBlock(before, 'stages', true, 'stages-child')
  const merged = mergeWithNext(withChild, 'image')
  const first = merged.blocks.find(block => block.block_id === 'image')!
  assert.match(first.content, /多阶段构建/)
  assert.match(first.content, /builder/)
  assert.deepEqual(first.evidence_refs.map(ref => ref.evidence_id), ['preview-e1', 'preview-e2', 'preview-e3'])
  assert.deepEqual(first.source_block_ids, ['source-image', 'source-stages'])
  assert.equal(merged.blocks.some(block => block.block_id === 'stages'), false)
  assert.equal(merged.blocks.find(block => block.block_id === 'stages-child')?.parent_id, 'image')
  assert.equal(merged.blocks.find(block => block.block_id === 'config')?.block_id, 'config')
  assert.equal(bodySchema.safeParse(merged).success, true)
})

test('Markdown uses immutable version and safe first-party links without media credentials', () => {
  const evidence = new Map(evidenceFixtures.map(row => [row.id, row]))
  const markdown = savedMarkdown(studyFixture, evidence, 'https://vidlens.example')
  assert.match(markdown, /^# 从代码到容器/m)
  assert.match(markdown, /已保存版本：v1/)
  assert.match(markdown, /\[查看此版本\]\(https:\/\/vidlens.example\/artifacts\/preview-study\?version=preview-version-1\)/)
  assert.match(markdown, /## 来源/)
  assert.match(markdown, /\/video\/42\?t=246000/)
  assert.doesNotMatch(markdown, /token=|Bearer |signature=/i)
  const outdated = savedMarkdown({ ...studyFixture, version: { ...studyFixture.version!, source_status: 'outdated' } }, evidence, 'https://vidlens.example')
  assert.doesNotMatch(outdated, /\/video\/42\?t=/)
  assert.throws(() => savedMarkdown(studyFixture, new Map(), 'https://vidlens.example'), /权限核对/)
  const withSecret = structuredClone(studyFixture)
  withSecret.version!.body.blocks[0].content += '\nBearer FAKE_TEST_ACCESS_TOKEN_123456789\nhttps://media.example/video?token=signed-example'
  const cleaned = savedMarkdown(withSecret, evidence, 'https://vidlens.example')
  assert.doesNotMatch(cleaned, /FAKE_TEST_ACCESS_TOKEN|signed-example/)
  const html = renderToStaticMarkup(createElement(ReactMarkdown, { remarkPlugins: [remarkGfm] }, markdown))
  assert.match(html, /<h1>从代码到容器，理解 Go 服务的交付<\/h1>/)
  assert.match(html, /<h2>构建与交付<\/h2>/)
  assert.match(html, /<h2>来源<\/h2>/)
  assert.match(html, /href="https:\/\/vidlens.example\/video\/42\?t=246000"/)
})

test('merging answer notes retains every chat citation label for shared evidence', () => {
  const before = source()
  const first = before.blocks.find(block => block.block_id === 'image')!
  const second = before.blocks.find(block => block.block_id === 'stages')!
  first.evidence_refs = [{ evidence_id: 'preview-e1', relation: 'context', chat_citation_id: 'C1' }]
  second.evidence_refs = [
    { evidence_id: 'preview-e1', relation: 'context', chat_citation_id: 'C1' },
    { evidence_id: 'preview-e1', relation: 'context', chat_citation_id: 'C2' },
  ]
  const merged = mergeWithNext(before, 'image')
  assert.deepEqual(merged.blocks.find(block => block.block_id === 'image')!.evidence_refs.map(ref => ref.chat_citation_id), ['C1', 'C2'])
  const markdown = savedMarkdown({ ...studyFixture, version: { ...studyFixture.version!, body: merged } }, new Map(evidenceFixtures.map(row => [row.id, row])), 'https://vidlens.example')
  assert.match(markdown, /聊天引用 C1/)
  assert.match(markdown, /聊天引用 C2/)
})
