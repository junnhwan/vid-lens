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
  })

  it('marks index complete only when the server reports a finished RAG job', () => {
    const beats = processBeats({ status: 3, stage: 'none', has_transcription: true, last_job_type: 'rag_index' })
    assert.deepEqual(beats.map(b => b.state), ['done', 'done', 'done', 'done'])
    const withoutIndex = processBeats({ status: 3, stage: 'none', has_transcription: true, last_job_type: 'analyze' })
    assert.equal(withoutIndex[3].state, 'queued')
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
