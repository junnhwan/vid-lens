import assert from 'node:assert/strict'

import {
  normalizeChatMessages,
  parseRetrievalSnapshot,
  resolveReusableChatSession,
} from '../src/chatHistoryPolicy.js'

assert.deepEqual(
  parseRetrievalSnapshot('[{"chunk_id":7,"content":"片段内容"}]'),
  [{ chunk_id: 7, content: '片段内容' }],
  'stored assistant retrieval_snapshot should be parsed back into citations',
)

assert.deepEqual(
  parseRetrievalSnapshot('not valid json'),
  [],
  'invalid retrieval_snapshot should not break chat history rendering',
)

assert.deepEqual(
  normalizeChatMessages([
    { id: 1, role: 'user', content: '刚刚问的问题', created_at: '2026-06-09T11:40:25Z' },
    {
      id: 2,
      role: 'assistant',
      content: '历史回答',
      retrieval_snapshot: '[{"chunk_id":8,"content":"参考片段"}]',
      created_at: '2026-06-09T11:40:26Z',
    },
  ]),
  [
    {
      id: 1,
      role: 'user',
      content: '刚刚问的问题',
      citations: [],
      timestamp: '2026-06-09T11:40:25Z',
    },
    {
      id: 2,
      role: 'assistant',
      content: '历史回答',
      citations: [{ chunk_id: 8, content: '参考片段' }],
      timestamp: '2026-06-09T11:40:26Z',
    },
  ],
  'backend chat messages should be normalized into the UI message shape',
)

const loadedSessionIDs = []
const reusable = await resolveReusableChatSession(
  [{ id: 11 }, { id: 10 }],
  async (sessionId) => {
    loadedSessionIDs.push(sessionId)
    return sessionId === 10 ? [{ id: 3, role: 'assistant', content: 'old answer' }] : []
  },
)

assert.deepEqual(
  reusable,
  { session: { id: 10 }, messages: [{ id: 3, role: 'assistant', content: 'old answer' }] },
  'chat should reopen the latest session that actually has history instead of a newer empty session',
)

assert.deepEqual(
  loadedSessionIDs,
  [11, 10],
  'session resolver should inspect newer empty sessions before falling back to older non-empty history',
)

assert.deepEqual(
  await resolveReusableChatSession([{ id: 12 }], async () => []),
  { session: { id: 12 }, messages: [] },
  'when all sessions are empty, chat should reuse the newest empty session instead of creating more empty sessions',
)
