import assert from 'node:assert/strict'

import { shouldResetSessionOnUnauthorized } from '../src/authErrorPolicy.js'

assert.equal(
  shouldResetSessionOnUnauthorized('/user/login'),
  false,
  'login failures must be shown in the modal instead of reloading the page',
)

assert.equal(
  shouldResetSessionOnUnauthorized('/user/register'),
  false,
  'register failures must be shown in the modal instead of reloading the page',
)

assert.equal(
  shouldResetSessionOnUnauthorized('/media/list'),
  true,
  'protected API failures should still clear the stale session',
)

assert.equal(
  shouldResetSessionOnUnauthorized('http://localhost:8080/api/v1/media/list?page=1'),
  true,
  'absolute protected API URLs should still clear the stale session',
)
