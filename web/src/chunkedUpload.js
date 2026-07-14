import SparkMD5 from 'spark-md5'

export const CHUNK_SIZE = 5 * 1024 * 1024

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

export async function uploadFileInChunks({
  file,
  api,
  onProgress,
  calculateMD5 = calculateFileMD5,
  chunkSize = CHUNK_SIZE,
}) {
  if (!file || file.size <= 0) throw new Error('文件不能为空')
  if (!api) throw new Error('上传 API 不可用')
  if (typeof calculateMD5 !== 'function') throw new Error('文件指纹计算器不可用')
  if (!Number.isInteger(chunkSize) || chunkSize <= 0) throw new Error('分片大小必须大于 0')

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
  emitProgress(onProgress, 'uploading', 10 + (completedBytes / file.size) * 85, {
    uploadedChunks: uploaded.size,
    totalChunks,
  })

  if (uploadState?.status !== 'completed') {
    for (let index = 0; index < totalChunks; index += 1) {
      if (uploaded.has(index)) continue

      const start = index * chunkSize
      const chunk = file.slice(start, Math.min(start + chunkSize, file.size))
      try {
        await api.uploadChunk(fileMD5, index, chunk, (event) => {
          const loaded = Math.min(Number(event?.loaded) || 0, chunk.size)
          emitProgress(onProgress, 'uploading', 10 + ((completedBytes + loaded) / file.size) * 85, {
            chunkNumber: index,
            uploadedChunks: uploaded.size,
            totalChunks,
          })
        })
      } catch (error) {
        throw new Error(`第 ${index + 1}/${totalChunks} 片上传失败：${formatUploadError(error)}`, { cause: error })
      }

      completedBytes += chunk.size
      uploaded.add(index)
      emitProgress(onProgress, 'uploading', 10 + (completedBytes / file.size) * 85, {
        chunkNumber: index,
        uploadedChunks: uploaded.size,
        totalChunks,
      })
    }
  }

  emitProgress(onProgress, 'merging', 95, { totalChunks })
  const result = await api.mergeChunks(fileMD5, file.name, totalChunks)
  emitProgress(onProgress, 'completed', 100, { totalChunks })
  return result
}


