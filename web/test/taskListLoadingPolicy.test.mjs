import assert from 'node:assert/strict'

import { shouldShowInitialTaskSkeleton } from '../src/taskListLoadingPolicy.js'

assert.equal(
  shouldShowInitialTaskSkeleton([], false),
  false,
  'empty task lists should show the empty state after loading has finished',
)

assert.equal(
  shouldShowInitialTaskSkeleton([], { 1: true }),
  false,
  'per-task action loading maps must not be treated as initial list loading',
)

assert.equal(
  shouldShowInitialTaskSkeleton([], true),
  true,
  'initial list loading should show skeletons while the first request is in flight',
)

assert.equal(
  shouldShowInitialTaskSkeleton([{ id: 1 }], true),
  false,
  'existing task lists should stay visible during refreshes',
)
