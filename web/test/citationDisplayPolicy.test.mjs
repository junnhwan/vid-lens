import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'

import {
  areAllExpandableCitationsExpanded,
  citationDisplayLabel,
  citationDomId,
  citationNeedsExpansion,
  citationPreview,
  citationTextForDisplay,
  setMessageCitationsExpanded,
  stripInternalCitationTokens,
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


assert.equal(
  stripInternalCitationTokens('工具结果会返回给模型[C1]，然后继续推理[C1,C3]。'),
  '工具结果会返回给模型，然后继续推理。',
  'internal citation tokens should be removed from visible prose',
)

assert.equal(
  stripInternalCitationTokens('相邻引用[C1][C2]也不应出现在正文。'),
  '相邻引用也不应出现在正文。',
  'adjacent internal citation tokens should all be removed',
)

assert.equal(
  stripInternalCitationTokens('全角逗号[C1， C3]，顿号[C1、C2]，小写[c2]。'),
  '全角逗号，顿号，小写。',
  'combined citation tokens should support Chinese separators and lowercase markers',
)

assert.equal(
  stripInternalCitationTokens('引用式链接 [C1][source] 应保留。'),
  '引用式链接 [C1][source] 应保留。',
  'reference-style Markdown links should remain unchanged',
)

assert.equal(
  stripInternalCitationTokens('框架是 [Gin]，参见 [C1](https://example.com/c1) 和 [文档](https://example.com)。'),
  '框架是 [Gin]，参见 [C1](https://example.com/c1) 和 [文档](https://example.com)。',
  'ordinary brackets and Markdown links should remain unchanged',
)

const codeRichAnswer = [
  '正文[C1]。',
  '',
  '行内代码 `[C1,C3]` 保留。',
  '',
  '```text',
  '[C1， C3]  code spacing',
  '```',
].join('\n')
assert.equal(
  stripInternalCitationTokens(codeRichAnswer),
  [
    '正文。',
    '',
    '行内代码 `[C1,C3]` 保留。',
    '',
    '```text',
    '[C1， C3]  code spacing',
    '```',
  ].join('\n'),
  'inline and fenced code contents should remain byte-for-byte unchanged',
)

assert.equal(
  citationDisplayLabel(0),
  '[1]',
  'citation labels should expose one-based public indexes',
)
assert.equal(
  citationDomId('message/42', 1),
  'citation-message%2F42-2',
  'citation DOM ids should be stable and safely encode message ids',
)


assert.equal(
  stripInternalCitationTokens(String.raw`转义引用 \[C1] 也不能泄露。`),
  String.raw`转义引用 \[C1] 也不能泄露。`,
  'escaped Markdown brackets should remain literal text',
)

assert.equal(
  stripInternalCitationTokens('未闭合反引号 `正文[C1]。'),
  '未闭合反引号 `正文。',
  'an unclosed inline backtick should not protect later citation tokens',
)

assert.equal(
  stripInternalCitationTokens('转义反引号 \\`正文[C1]\\`。'),
  '转义反引号 \\`正文\\`。',
  'escaped backticks should not turn prose into protected code',
)

const referenceMarkdown = [
  '[C1]: https://example.com/evidence',
  '[来源][C1]',
].join('\n')
assert.equal(
  stripInternalCitationTokens(referenceMarkdown),
  referenceMarkdown,
  'citation-like Markdown reference definitions and target labels should remain intact',
)

const markdownCitationBoundaries = [
  String.raw`转义字面量 \[C1]。`,
  '引用式链接 [C1][source]。',
  '[C1]: https://example.com',
  '引用目标 [来源][C1]。',
  '普通链接 [C1](https://example.com)。',
  '真正内部标记[C1][C2]和[C1,C2]。',
].join('\n')
assert.equal(
  stripInternalCitationTokens(markdownCitationBoundaries),
  [
    String.raw`转义字面量 \[C1]。`,
    '引用式链接 [C1][source]。',
    '[C1]: https://example.com',
    '引用目标 [来源][C1]。',
    '普通链接 [C1](https://example.com)。',
    '真正内部标记和。',
  ].join('\n'),
  'only standalone internal tokens should be removed at Markdown boundaries',
)

const videoRAGChatSource = readFileSync(new URL('../src/components/VideoRAGChat.vue', import.meta.url), 'utf8')
for (const debugLabel of ['RRF:', '向量: #', '关键词: #']) {
  assert.equal(
    videoRAGChatSource.includes(debugLabel),
    false,
    `ordinary citation cards should not render retrieval debug label ${debugLabel}`,
  )
}

const indentedCode = [
  '正文[C1]。',
  '',
  '    const citation = "[C1]"',
  '    // [C1,C2] remains code',
].join('\n')
assert.equal(
  stripInternalCitationTokens(indentedCode),
  [
    '正文。',
    '',
    '    const citation = "[C1]"',
    '    // [C1,C2] remains code',
  ].join('\n'),
  'four-space indented Markdown code blocks should remain unchanged',
)
