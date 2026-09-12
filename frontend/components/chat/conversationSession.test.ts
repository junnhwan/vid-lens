import assert from 'node:assert/strict'
import test from 'node:test'

import {
  conversationSessionReducer,
  emptyConversationSessionState,
} from './conversationSession.ts'

test('conversation session reducer owns RAG message patch and terminal error state', () => {
  let state = emptyConversationSessionState()
  state = conversationSessionReducer(state, { type: 'rag_start', question: '问题' })
  state = conversationSessionReducer(state, { type: 'answer_delta', delta: '部分回答' })
  state = conversationSessionReducer(state, { type: 'rag_event', event: 'answer' })
  state = conversationSessionReducer(state, { type: 'stream_error', message: '失败' })

  assert.equal(state.streaming, false)
  assert.equal(state.messages.length, 2)
  assert.equal(state.messages[1]?.content, '部分回答')
  assert.equal(state.messages[1]?.error, '失败')
  assert.equal(state.ragTrace.at(-1)?.status, 'error')
})

test('conversation session reducer keeps replayed Agent events idempotent', () => {
  let state = emptyConversationSessionState()
  state = conversationSessionReducer(state, { type: 'agent_start', question: '研究' })
  state = conversationSessionReducer(state, {
    type: 'agent_event',
    event: { type: 'run_start', data: { run_id: 'run-1', mode: 'agent', scope_type: 'video' } },
  })
  const step = {
    type: 'step_start' as const,
    data: { run_id: 'run-1', step_id: 's1', kind: 'retrieve', label: '检索', status: 'running' },
  }
  state = conversationSessionReducer(state, { type: 'agent_event', event: step })
  state = conversationSessionReducer(state, { type: 'agent_event', event: step })
  state = conversationSessionReducer(state, { type: 'stream_cancelled' })

  assert.equal(state.agentTrace.steps.length, 1)
  assert.equal(state.messages[1]?.trace?.length, 1)
  assert.equal(state.messages[1]?.streaming, false)
  assert.equal(state.messages[1]?.error, undefined)
  assert.equal(state.messages[1]?.trace?.[0]?.status, 'cancelled')
})

test('reasoning stays separate and final persisted answer replaces deltas including empty answers', () => {
  let state = conversationSessionReducer(emptyConversationSessionState(), { type: 'agent_start', question: '研究' })
  state = conversationSessionReducer(state, { type: 'reasoning', event: { call_id: 'plan-1', delta: '检查证据' } })
  state = conversationSessionReducer(state, { type: 'answer_delta', delta: '临时答案' })
  state = conversationSessionReducer(state, { type: 'progress', event: { id: 'save', kind: 'save', label: '保存', status: 'running' } })
  state = conversationSessionReducer(state, { type: 'stream_done', patch: { content: '' } })
  state = conversationSessionReducer(state, { type: 'stream_cancelled' })
  assert.equal(state.messages[1]?.content, '')
  assert.equal(state.messages[1]?.reasoning?.['plan-1'], '检查证据')
  assert.equal(state.messages[1]?.cancelled, undefined)
  assert.equal(state.messages[1]?.trace?.[0]?.status, 'done')
})

test('failed planning closes the live timeline before any tool starts', () => {
  let state = conversationSessionReducer(emptyConversationSessionState(), { type: 'agent_start', question: '研究' })
  state = conversationSessionReducer(state, { type: 'progress', event: { id: 'plan-1', kind: 'plan', label: '规划', status: 'running' } })
  state = conversationSessionReducer(state, { type: 'stream_error', message: 'invalid arguments' })
  assert.equal(state.messages[1]?.trace?.[0]?.status, 'error')
  assert.equal(state.agentTrace.finished, true)
})
