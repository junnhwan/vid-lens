import assert from 'node:assert/strict'

import {
  normalizeChatMessages,
  parseRetrievalSnapshot,
  parseRetrievalSnapshotDetail,
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
  parseRetrievalSnapshot(
    JSON.stringify({
      template: 'direct_qa',
      citations: [{ chunk_id: 1, content: '命中片段' }],
      trace: [{ tool: 'search_transcript', name: 'search topic' }],
    }),
  ),
  [{ chunk_id: 1, content: '命中片段' }],
  'agentic retrieval_snapshot object should still expose its citations array',
)

assert.deepEqual(
  parseRetrievalSnapshotDetail(
    JSON.stringify({
      template: 'compare_topics',
      citations: [{ chunk_id: 5, content: '片段A' }],
      trace: [{ tool: 'compare_segments', name: 'compare' }],
    }),
  ),
  {
    template: 'compare_topics',
    citations: [{ chunk_id: 5, content: '片段A' }],
    trace: [{ tool: 'compare_segments', name: 'compare' }],
    mode: 'agent',
  },
  'agentic retrieval_snapshot should keep template / trace alongside citations',
)

assert.deepEqual(
  parseRetrievalSnapshotDetail('[{"chunk_id":8,"content":"参考片段"}]'),
  { template: null, citations: [{ chunk_id: 8, content: '参考片段' }], trace: [], mode: 'rag' },
  'plain RAG retrieval_snapshot should normalize into rag mode with empty trace',
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
      template: null,
      trace: [],
      model: null,
      mode: 'rag',
    },
    {
      id: 2,
      role: 'assistant',
      content: '历史回答',
      citations: [{ chunk_id: 8, content: '参考片段' }],
      timestamp: '2026-06-09T11:40:26Z',
      template: null,
      trace: [],
      model: null,
      mode: 'rag',
    },
  ],
  'backend chat messages should be normalized into the UI message shape',
)

assert.deepEqual(
  normalizeChatMessages([
    {
      id: 3,
      role: 'assistant',
      content: 'Agentic 回答',
      model: 'gpt-4o-mini',
      created_at: '2026-06-28T10:00:00Z',
      retrieval_snapshot: {
        template: 'compare_topics',
        citations: [{ chunk_id: 5, content: '片段A', source: 'hybrid' }],
        trace: [
          {
            name: 'search topic',
            tool: 'search_transcript',
            input: { question: '比较两者', top_k: 5 },
            output_ref: 'citations:5',
            error: '',
          },
        ],
      },
    },
  ]),
  [
    {
      id: 3,
      role: 'assistant',
      content: 'Agentic 回答',
      citations: [{ chunk_id: 5, content: '片段A', source: 'hybrid' }],
      timestamp: '2026-06-28T10:00:00Z',
      template: 'compare_topics',
      trace: [
        {
          name: 'search topic',
          tool: 'search_transcript',
          input: { question: '比较两者', top_k: 5 },
          output_ref: 'citations:5',
          error: '',
        },
      ],
      model: 'gpt-4o-mini',
      mode: 'agent',
    },
  ],
  'agentic chat history should preserve template / trace / model and resolve agent mode',
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
