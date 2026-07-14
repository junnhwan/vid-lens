import SparkMD5 from 'spark-md5'

export const CHUNK_SIZE = 1024 * 1024

function emitProgress(onProgress, stage, percent, extra = {}) {
  onProgress?.({ stage, percent: Math.max(0, Math.min(100, Math.round(percent))), ...extra })
}

function chunkByteLength(fileSize, chunkSize, index) {
  const start = index * chunkSize
  return Math.max(0, Math.min(chunkSize, fileSize - start))
}

export async function calculateFileMD5(file, { chunkSize = CHUNK_SIZE, onProgress } = {}) {
  if (!file || file.size <= 0) throw new Error('文件不能为空')
  if (!Number.isInteger(chunkSize) || chunkSize <= 0) throw new Error('分片大小必须大于 0')

  const spark = new SparkMD5.ArrayBuffer()
  for (let start = 0; start < file.size; start += chunkSize) {
    const end = Math.min(start + chunkSize, file.size)
    const buffer = await file.slice(start, end).arrayBuffer()
    spark.append(buffer)
    onProgress?.((end / file.size) * 100)
  }
  return spark.end()
}

export function formatUploadError(error) {
  if (error?.response?.status === 413 || error?.status === 413) {
    return '上传请求过大，请使用分片上传后重试'
  }
  const serverMessage = error?.response?.data?.message || error?.response?.data?.msg
  if (typeof serverMessage === 'string' && serverMessage.trim()) {
    return serverMessage
  }
  if (error?.code === 'ECONNABORTED') {
    return '上传超时，请重新选择同一文件继续上传'
  }
  if (typeof error?.message === 'string' && error.message.trim()) {
    return error.message
  }
  if (typeof error === 'string' && error.trim()) {
    return error
  }
  return '上传失败，请稍后重试'
}

export function formatUploadProgressMessage({
  chunkNumber,
  chunkPercent = 0,
  uploadedChunks = 0,
  totalChunks = 0,
  bytesPerSecond = 0,
  etaSeconds,
  retryAttempt = 0,
} = {}) {
  if (!totalChunks) return '正在上传分片...'
  if (!Number.isInteger(chunkNumber)) return `正在上传分片（已完成 ${uploadedChunks}/${totalChunks}）...`

  const prefix = retryAttempt > 0
    ? `正在重试第 ${chunkNumber + 1}/${totalChunks} 片（第 ${retryAttempt} 次）`
    : `正在上传第 ${chunkNumber + 1}/${totalChunks} 片（当前片 ${chunkPercent}%）`
  const details = [`已完成 ${uploadedChunks}/${totalChunks}`]
  if (bytesPerSecond > 0) {
    details.push(bytesPerSecond >= 1024 * 1024
      ? `${(bytesPerSecond / 1024 / 1024).toFixed(1)} MiB/s`
      : `${Math.round(bytesPerSecond / 1024)} KiB/s`)
  }
  if (Number.isFinite(etaSeconds) && etaSeconds >= 0) {
    details.push(etaSeconds < 60
      ? `预计剩余 ${Math.max(1, Math.ceil(etaSeconds))} 秒`
      : `预计剩余 ${Math.ceil(etaSeconds / 60)} 分钟`)
  }
  return `${prefix} · ${details.join(' · ')}`
}

export async function uploadFileInChunks({
  file,
  api,
  onProgress,
  calculateMD5 = calculateFileMD5,
  chunkSize = CHUNK_SIZE,
  maxChunkRetries = 2,
  maxConcurrency = 3,
  retryDelay = (ms) => new Promise((resolve) => setTimeout(resolve, ms)),
  now = () => Date.now(),
}) {
  if (!file || file.size <= 0) throw new Error('文件不能为空')
  if (!api) throw new Error('上传 API 不可用')
  if (typeof calculateMD5 !== 'function') throw new Error('文件指纹计算器不可用')
  if (!Number.isInteger(chunkSize) || chunkSize <= 0) throw new Error('分片大小必须大于 0')
  if (!Number.isInteger(maxConcurrency) || maxConcurrency <= 0) throw new Error('上传并发数必须大于 0')

  emitProgress(onProgress, 'hashing', 0)
  const fileMD5 = await calculateMD5(file, {
    chunkSize,
    onProgress: (percent) => emitProgress(onProgress, 'hashing', percent * 0.1),
  })
  const totalChunks = Math.ceil(file.size / chunkSize)
  const uploadState = await api.checkUpload(fileMD5)
  const uploaded = new Set(
    Array.isArray(uploadState?.uploaded)
      ? uploadState.uploaded.map(Number).filter((index) => Number.isInteger(index) && index >= 0 && index < totalChunks)
      : [],
  )

  let completedBytes = [...uploaded].reduce(
    (total, index) => total + chunkByteLength(file.size, chunkSize, index),
    0,
  )
  const initialCompletedBytes = completedBytes
  const uploadStartedAt = now()
  const inflightLoaded = new Map()
  const pendingIndexes = []
  for (let index = 0; index < totalChunks; index += 1) {
    if (!uploaded.has(index)) pendingIndexes.push(index)
  }

  const emitUploadProgress = (index, chunkSizeForIndex, retryAttempt = 0) => {
    const inflightBytes = [...inflightLoaded.values()].reduce((total, loaded) => total + loaded, 0)
    const transferredBytes = Math.min(completedBytes + inflightBytes, file.size)
    const elapsedSeconds = Math.max((now() - uploadStartedAt) / 1000, 0.001)
    const bytesPerSecond = Math.max(transferredBytes - initialCompletedBytes, 0) / elapsedSeconds
    const loaded = inflightLoaded.get(index) || 0
    emitProgress(onProgress, 'uploading', 10 + (transferredBytes / file.size) * 85, {
      chunkNumber: index,
      chunkPercent: chunkSizeForIndex > 0 ? Math.round((loaded / chunkSizeForIndex) * 100) : 0,
      uploadedChunks: uploaded.size,
      totalChunks,
      bytesPerSecond,
      etaSeconds: bytesPerSecond > 0 ? Math.max(file.size - transferredBytes, 0) / bytesPerSecond : null,
      retryAttempt,
      maxConcurrency,
    })
  }

  emitProgress(onProgress, 'uploading', 10 + (completedBytes / file.size) * 85, {
    uploadedChunks: uploaded.size,
    totalChunks,
    maxConcurrency,
  })

  if (uploadState?.status !== 'completed' && pendingIndexes.length > 0) {
    let cursor = 0
    let fatalError = null

    const worker = async () => {
      while (!fatalError) {
        const position = cursor
        cursor += 1
        if (position >= pendingIndexes.length) return

        const index = pendingIndexes[position]
        const start = index * chunkSize
        const chunk = file.slice(start, Math.min(start + chunkSize, file.size))
        let lastError
        let succeeded = false

        for (let attempt = 0; attempt <= maxChunkRetries; attempt += 1) {
          inflightLoaded.set(index, 0)
          if (attempt > 0) {
            emitUploadProgress(index, chunk.size, attempt)
            await retryDelay(Math.min(1000 * (2 ** (attempt - 1)), 4000))
          }
          try {
            await api.uploadChunk(fileMD5, index, chunk, (event) => {
              inflightLoaded.set(index, Math.min(Number(event?.loaded) || 0, chunk.size))
              emitUploadProgress(index, chunk.size, attempt)
            })
            succeeded = true
            break
          } catch (error) {
            lastError = error
            inflightLoaded.set(index, 0)
          }
        }

        inflightLoaded.delete(index)
        if (!succeeded) {
          fatalError = new Error(`第 ${index + 1}/${totalChunks} 片上传失败：${formatUploadError(lastError)}；重新选择同一文件可断点续传`, { cause: lastError })
          return
        }

        completedBytes += chunk.size
        uploaded.add(index)
        emitUploadProgress(index, chunk.size)
      }
    }

    await Promise.all(Array.from(
      { length: Math.min(maxConcurrency, pendingIndexes.length) },
      () => worker(),
    ))
    if (fatalError) throw fatalError
  }

  emitProgress(onProgress, 'merging', 95, { totalChunks })
  const result = await api.mergeChunks(fileMD5, file.name, totalChunks)
  emitProgress(onProgress, 'completed', 100, { totalChunks })
  return result
}
