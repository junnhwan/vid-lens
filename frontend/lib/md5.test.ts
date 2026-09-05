import { test } from 'node:test'
import assert from 'node:assert/strict'
import { createHash } from 'node:crypto'
import { MD5, md5Hex } from './md5.ts'

const enc = (s: string) => new TextEncoder().encode(s)
const nodeMd5 = (bytes: Uint8Array) => createHash('md5').update(bytes).digest('hex')

test('md5 matches RFC 1321 / node crypto vectors', () => {
  const vectors = [
    '',
    'abc',
    'The quick brown fox jumps over the lazy dog',
    'message digest',
    'a'.repeat(1000),
    '映知视频理解', // 多字节 UTF-8
  ]
  for (const v of vectors) {
    const bytes = enc(v)
    const expected = nodeMd5(bytes)
    assert.equal(md5Hex(bytes), expected, `md5(${v.slice(0, 20)}…)`)
  }
})

test('md5 incremental updates equal single-shot', () => {
  const bytes = enc('The quick brown fox jumps over the lazy dog — 映知,可回放的视频问答 0123456789')
  const expected = nodeMd5(bytes)
  for (const step of [1, 3, 7, 63, 64, 65, 200]) {
    const hasher = new MD5()
    for (let i = 0; i < bytes.length; i += step) hasher.update(bytes.subarray(i, Math.min(i + step, bytes.length)))
    assert.equal(hasher.digestHex(), expected, `step=${step}`)
  }
})
