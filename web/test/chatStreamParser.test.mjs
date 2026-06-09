import assert from 'node:assert/strict'

global.localStorage = {
  getItem: (key) => (key === 'token' ? 'test-token' : null),
}

const { default: api } = await import('../src/api.js')

const encoder = new TextEncoder()
global.fetch = async (url, options) => {
  assert.equal(url, '/api/v1/chat/sessions/12/messages/stream')
  assert.equal(options.method, 'POST')
  assert.equal(options.headers.Authorization, 'Bearer test-token')

  const body = [
    'event: citations',
    'data: [{"chunk_id":7,"content":"片段内容"}]',
    '',
    'event: answer',
    'data: "第一段"',
    '',
    'event: answer',
    'data: "第二段"',
    '',
    'event: done',
    'data: {"message_id":42,"model":"chat-model"}',
    '',
  ].join('\n')

  return {
    ok: true,
    body: new ReadableStream({
      start(controller) {
        controller.enqueue(encoder.encode(body))
        controller.close()
      },
    }),
  }
}

const events = []
await api.sendChatMessageStream(12, '问问视频内容', 5, (event) => events.push(event))

assert.deepEqual(
  events,
  [
    { type: 'citations', citations: [{ chunk_id: 7, content: '片段内容' }] },
    { type: 'answer', delta: '第一段' },
    { type: 'answer', delta: '第二段' },
    { type: 'done', message_id: 42, model: 'chat-model' },
  ],
  'chat stream parser should combine Gin SSE event names with JSON data payloads',
)
