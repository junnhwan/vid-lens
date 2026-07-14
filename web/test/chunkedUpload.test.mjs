import assert from 'node:assert/strict'

import { uploadFileInChunks } from '../src/chunkedUpload.js'

function makeFile(size, name = 'demo.mp4') {
  const file = new Blob([new Uint8Array(size)], { type: 'video/mp4' })
  Object.defineProperty(file, 'name', { value: name })
  return file
}

{
  const calls = []
  const progress = []
  const api = {
    checkUpload: async (md5) => {
      calls.push(['check', md5])
      return { status: 'uploading', uploaded: [1] }
    },
    uploadChunk: async (md5, index, chunk, onProgress) => {
      calls.push(['chunk', md5, index, chunk.size])
      onProgress?.({ loaded: chunk.size, total: chunk.size })
    },
    mergeChunks: async (...args) => {
      calls.push(['merge', ...args])
      return { task_id: 9 }
    },
  }

  const result = await uploadFileInChunks({
    file: makeFile(11),
    api,
    chunkSize: 5,
    calculateMD5: async () => '0123456789abcdef0123456789abcdef',
    onProgress: (event) => progress.push(event),
  })

  assert.deepEqual(
    calls.filter(([type]) => type === 'chunk'),
    [
      ['chunk', '0123456789abcdef0123456789abcdef', 0, 5],
      ['chunk', '0123456789abcdef0123456789abcdef', 2, 1],
    ],
    'already uploaded chunks should be skipped while preserving chunk boundaries',
  )
  assert.deepEqual(
    calls.at(-1),
    ['merge', '0123456789abcdef0123456789abcdef', 'demo.mp4', 3],
    'all chunks should be merged with the file identity and total chunk count',
  )
  assert.deepEqual(result, { task_id: 9 })
  assert.equal(progress.at(-1).stage, 'completed')
  assert.equal(progress.at(-1).percent, 100)
  const percents = progress.map((event) => event.percent)
  assert.ok(percents.every((value, index) => index === 0 || value >= percents[index - 1]), 'progress must be monotonic')
}

{
  const calls = []
  const api = {
    checkUpload: async () => ({ status: 'completed', uploaded: [] }),
    uploadChunk: async () => calls.push('unexpected chunk'),
    mergeChunks: async (...args) => {
      calls.push(['merge', ...args])
      return { task_id: 10 }
    },
  }

  await uploadFileInChunks({
    file: makeFile(3, 'existing.mp4'),
    api,
    chunkSize: 5,
    calculateMD5: async () => 'abcdef0123456789abcdef0123456789',
  })

  assert.deepEqual(calls, [['merge', 'abcdef0123456789abcdef0123456789', 'existing.mp4', 1]])
}

{
  const api = {
    checkUpload: async () => ({ status: 'new', uploaded: [] }),
    uploadChunk: async (_md5, index) => {
      if (index === 1) throw new Error('Network Error')
    },
    mergeChunks: async () => assert.fail('merge must not run after a failed chunk'),
  }

  await assert.rejects(
    uploadFileInChunks({
      file: makeFile(11),
      api,
      chunkSize: 5,
      calculateMD5: async () => 'fedcba9876543210fedcba9876543210',
    }),
    /第 2\/3 片.*Network Error/,
    'chunk failures should identify the resumable chunk position',
  )
}

{
  const { calculateFileMD5 } = await import('../src/chunkedUpload.js')
  const progress = []
  const file = new Blob([new TextEncoder().encode('hello world')])
  const md5 = await calculateFileMD5(file, {
    chunkSize: 4,
    onProgress: (percent) => progress.push(percent),
  })

  assert.equal(md5, '5eb63bbbe01eeed093cb22bb8f5acdc3')
  assert.ok(progress.length >= 3, 'hashing should process the file incrementally')
  assert.equal(progress.at(-1), 100)
}

{
  let checkedMD5 = ''
  const api = {
    checkUpload: async (md5) => {
      checkedMD5 = md5
      return { status: 'new', uploaded: [] }
    },
    uploadChunk: async () => {},
    mergeChunks: async () => ({ task_id: 11 }),
  }
  const file = new Blob([new TextEncoder().encode('hello world')], { type: 'video/mp4' })
  Object.defineProperty(file, 'name', { value: 'default-md5.mp4' })

  await uploadFileInChunks({ file, api, chunkSize: 4 })
  assert.equal(checkedMD5, '5eb63bbbe01eeed093cb22bb8f5acdc3', 'the production flow should use incremental MD5 by default')
}
