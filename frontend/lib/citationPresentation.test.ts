import test from 'node:test'
import assert from 'node:assert/strict'
import { presentAnswerCitations } from './citationPresentation.ts'
import type { CiteRef } from '../components/Citation'

const cite = (id: string, startMS = 1000): CiteRef => ({ id, taskId: 2, chunkIndex: 0, score: 1, content: `原文${id}`, startMS, endMS: startMS + 24000, timeRangeStatus: 'coarse', supportStatus: 'unsupported', sourceRefs: [{ source_type: 'transcript', stable_id: 'asr:71', source_row_id: 71, start_ms: startMS, end_ms: startMS + 24000, time_range_status: 'coarse' }] })

test('first appearance determines consecutive labels, repeated references and provenance stay stable', () => {
  const citations = [cite('C3'), cite('C4'), cite('C6', 838000), cite('C14', 798000), cite('C15', 798000), cite('C16'), cite('C17')]
  const original = structuredClone(citations)
  const result = presentAnswerCitations('高成本[C14][C15][C16][C17]。低成本[C3][C4]，后续[C6]，再次[C14]。', citations)
  assert.equal(result.content, '高成本[C1][C2][C3][C4]。低成本[C5][C6]，后续[C7]，再次[C1]。')
  assert.deepEqual(result.cites.map(c => c.id), ['C1','C2','C3','C4','C5','C6','C7'])
  assert.deepEqual(result.cites.map(c => c.candidateId), ['C14','C15','C16','C17','C3','C4','C6'])
  assert.equal(result.cites[0].startMS, 798000)
  assert.equal(result.cites[0].supportStatus, 'unsupported')
  assert.deepEqual(result.cites[0].sourceRefs, citations[3].sourceRefs)
  assert.deepEqual(citations, original)
})

test('code, links and escaped examples cannot consume labels or be rewritten', () => {
  const content = '`[C14]` [链接[C14]](https://example.com/[C14])\n\n```text\n[C14]\n```\n\n\\[C14] 正文**依据[C3]**，再引用[C14]'
  const result = presentAnswerCitations(content, [cite('C14'), cite('C3')])
  assert.equal(result.content, content.replace('依据[C3]', '依据[C1]').replace('再引用[C14]', '再引用[C2]'))
  assert.deepEqual(result.cites.map(c => c.candidateId), ['C3','C14'])
})

test('Unicode claim offsets are inserted before numbering, snapshots without links remain deterministic', () => {
  const result = presentAnswerCitations('😀甲。乙。', [{ ...cite('C9'), claimEndRunes: [5] }, { ...cite('C3'), claimEndRunes: [3] }])
  assert.equal(result.content, '😀甲。[C1]乙。[C2]')
  assert.deepEqual(result.cites.map(c => c.candidateId), ['C3','C9'])
  assert.deepEqual(presentAnswerCitations('旧回答', [cite('C7'),cite('C9')]).cites.map(c=>c.id), ['C1','C2'])
})

test('unknown labels cannot accidentally point at a renumbered source', () => {
  assert.equal(presentAnswerCitations('未验证[C1]，有来源[C14]', [cite('C14')]).content, '未验证，有来源[C1]')
})

test('appending streamed references keeps previous labels and numbering resets per answer', () => {
  const candidates = [cite('C3'),cite('C14')]
  assert.equal(presentAnswerCitations('先说[C14]', candidates).content, '先说[C1]')
  assert.equal(presentAnswerCitations('先说[C14]，后说[C3]，再次[C14]', candidates).content, '先说[C1]，后说[C2]，再次[C1]')
  assert.equal(presentAnswerCitations('另一回答[C3]', [cite('C3')]).content, '另一回答[C1]')
})
