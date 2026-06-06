import assert from 'node:assert/strict'

import { needsResultDetail, needsTaskDetail, taskFailureMessage } from '../src/taskDetailPolicy.js'

assert.equal(
  needsTaskDetail({ status: 3 }),
  true,
  'completed list items need detail because list API omits large result content',
)

assert.equal(
  needsResultDetail({ status: 3 }, 'transcription'),
  true,
  'completed transcription action should fetch detail when transcription content is missing',
)

assert.equal(
  needsResultDetail({ status: 3, transcription: { content: 'done' } }, 'transcription'),
  false,
  'completed transcription action should not refetch detail when transcription content is already present',
)

assert.equal(
  needsResultDetail({ status: 3, transcription: { content: 'done' } }, 'summary'),
  false,
  'completed transcriptions without a summary should allow submitting AI analysis',
)

assert.equal(
  needsResultDetail({ status: 3, summary: { content: 'done' } }, 'summary'),
  true,
  'completed summaries should fetch detail for display instead of submitting duplicate analysis',
)

assert.equal(
  needsTaskDetail({ status: 2 }),
  false,
  'running list items should not fetch detail yet',
)

assert.equal(
  taskFailureMessage({ status: 4, error_msg: '音频过大' }),
  '音频过大',
  'failed tasks should expose backend error messages for display',
)

assert.equal(
  taskFailureMessage({ status: 3, error_msg: 'old error' }),
  '',
  'completed tasks should not expose stale error messages',
)
