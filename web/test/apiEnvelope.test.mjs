import assert from 'node:assert/strict'

import { normalizeListResponse, unwrapApiResponse } from '../src/apiEnvelope.js'

const authPayload = {
  code: 200,
  message: 'success',
  data: {
    user: { id: 1, username: 'alice' },
    token: 'jwt-token',
  },
}

assert.deepEqual(
  unwrapApiResponse({ data: authPayload }),
  authPayload.data,
  'API wrapper should expose the backend response data payload directly',
)

assert.equal(
  unwrapApiResponse({ data: { code: 200, message: 'success' } }),
  undefined,
  'API wrapper should return undefined when a success envelope has no data payload',
)

assert.deepEqual(
  normalizeListResponse([{ id: 1, name: 'default profile' }]),
  [{ id: 1, name: 'default profile' }],
  'list normalizer should keep array payloads returned by non-paginated APIs',
)

assert.deepEqual(
  normalizeListResponse({ list: [{ id: 2, name: 'task' }] }),
  [{ id: 2, name: 'task' }],
  'list normalizer should remain compatible with paginated list payloads',
)

assert.deepEqual(
  normalizeListResponse(null),
  [],
  'list normalizer should return an empty list for empty payloads',
)
