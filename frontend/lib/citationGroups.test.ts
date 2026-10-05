import assert from 'node:assert/strict'
import { test } from 'node:test'
import { groupCitationSources } from './citationGroups.ts'
import type { CiteRef } from '../components/Citation'

const source: CiteRef = { id: 'C1', taskId: 2, chunkIndex: 0, score: 1, content: '原句一。', startMS: 595000, endMS: 905000, timeRangeStatus: 'coarse', modality: 'transcript' }

test('seven claims from one observed window remain seven quotes in one playback source', () => {
  const cites = Array.from({ length: 7 }, (_, i) => ({ ...source, id: `C${i + 1}`, chunkIndex: i, content: `原句${i + 1}。` }))
  const groups = groupCitationSources(cites)
  assert.equal(groups.length, 1)
  assert.deepEqual(groups[0].citations, cites)
  assert.equal(cites.length, 7)
})

test('video, modality, exact interval and timing certainty separate playback sources', () => {
  assert.equal(groupCitationSources([
    source,
    { ...source, id: 'C2', taskId: 3 },
    { ...source, id: 'C3', startMS: 600000 },
    { ...source, id: 'C4', modality: 'visual_ocr' },
    { ...source, id: 'C5', timeRangeStatus: 'exact' },
  ]).length, 5)
})

test('unknown, malformed or unidentified sources never collapse because their times look the same', () => {
  for (const variation of [{ timeRangeStatus: 'unknown' }, { startMS: -1 }, { endMS: 0 }, { taskId: undefined, videoTitle: undefined }, { taskId: undefined, videoTitle: '同名视频' }]) {
    const a = { ...source, ...variation }
    assert.equal(groupCitationSources([a, { ...a, id: 'C2' }]).length, 2)
  }
  assert.equal(groupCitationSources([{ ...source, taskId: undefined }, { ...source, id: 'C2', taskId: undefined }], 2).length, 1)
})
