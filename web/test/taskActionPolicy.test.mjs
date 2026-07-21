import assert from 'node:assert/strict'

import {
  TASK_STATUS,
  hasSummaryResult,
  hasTranscriptionResult,
  isPrimaryAnalyzeDisabled,
  isPrimaryTranscribeDisabled,
  isTaskActionDisabled,
  primaryAnalyzeLabel,
  primaryTranscribeLabel,
} from '../src/taskActionPolicy.js'

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

assert.equal(
  hasTranscriptionResult({ transcription: { content: 'hello' } }),
  true,
  'detail payload with content counts as done',
)

assert.equal(
  hasTranscriptionResult({ has_transcription: true }),
  true,
  'list flag counts as done without loading full content',
)

assert.equal(
  hasTranscriptionResult({ hasTranscription: true }),
  true,
  'camelCase flag is accepted',
)

assert.equal(
  hasTranscriptionResult({ has_transcription: false, transcription: {} }),
  false,
  'empty transcription object is not a result',
)

assert.equal(
  hasSummaryResult({ summary: { content: 'sum' } }),
  true,
)

assert.equal(
  hasSummaryResult({ has_summary: true }),
  true,
)

assert.equal(
  isPrimaryTranscribeDisabled({ status: TASK_STATUS.PENDING, has_transcription: true }),
  true,
  'primary extract should grey out when result already exists',
)

assert.equal(
  isPrimaryTranscribeDisabled({ status: TASK_STATUS.PENDING }),
  false,
  'primary extract stays active when no result yet',
)

assert.equal(
  isPrimaryAnalyzeDisabled({ status: TASK_STATUS.COMPLETED, has_summary: true }),
  true,
  'primary summarize should grey out when result already exists',
)

assert.equal(
  isPrimaryAnalyzeDisabled({ status: TASK_STATUS.COMPLETED, has_transcription: true }),
  false,
  'primary summarize stays active when only transcription exists',
)

assert.equal(primaryTranscribeLabel({ has_transcription: true }), '已提取')
assert.equal(primaryTranscribeLabel({}), '提取文字')
assert.equal(primaryAnalyzeLabel({ has_summary: true }), '已总结')
assert.equal(primaryAnalyzeLabel({}), 'AI 总结')
