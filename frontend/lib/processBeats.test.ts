import assert from 'node:assert/strict'
import { describe, it } from 'node:test'
import { processBeats } from './processBeats.ts'

describe('processBeats', () => {
  it('marks ingest running while downloading', () => {
    const beats = processBeats({ status: 2, stage: 'downloading', has_transcription: false })
    assert.equal(beats[0].state, 'running')
    assert.equal(beats[1].state, 'queued')
  })

  it('lights asr while transcribing', () => {
    const beats = processBeats({ status: 2, stage: 'transcribing', has_transcription: false })
    assert.equal(beats[0].state, 'done')
    assert.equal(beats[1].state, 'running')
    assert.equal(beats[2].state, 'queued')
  })

  it('keeps queued work still', () => {
    const beats = processBeats({ status: 1, stage: 'indexing', has_transcription: true })
    assert.equal(beats[3].state, 'queued')
  })

  it('advances visual after transcription exists', () => {
    const beats = processBeats({ status: 2, stage: 'visual_indexing', has_transcription: true })
    assert.equal(beats[1].state, 'done')
    assert.equal(beats[2].state, 'running')
    const skipped = processBeats({ status: 2, stage: 'transcribing', has_transcription: true, visual_status: 'skipped' })
    assert.equal(skipped[2].state, 'skipped')
  })

  it('uses published index and visual records for a completed task', () => {
    const beats = processBeats({ status: 3, stage: 'none', has_transcription: true, has_rag_index: true, visual_status: 'completed' })
    assert.deepEqual(beats.map(b => b.state), ['done', 'done', 'done', 'done'])
    const dedup = processBeats({ status: 3, stage: 'none', has_transcription: true, has_rag_index: true, visual_status: '' })
    assert.deepEqual(dedup.map(b => b.state), ['done', 'done', 'skipped', 'done'])
  })

  it('does not call summary generation an active index build', () => {
    const beats = processBeats({ status: 2, stage: 'summarizing', has_transcription: true })
    assert.equal(beats[3].state, 'queued')
  })

  it('flags the failed beat and leaves later ones queued', () => {
    const beats = processBeats({ status: 4, stage: 'transcribing', has_transcription: false })
    assert.equal(beats[0].state, 'done')
    assert.equal(beats[1].state, 'error')
    assert.equal(beats[2].state, 'queued')
  })
})
