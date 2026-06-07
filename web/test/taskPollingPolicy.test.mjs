import assert from 'node:assert/strict'

import { isPollingSuccessful, shouldStopPolling } from '../src/taskPollingPolicy.js'

assert.equal(
  shouldStopPolling({ status: 3 }, 'transcription'),
  true,
  'completed list items should stop transcription polling even when list API omits transcription content',
)

assert.equal(
  shouldStopPolling({ status: 3 }, 'summary'),
  true,
  'completed list items should stop summary polling even when list API omits summary content',
)

assert.equal(
  shouldStopPolling({ status: 4 }, 'transcription'),
  true,
  'failed tasks should stop polling and let the UI show the final status',
)

assert.equal(
  shouldStopPolling({ status: 2 }, 'transcription'),
  false,
  'running tasks should continue polling',
)

assert.equal(
  shouldStopPolling({ status: 2, stage: 'downloading' }, 'download'),
  false,
  'URL download polling should continue while the task is downloading',
)

assert.equal(
  shouldStopPolling({ status: 0, stage: 'uploaded' }, 'download'),
  true,
  'URL download polling should stop when the downloaded video is uploaded',
)

assert.equal(
  isPollingSuccessful({ status: 0, stage: 'uploaded' }, 'download'),
  true,
  'URL download polling should treat uploaded pending tasks as successful',
)

assert.equal(
  isPollingSuccessful({ status: 4, stage: 'downloading' }, 'download'),
  false,
  'failed URL download polling should not be treated as successful',
)
