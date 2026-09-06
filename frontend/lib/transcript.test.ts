import assert from 'node:assert/strict'
import { describe, it } from 'node:test'
import { expandTranscript, splitForReading } from './transcript.ts'

describe('splitForReading', () => {
  it('keeps short text intact', () => {
    assert.deepEqual(splitForReading('很短的一句。'), ['很短的一句。'])
  })

  it('breaks a long blob on sentence ends', () => {
    const blob = Array.from({ length: 8 }, (_, i) => `这是第${i + 1}句,用来把一整段转写拆成可以阅读的小块,避免糊成一坨。`).join('')
    const parts = splitForReading(blob)
    assert.ok(parts.length >= 2)
    assert.equal(parts.join(''), blob.replace(/\s+/g, ' ').trim())
  })
})

describe('expandTranscript', () => {
  it('interpolates timestamps across split chunks', () => {
    const content = Array.from({ length: 8 }, (_, i) => `第${i + 1}段说明一个完整的意思,并且足够长所以会被切开。`).join('')
    const rows = expandTranscript([{
      id: 'a',
      start_ms: 0,
      end_ms: 10000,
      content,
    }])
    assert.ok(rows.length >= 2)
    assert.equal(rows[0].start_ms, 0)
    assert.ok(rows[rows.length - 1].end_ms >= 9000)
  })
})
