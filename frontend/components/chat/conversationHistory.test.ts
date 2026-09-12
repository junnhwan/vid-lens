import assert from 'node:assert/strict'
import test from 'node:test'
import { mergeRunHistory } from './conversationHistory.ts'

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
