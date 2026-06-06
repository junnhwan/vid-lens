import assert from 'node:assert/strict'

import { isTaskActionDisabled, TASK_STATUS } from '../src/taskActionPolicy.js'

assert.equal(
  isTaskActionDisabled({ status: TASK_STATUS.PENDING }),
  false,
  'newly uploaded pending tasks should allow starting transcription or analysis',
)

assert.equal(
  isTaskActionDisabled({ status: TASK_STATUS.FAILED }),
  false,
  'failed tasks should allow retrying transcription or analysis',
)

assert.equal(
  isTaskActionDisabled({ status: TASK_STATUS.COMPLETED }),
  false,
  'completed tasks should allow opening existing results or requesting the other result type',
)

assert.equal(
  isTaskActionDisabled({ status: TASK_STATUS.QUEUED }),
  true,
  'queued tasks should not allow duplicate submissions',
)

assert.equal(
  isTaskActionDisabled({ status: TASK_STATUS.RUNNING }),
  true,
  'running tasks should not allow duplicate submissions',
)

assert.equal(
  isTaskActionDisabled({ status: TASK_STATUS.PENDING }, true),
  true,
  'locally loading tasks should not allow duplicate clicks',
)
