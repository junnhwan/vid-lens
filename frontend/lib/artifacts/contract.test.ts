import assert from 'node:assert/strict'
import test from 'node:test'
import { bodySchema, detailSchema, evidenceSchema, runSchema } from './schema.ts'
import { canReplay, evidenceTime, mapTree } from './view.ts'
import { studyFixture, evidenceFixtures, runFixture } from '../../dev/productFixtures.ts'

test('first-delivery specimens validate against the API contract', () => {
  assert.equal(detailSchema.safeParse(studyFixture).success, true)
  assert.equal(runSchema.safeParse(runFixture).success, true)
  for (const evidence of evidenceFixtures) assert.equal(evidenceSchema.safeParse(evidence).success, true)
})
test('unknown timestamps never become a replay link even with stray numeric coordinates', () => {
  const evidence = { ...evidenceFixtures[0], time_range_status: 'unknown' as const, start_ms: 0, end_ms: 1000 }
  assert.equal(canReplay(evidence), false)
  assert.equal(evidenceTime(evidence), '时间未知')
  assert.match(evidenceTime(evidenceFixtures[1]), /^约 08:42/)
})
test('invalid schema, broken parent order, duplicate identities and unsupported claims are rejected', () => {
  const body = structuredClone(studyFixture.version!.body)
  assert.equal(bodySchema.safeParse({ ...body, schema_version: 2 }).success, false)
  assert.equal(bodySchema.safeParse({ ...body, blocks: [...body.blocks].reverse() }).success, false)
  assert.equal(bodySchema.safeParse({ ...body, blocks: [...body.blocks, body.blocks[0]] }).success, false)
  assert.equal(bodySchema.safeParse({ ...body, blocks: [{ ...body.blocks[0], evidence_refs: [] }] }).success, false)
})
test('map projection escapes labels and identities before giving content to the HTML renderer', () => {
  const body = structuredClone(studyFixture.version!.body)
  body.title = '<img src=x onerror=alert(1)>'
  body.blocks = [{ ...body.blocks[0], title: '<script>alert(1)</script>', block_id: '" onclick="alert(1)' }]
  const tree = mapTree(body)
  assert.equal(tree.content.includes('<img'), false)
  assert.equal(tree.children[0].content.includes('<script>'), false)
  assert.match(tree.children[0].content, /data-block-id="&quot; onclick=&quot;/)
})
test('200 Chinese nodes remain one content tree; invalid depth is rejected', () => {
  const body = structuredClone(studyFixture.version!.body)
  body.blocks = Array.from({ length: 200 }, (_, i) => ({ ...body.blocks[0], block_id: `b${i}`, parent_id: i === 0 ? null : 'b0', title: `中文概念 ${i}：较长的标签应该换行并能打开证据` }))
  assert.equal(bodySchema.safeParse(body).success, true)
  assert.equal(mapTree(body).children[0].children.length, 199)
  body.blocks = body.blocks.slice(0, 9).map((block, i) => ({ ...block, parent_id: i === 0 ? null : `b${i - 1}` }))
  assert.equal(bodySchema.safeParse(body).success, false)
})
