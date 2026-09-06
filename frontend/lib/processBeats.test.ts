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

  it('advances visual after transcription exists', () => {
    const beats = processBeats({ status: 2, stage: 'visual_indexing', has_transcription: true })
    assert.equal(beats[1].state, 'done')
    assert.equal(beats[2].state, 'running')
  })

  it('completes all beats on a finished transcripted task', () => {
    const beats = processBeats({ status: 3, stage: 'indexing', has_transcription: true })
    assert.deepEqual(beats.map(b => b.state), ['done', 'done', 'done', 'done'])
  })

  it('flags the failed beat and leaves later ones queued', () => {
    const beats = processBeats({ status: 4, stage: 'transcribing', has_transcription: false })
    assert.equal(beats[0].state, 'done')
    assert.equal(beats[1].state, 'error')
    assert.equal(beats[2].state, 'queued')
  })
})
