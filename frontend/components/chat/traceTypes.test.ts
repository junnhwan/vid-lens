import assert from 'node:assert/strict'
import test from 'node:test'

import {
  agentTraceReducer,
  emptyAgentTraceState,
} from './traceTypes.ts'

test('agentTraceReducer is idempotent for replayed run and step events', () => {
  let state = emptyAgentTraceState()
  state = agentTraceReducer(state, {
    type: 'run_start',
    data: { run_id: 'run-1', mode: 'agent', scope_type: 'video', task_id: 9 },
  })
  const start = {
    type: 'step_start' as const,
    data: { run_id: 'run-1', step_id: 's1', kind: 'retrieve', label: '检索', status: 'running' },
  }
  state = agentTraceReducer(state, start)
  state = agentTraceReducer(state, start)
  state = agentTraceReducer(state, {
    type: 'step_done',
    data: { ...start.data, status: 'done', hits: 2 },
  })

  assert.equal(state.steps.length, 1)
  assert.equal(state.steps[0]?.status, 'done')
  assert.equal(state.steps[0]?.hits, 2)
})
