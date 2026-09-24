import assert from 'node:assert/strict'
import test from 'node:test'

import { parseSnapshotTrace } from './snapshotTraceAdapter.ts'

test('snapshot adapter prefers versioned steps and preserves cancelled terminal state', () => {
  const parsed = parseSnapshotTrace(JSON.stringify({
    version: 1,
    run_id: 'run-2',
    mode: 'agent',
    steps: [{ step_id: 's1', kind: 'tool', label: '工具', status: 'cancelled', error: '已取消' }],
    trace: [{ name: '旧步骤', tool: 'search_transcript' }],
  }))
  assert.equal(parsed?.source, 'agent')
  assert.equal(parsed?.runId, 'run-2')
  assert.equal(parsed?.steps.length, 1)
  assert.equal(parsed?.steps[0]?.status, 'cancelled')
  assert.equal(parsed?.steps[0]?.error, '已取消')
})

test('snapshot adapter keeps legacy trace and bare citation compatibility isolated', () => {
  const legacy = parseSnapshotTrace(JSON.stringify({
    mode: 'research',
    trace: [{ name: '检索', tool: 'search_transcript', output_ref: 'hits:2' }],
  }))
  const citations = parseSnapshotTrace(JSON.stringify([{ citation_id: 'C1' }, { citation_id: 'C2' }]))
  assert.equal(legacy?.source, 'legacy')
  assert.equal(legacy?.steps[0]?.toolOutput, 'hits:2')
  assert.equal(citations?.source, 'inferred')
  assert.equal(citations?.steps[0]?.hits, 2)
})


test('budget-limited Agent history retains its degraded result', () => {
  const parsed = parseSnapshotTrace(JSON.stringify({ version: 1, mode: 'agent', run_id: 'budget', steps: [], citations: [], degraded: true }))
  assert.equal(parsed?.degraded, true)
  assert.equal(parsed?.runId, 'budget')
})

test('Chat retrieval fallback survives history reload without becoming an Agent run', () => {
  const parsed = parseSnapshotTrace(JSON.stringify({ citations: [], degraded: true, degradation_reason: 'retrieval_unavailable', diagnostic_id: 'trace-123' }))
  assert.equal(parsed?.degraded, true)
  assert.equal(parsed?.degradationReason, 'retrieval_unavailable')
  assert.equal(parsed?.diagnosticId, 'trace-123')
  assert.equal(parsed?.isAgentEnvelope, false)
})

test('saved Chat stages return as public execution records after reload', () => {
  const parsed = parseSnapshotTrace(JSON.stringify({ mode: 'chat', citations: [], steps: [
    { step_id: 'retrieve', kind: 'retrieve', label: '检索视频证据', status: 'done', tool: 'video_evidence_search', input: { summary: '授权范围' }, output: '找到 2 条候选引用', duration_ms: 123 },
  ] }))
  assert.equal(parsed?.isAgentEnvelope, false)
  assert.equal(parsed?.source, 'server')
  assert.equal(parsed?.steps[0]?.durationMs, 123)
  assert.match(parsed?.steps[0]?.toolInput || '', /授权范围/)
})
