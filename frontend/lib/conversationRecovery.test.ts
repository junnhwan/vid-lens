import test from 'node:test'
import assert from 'node:assert/strict'
import { recoverConversationRun } from './conversationRecovery.ts'

test('recovery reads run then committed messages without executing again', async () => {
  const calls: string[] = []
  const message = { id: 'r1', content: 'saved answer' }
  const result = await recoverConversationRun('r1', {
    detail: async () => { calls.push('GET run'); return { status: 'completed' } },
    messages: async () => { calls.push('GET messages'); return [message] },
    runId: message => message.id,
  })
  assert.equal(result.message, message)
  assert.deepEqual(calls, ['GET run', 'GET messages'])
})
for (const status of ['pending', 'running', 'failed', 'cancelled', 'budget_exhausted', 'completed']) {
  test(`recovery keeps ${status} distinct from saved success`, async () => {
    const result = await recoverConversationRun('r', { detail: async () => ({ status }), messages: async () => [], runId: () => undefined })
    assert.equal(result.message, undefined)
    assert.ok(result.notice)
    assert.equal(result.status, status === 'completed' ? 'unconfirmed' : status)
  })
}
test('network failure leaves result unconfirmed', async () => {
  const result = await recoverConversationRun('r', { detail: async () => { throw new Error('offline') }, messages: async () => [], runId: () => undefined })
  assert.equal(result.status, 'unconfirmed')
})
