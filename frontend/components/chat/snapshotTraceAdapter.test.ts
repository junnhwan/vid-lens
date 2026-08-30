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
  assert.equal(parsed?.steps[0]?.status, 'error')
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
