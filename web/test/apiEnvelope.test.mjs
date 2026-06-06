import assert from 'node:assert/strict'

import { unwrapApiResponse } from '../src/apiEnvelope.js'

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
