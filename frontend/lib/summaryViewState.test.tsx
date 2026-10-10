import { afterEach, beforeEach, expect, test, vi } from 'vitest'
import type { SummaryContextRef } from './summaryExperience'
import { clearSummaryViewState, getSummarySessionView, getSummaryTaskView, patchSummarySessionView, patchSummaryTaskView, promoteSummarySessionView, setSummaryViewOwner } from './summaryViewState'

const ref: SummaryContextRef = { kind: 'summary_selection', task_id: 42, version_ref: { generated_version: 1 }, document_digest: 'body', block_id: 'chapter', block_digest: 'block', text_start: 0, text_end: 2, quote: '选段' }
beforeEach(() => { clearSummaryViewState(); setSummaryViewOwner(7) })
afterEach(() => { clearSummaryViewState(); vi.useRealTimers() })
test('views are scoped by task and session; only creation promotes an unsent draft', () => {
  patchSummarySessionView(7, 42, null, { draft: '新问题', contexts: [ref] })
  promoteSummarySessionView(7, 42, 9)
  expect(getSummarySessionView(7, 42, 9)).toMatchObject({ draft: '新问题', contexts: [ref] })
  expect(getSummarySessionView(7, 42, null).contexts).toEqual([])
  patchSummarySessionView(7, 42, 10, { draft: '另一会话', messageAnchor: { messageID: 101, offset: 30, scrollTop: 200 } })
  patchSummaryTaskView(7, 42, { sessionID: 10, reader: { scrollTop: 420, contentDigest: 'body' } })
  expect(getSummarySessionView(7, 42, 10).contexts).toEqual([])
  expect(getSummarySessionView(7, 43, 9).draft).toBe('')
  expect(getSummarySessionView(7, 42, 9).contexts).toEqual([ref])
  expect(getSummaryTaskView(7, 42).reader?.scrollTop).toBe(420)
})
test('account changes and logout erase all temporary state and reject stale writes', () => {
  patchSummarySessionView(7, 42, 9, { draft: '私有草稿', contexts: [ref] })
  setSummaryViewOwner(8)
  patchSummarySessionView(7, 42, 9, { draft: '迟到请求' })
  expect(getSummarySessionView(8, 42, 9).draft).toBe('')
  setSummaryViewOwner(7)
  expect(getSummarySessionView(7, 42, 9).draft).toBe('')
  patchSummarySessionView(7, 42, 9, { draft: '注销前' })
  clearSummaryViewState()
  patchSummarySessionView(7, 42, 9, { draft: '注销后迟到请求' })
  setSummaryViewOwner(7)
  expect(getSummarySessionView(7, 42, 9).draft).toBe('')
})
test('memory limits and expiry bound retained data; invalid cross-task refs are rejected', () => {
  for (let id = 1; id <= 41; id++) patchSummarySessionView(7, 42, id, { draft: `draft-${id}` })
  expect(getSummarySessionView(7, 42, 1).draft).toBe('')
  expect(getSummarySessionView(7, 42, 41).draft).toBe('draft-41')
  patchSummarySessionView(7, 43, 9, { contexts: [ref] })
  expect(getSummarySessionView(7, 43, 9).contexts).toEqual([])
  patchSummarySessionView(7, 42, 41, { contexts: Array(4).fill(ref) })
  expect(getSummarySessionView(7, 42, 41).contexts).toEqual([])
  vi.useFakeTimers(); vi.setSystemTime(Date.now() + 7 * 60 * 60 * 1000)
  expect(getSummarySessionView(7, 42, 41).draft).toBe('')
})
test('caller mutations cannot rewrite a retained quote or its accepted version', () => {
  const incoming = { ...ref, version_ref: { generated_version: 1 } }
  patchSummarySessionView(7, 42, 9, { contexts: [incoming] })
  incoming.quote = '已变化'; incoming.version_ref.generated_version = 99
  expect(getSummarySessionView(7, 42, 9).contexts[0]).toEqual(ref)
  expect(() => getSummarySessionView(7, 42, null).contexts.push(ref)).toThrow()
})
