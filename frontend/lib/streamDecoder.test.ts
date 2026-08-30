import assert from 'node:assert/strict'
import test from 'node:test'

import { SSEStreamDecoder } from './streamDecoder.ts'

const bytes = (value: string) => new TextEncoder().encode(value)

test('SSEStreamDecoder preserves event order across arbitrary chunk boundaries', () => {
  const decoder = new SSEStreamDecoder()

  const first = decoder.push(bytes('event: run_start\r\ndata: {"run_id":"r1"}\r'))
  const second = decoder.push(bytes('\n\r\nevent: answer\ndata: "你'))
  const third = decoder.push(bytes('好"\n\n'))

  assert.deepEqual(first, [])
  assert.deepEqual(second, [{ event: 'run_start', data: { run_id: 'r1' } }])
  assert.deepEqual(third, [{ event: 'answer', data: '你好' }])
})

test('SSEStreamDecoder flushes a final event without a trailing blank line', () => {
  const decoder = new SSEStreamDecoder()
  decoder.push(bytes('event: done\ndata: {"message_id":7}'))

  assert.deepEqual(decoder.finish(), [{ event: 'done', data: { message_id: 7 } }])
})

test('SSEStreamDecoder keeps malformed JSON as a safe raw payload', () => {
  const decoder = new SSEStreamDecoder()

  assert.deepEqual(
    decoder.push(bytes('event: error\ndata: not-json\n\n')),
    [{ event: 'error', data: 'not-json' }],
  )
})
