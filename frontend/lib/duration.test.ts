import assert from 'node:assert/strict'
import { test } from 'node:test'
import { formatDuration } from './duration.ts'

test('duration uses integer milliseconds below one second and integer seconds above', () => {
  assert.equal(formatDuration(637.4), '637ms')
  assert.equal(formatDuration(999.9), '1000ms')
  assert.equal(formatDuration(1000), '1s')
  assert.equal(formatDuration(69900), '70s')
})
