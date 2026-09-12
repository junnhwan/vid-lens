import assert from 'node:assert/strict'
import test from 'node:test'
import { mergeRunHistory } from './conversationHistory.ts'

test('refresh reconstructs pending execution identity from authoritative run history', () => {
  const run = { run_id: 'active', question: 'still working', status: 'running', created_at: '2026-01-01', steps: [] }
  const freshPage = mergeRunHistory([], [run])
  assert.equal(freshPage[1].agentRunId, 'active')
  assert.match(freshPage[1].error!, /仍在进行/)
  assert.equal(mergeRunHistory(freshPage, [run]).length, 2)
})

test('unsaved failures survive reload without duplicating committed runs', () => {
  const runs = [
    { run_id: 'saved', question: 'saved question', status: 'budget_exhausted', created_at: '2026-01-01', steps: [] },
    { run_id: 'failed', question: 'new question', status: 'failed', error: 'planner failed', created_at: '2026-01-02', steps: [] },
  ]
  const messages = mergeRunHistory([{ role: 'assistant', content: 'saved answer', agentRunId: 'saved', createdAt: 1 }], runs)
  assert.equal(messages.length, 3)
  assert.equal(messages[2]?.agentRunId, 'failed')
  assert.equal(messages[2]?.content, '')
  assert.equal(messages[2]?.error, 'planner failed')
  assert.equal(mergeRunHistory(messages, runs).length, 3)
})
