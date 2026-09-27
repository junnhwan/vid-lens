import assert from 'node:assert/strict'
import test from 'node:test'
import { bodySchema, detailSchema, evidenceSchema, runSchema } from './schema.ts'
import { artifactCardStatus, canReplay, evidenceTime, isPointEvidence, mapTree, warningMessage } from './view.ts'
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
test('point evidence seeks to its frame and generated warnings read as product copy', () => {
  const point = { ...evidenceFixtures[0], modality: 'visual_caption', time_range_status: 'precise' as const, start_ms: 60_000, end_ms: 60_001 }
  assert.equal(canReplay(point), true)
  assert.equal(isPointEvidence(point), true)
  assert.equal(evidenceTime(point), '01:00 · 画面时间点')
  assert.match(warningMessage('covered_segments:2/3'), /2\/3/)
  assert.match(warningMessage('coverage_is_observations_not_all_video_frames'), /抽样观察/)
})

test('artifact states use the server latest run and preserve a readable prior version', () => {
  for (const [status, label] of [['pending', '排队中'], ['running', '生成中'], ['failed', '生成失败'], ['cancelled', '已取消'], ['budget_exhausted', '预算已用尽']] as const) {
    const empty = { ...studyFixture, current_version_id: null, head_version: 0, latest_run: { ...runFixture, status } }
    assert.equal(artifactCardStatus(empty), label)
  }
  assert.match(artifactCardStatus({ ...studyFixture, latest_run: { ...runFixture, status: 'failed' } }), /v1.*最近一次：生成失败/)
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
