import assert from 'node:assert/strict'

import {
  DEFAULT_SUMMARY_PREVIEW_OPTIONS,
  DEFAULT_TRANSCRIPTION_PREVIEW_OPTIONS,
  taskResultExpandButtonLabel,
  taskResultExpandMeta,
  taskResultNeedsExpansion,
  taskResultTextForDisplay,
} from '../src/taskResultDisplayPolicy.js'

const shortContent = '这是一段较短的结果。'
// 需明显超过 DEFAULT maxChars(1800) / maxLines，才应触发展开
const longSingleLine = '这是一段很长的识别文本'.repeat(200)
const longTranscriptionLines = Array.from({ length: 20 }, (_, index) => `转写第${index + 1}行`).join('\n')
const longSummaryLines = Array.from({ length: 18 }, (_, index) => `总结第${index + 1}行`).join('\n')

assert.equal(
  taskResultNeedsExpansion(shortContent, DEFAULT_TRANSCRIPTION_PREVIEW_OPTIONS),
  false,
  'short transcription content should not need expansion',
)

assert.equal(
  taskResultNeedsExpansion(longSingleLine, DEFAULT_TRANSCRIPTION_PREVIEW_OPTIONS),
  true,
  'long single-line transcription content should need expansion',
)

assert.equal(
  taskResultTextForDisplay(longSingleLine, false, { maxChars: 15, maxLines: 10 }),
  '这是一段很长的识别文本这是一段...',
  'collapsed long transcription content should be truncated by characters with a suffix',
)

assert.equal(
  taskResultNeedsExpansion(longTranscriptionLines, DEFAULT_TRANSCRIPTION_PREVIEW_OPTIONS),
  true,
  'transcription content over the preview line limit should need expansion',
)

assert.equal(
  taskResultTextForDisplay(longTranscriptionLines, false, { maxLines: 3, maxChars: 2000 }),
  '转写第1行\n转写第2行\n转写第3行...',
  'collapsed transcription content should be truncated by line count first',
)

assert.equal(
  taskResultNeedsExpansion(longSummaryLines, DEFAULT_SUMMARY_PREVIEW_OPTIONS),
  true,
  'summary content over the preview line limit should need expansion',
)

assert.equal(
  taskResultTextForDisplay(longSummaryLines, true, DEFAULT_SUMMARY_PREVIEW_OPTIONS),
  longSummaryLines,
  'expanded task result content should display the full original content',
)

const meta = taskResultExpandMeta(longTranscriptionLines)
assert.equal(meta.lines, 20, 'expand meta should count lines')
assert.ok(meta.chars > 0, 'expand meta should count characters')
assert.match(meta.label, /字/, 'expand meta label should mention characters')

assert.equal(
  taskResultExpandButtonLabel(true, longTranscriptionLines),
  '收起',
  'expanded button label is collapse',
)

assert.match(
  taskResultExpandButtonLabel(false, longTranscriptionLines),
  /^展开全部 · /,
  'collapsed button label includes full length hint',
)
