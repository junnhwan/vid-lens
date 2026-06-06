import assert from 'node:assert/strict'

import { shouldStopPolling } from '../src/taskPollingPolicy.js'

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
