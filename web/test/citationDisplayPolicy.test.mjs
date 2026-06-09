import assert from 'node:assert/strict'

import {
  areAllExpandableCitationsExpanded,
  citationNeedsExpansion,
  citationPreview,
  citationTextForDisplay,
  setMessageCitationsExpanded,
} from '../src/citationDisplayPolicy.js'

const shortContent = '这是一段较短的参考片段。'
const longContent = '第一段内容'.repeat(80)
const multiLineContent = ['第一行', '第二行', '第三行', '第四行'].join('\n')

assert.equal(
  citationNeedsExpansion(shortContent),
  false,
  'short citations should not show expand controls',
)

assert.equal(
  citationNeedsExpansion(longContent, { maxChars: 120 }),
  true,
  'long citations should show expand controls',
)

assert.equal(
  citationNeedsExpansion(multiLineContent, { maxLines: 3 }),
  true,
  'citations with too many lines should show expand controls',
)

assert.equal(
  citationPreview(longContent, { maxChars: 12 }),
  '第一段内容第一段内容第一...',
  'long citation previews should be truncated with a stable suffix',
)

assert.equal(
  citationPreview(multiLineContent, { maxLines: 2 }),
  '第一行\n第二行...',
  'multi-line citation previews should be limited by line count first',
)

assert.equal(
  citationTextForDisplay(longContent, true, { maxChars: 12 }),
  longContent,
  'expanded citations should display the full original content',
)

assert.equal(
  citationTextForDisplay(longContent, false, { maxChars: 12 }),
  '第一段内容第一段内容第一...',
  'collapsed citations should display a preview',
)

const citations = [
  { chunk_id: 1, content: shortContent },
  { chunk_id: 2, content: longContent },
  { chunk_id: 3, content: multiLineContent },
]

const collapsedKeys = new Set()
assert.equal(
  areAllExpandableCitationsExpanded(collapsedKeys, 'msg-1', citations, { maxChars: 120, maxLines: 3 }),
  false,
  'collapsed messages with expandable citations should not report all expanded',
)

const expandedKeys = setMessageCitationsExpanded(
  collapsedKeys,
  'msg-1',
  citations,
  true,
  { maxChars: 120, maxLines: 3 },
)
assert.equal(
  expandedKeys.has('msg-1:1'),
  true,
  'expand-all should include long citation keys',
)
assert.equal(
  expandedKeys.has('msg-1:2'),
  true,
  'expand-all should include multi-line citation keys',
)
assert.equal(
  expandedKeys.has('msg-1:0'),
  false,
  'expand-all should ignore citations that do not need expansion',
)
assert.equal(
  areAllExpandableCitationsExpanded(expandedKeys, 'msg-1', citations, { maxChars: 120, maxLines: 3 }),
  true,
  'expanded messages should report all expandable citations expanded',
)

const resetKeys = setMessageCitationsExpanded(
  expandedKeys,
  'msg-1',
  citations,
  false,
  { maxChars: 120, maxLines: 3 },
)
assert.equal(
  resetKeys.has('msg-1:1'),
  false,
  'collapse-all should remove expanded citation keys',
)
