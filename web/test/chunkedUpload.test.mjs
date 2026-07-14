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

{
  const progress = []
  let attempts = 0
  const api = {
    checkUpload: async () => ({ status: 'uploading', uploaded: [] }),
    uploadChunk: async (_md5, _index, chunk, onProgress) => {
      attempts += 1
      if (attempts === 1) throw Object.assign(new Error('Request failed with status code 400'), { response: { status: 400, data: { message: '请选择分片文件' } } })
      onProgress?.({ loaded: Math.ceil(chunk.size / 2), total: chunk.size })
      onProgress?.({ loaded: chunk.size, total: chunk.size })
    },
    mergeChunks: async () => ({ task_id: 12 }),
  }

  const result = await uploadFileInChunks({
    file: makeFile(5), api, chunkSize: 5, maxChunkRetries: 2,
    retryDelay: async () => {},
    now: (() => { let value = 0; return () => (value += 1000) })(),
    calculateMD5: async () => '11111111111111111111111111111111',
    onProgress: (event) => progress.push(event),
  })

  assert.deepEqual(result, { task_id: 12 })
  assert.equal(attempts, 2, 'a transient chunk failure should be retried')
  const uploading = progress.filter((event) => event.stage === 'uploading' && event.chunkNumber === 0)
  assert.ok(uploading.some((event) => event.retryAttempt === 1), 'retry state should be visible')
  assert.ok(uploading.some((event) => event.chunkPercent === 60), 'current chunk percentage should be emitted')
  assert.ok(uploading.some((event) => event.bytesPerSecond > 0), 'upload speed should be emitted')
  assert.ok(uploading.some((event) => Number.isFinite(event.etaSeconds)), 'remaining time should be emitted')
}

{
  const { formatUploadError } = await import('../src/chunkedUpload.js')
  const error = { response: { status: 400, data: { message: '请选择分片文件' } }, message: 'Request failed with status code 400' }
  assert.match(formatUploadError(error), /请选择分片文件/)
}

{
  const { formatUploadProgressMessage } = await import('../src/chunkedUpload.js')
  assert.equal(
    formatUploadProgressMessage({ chunkNumber: 1, chunkPercent: 37, uploadedChunks: 1, totalChunks: 24, bytesPerSecond: 58 * 1024, etaSeconds: 2040 }),
    '正在上传第 2/24 片（当前片 37%） · 已完成 1/24 · 58 KiB/s · 预计剩余 34 分钟',
  )
  assert.match(
    formatUploadProgressMessage({ chunkNumber: 3, chunkPercent: 0, uploadedChunks: 3, totalChunks: 24, retryAttempt: 1 }),
    /正在重试第 4\/24 片（第 1 次）/,
  )
}

{
  const { CHUNK_SIZE } = await import('../src/chunkedUpload.js')
  assert.equal(CHUNK_SIZE, 1024 * 1024, 'Cloudflare uploads should use 1 MiB chunks to stay below request deadlines')
}

{
  let active = 0
  let maxActive = 0
  const indexes = []
  const api = {
    checkUpload: async () => ({ status: 'new', uploaded: [] }),
    uploadChunk: async (_md5, index, chunk, onProgress) => {
      indexes.push(index)
      active += 1
      maxActive = Math.max(maxActive, active)
      await new Promise((resolve) => setTimeout(resolve, 10))
      onProgress?.({ loaded: chunk.size, total: chunk.size })
      active -= 1
    },
    mergeChunks: async () => ({ task_id: 13 }),
  }

  await uploadFileInChunks({
    file: makeFile(10), api, chunkSize: 2, maxConcurrency: 3,
    calculateMD5: async () => '22222222222222222222222222222222',
  })

  assert.equal(maxActive, 3, 'uploads should use the configured bounded concurrency')
  assert.deepEqual(indexes.sort((a, b) => a - b), [0, 1, 2, 3, 4])
}
