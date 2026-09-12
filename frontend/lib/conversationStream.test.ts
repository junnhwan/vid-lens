import assert from 'node:assert/strict'
import test from 'node:test'
import { readConversationStream } from './conversationStream.ts'

const encode = (event: string, data: unknown) => new TextEncoder().encode(`event: ${event}\ndata: ${JSON.stringify(data)}\n\n`)

test('delivers answer while upstream remains open; done is terminal', async () => {
  let source!: ReadableStreamDefaultController<Uint8Array>
  const body = new ReadableStream<Uint8Array>({ start(controller) { source = controller } })
  const seen: string[] = []
  let first!: () => void
  const firstArrived = new Promise<void>(resolve => { first = resolve })
  const consuming = readConversationStream(body, (event) => { seen.push(event); if (event === 'answer') first() })
  source.enqueue(encode('answer', '第一段'))
  await Promise.race([firstArrived, new Promise((_, reject) => setTimeout(() => reject(Error('first delta buffered')), 500))])
  assert.deepEqual(seen, ['answer'])
  source.enqueue(encode('done', { answer: '最终回答' }))
  await consuming
  assert.deepEqual(seen, ['answer', 'done'])
})

test('EOF without a business terminal reports interruption after partial text', async () => {
  const body = new ReadableStream<Uint8Array>({ start(c) { c.enqueue(encode('answer', '一部分')); c.close() } })
  const events: string[] = []
  await readConversationStream(body, event => events.push(event))
  assert.deepEqual(events, ['answer', 'error'])
})

test('cancellation releases a blocked reader without a false failure', async () => {
  const abort = new AbortController()
  let cancelled = false
  const body = new ReadableStream<Uint8Array>({ cancel() { cancelled = true } })
  const events: string[] = []
  const reading = readConversationStream(body, event => events.push(event), abort.signal)
  abort.abort()
  await reading
  assert.equal(cancelled, true)
  assert.deepEqual(events, [])
})
