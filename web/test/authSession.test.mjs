import assert from 'node:assert/strict'

import { buildStoredUser, getStoredAuthToken } from '../src/authSession.js'

function createStorage(initial = {}) {
  const state = new Map(Object.entries(initial))
  return {
    getItem: (key) => state.get(key) ?? null,
    setItem: (key, value) => state.set(key, value),
  }
}

assert.deepEqual(
  buildStoredUser({ id: 1, username: 'alice' }, 'jwt-token'),
  { id: 1, username: 'alice', token: 'jwt-token' },
  'stored user should include token so page reloads can keep authenticated API access',
)

assert.equal(
  getStoredAuthToken(createStorage({ user: JSON.stringify({ username: 'alice', token: 'nested-token' }) })),
  'nested-token',
  'request interceptor should read token from the stored user session',
)

assert.equal(
  getStoredAuthToken(createStorage({ token: 'direct-token' })),
  'direct-token',
  'request interceptor should remain compatible with the existing standalone token key',
)
