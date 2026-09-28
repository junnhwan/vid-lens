import assert from 'node:assert/strict'
import test from 'node:test'
import { canvasDescendants, canvasKind, computeCanvasLayout, emptyCanvasLayout, fillCanvasPositions, visibleCanvasIds } from './canvas.ts'
import type { StudyBody } from './schema.ts'

const body: StudyBody = { schema_version: 1, kind: 'study', title: '安装和模型配置', warnings: [], blocks: [
  { block_id: 'section', parent_id: null, type: 'section', title: '安装', content: '', claim_origin: 'user', evidence_refs: [] },
  { block_id: 'command', parent_id: 'section', type: 'example', title: '运行命令', content: '`vidlens start`', claim_origin: 'user', evidence_refs: [] },
  { block_id: 'concept', parent_id: 'section', type: 'concept', title: '模型配置', content: '这里是一段中文说明', claim_origin: 'user', evidence_refs: [] },
] }

test('canvas categories and visibility follow content tree without deleting body', () => {
  const layout = fillCanvasPositions(body, emptyCanvasLayout())
  assert.equal(canvasKind('example', '`vidlens start`'), 'command')
  assert.deepEqual([...canvasDescendants(body, 'section')], ['section', 'command', 'concept'])
  assert.equal(Object.keys(layout.nodes).length, 3)
  layout.nodes.section.collapsed = true
  assert.deepEqual([...visibleCanvasIds(body, layout)], ['section'])
  assert.equal(body.blocks.length, 3)
})

test('ELK local layout preserves pinned and out-of-scope coordinates', async () => {
  const layout = fillCanvasPositions(body, emptyCanvasLayout())
  layout.nodes.section.position = { x: 612, y: 319 }
  layout.nodes.section.pinned = true
  layout.nodes.concept.position = { x: -510, y: -110 }
  const originalSection = { ...layout.nodes.section.position }
  const originalConcept = { ...layout.nodes.concept.position }
  const result = await computeCanvasLayout(body, layout, 'DOWN', new Set(['section', 'command']))
  assert.deepEqual(result.layout.nodes.section.position, originalSection)
  assert.deepEqual(result.layout.nodes.concept.position, originalConcept)
  assert.equal(result.layout.direction, 'DOWN')
})

test('new grouping parent appears beside existing fixed children', () => {
  const grouped: StudyBody = { ...body, blocks: [
    { block_id: 'group', parent_id: null, type: 'section', title: '入门基础', content: '', claim_origin: 'user', evidence_refs: [] },
    { ...body.blocks[0], parent_id: 'group' },
    { ...body.blocks[1], parent_id: 'group' },
    body.blocks[2],
  ] }
  const prior = fillCanvasPositions(body, emptyCanvasLayout())
  prior.nodes.section.position = { x: 420, y: 20 }
  prior.nodes.command.position = { x: 500, y: 140 }
  prior.nodes.section.pinned = true
  prior.nodes.command.pinned = true
  const next = fillCanvasPositions(grouped, prior)
  assert.deepEqual(next.nodes.group.position, { x: 70, y: 80 })
  assert.deepEqual(next.nodes.section.position, { x: 420, y: 20 })
  assert.deepEqual(next.nodes.command.position, { x: 500, y: 140 })
})

test('long Chinese 200-node canvas lays out finite coordinates', async () => {
  const many: StudyBody = { ...body, blocks: Array.from({ length: 200 }, (_, index) => ({ block_id: `b${index}`, parent_id: index ? 'b0' : null, type: index ? 'concept' : 'section', title: `第${index}节：${'非常长的中文标题'.repeat(8)}`, content: '正文'.repeat(80), claim_origin: 'user', evidence_refs: [] })) }
  const result = await computeCanvasLayout(many, emptyCanvasLayout(), 'RIGHT')
  assert.equal(Object.keys(result.layout.nodes).length, 200)
  assert.ok(Object.values(result.layout.nodes).every(node => Number.isFinite(node.position.x) && Number.isFinite(node.position.y)))
})
