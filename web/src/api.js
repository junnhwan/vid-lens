import axios from 'axios'
import { unwrapApiResponse } from './apiEnvelope.js'
import { shouldResetSessionOnUnauthorized } from './authErrorPolicy.js'
import { getStoredAuthToken } from './authSession.js'

const api = axios.create({
  baseURL: '/api/v1',
  timeout: 300000, // 5 分钟（大文件上传）
})

function normalizeChatStreamEvent(eventType, payload) {
  if (payload && typeof payload === 'object' && !Array.isArray(payload) && payload.type) {
    return payload
  }
  if (eventType === 'citations') {
    return { type: 'citations', citations: Array.isArray(payload) ? payload : payload?.citations || [] }
  }
  if (eventType === 'answer') {
    return { type: 'answer', delta: typeof payload === 'string' ? payload : payload?.delta || '' }
  }
  if (eventType === 'done') {
    return { type: 'done', ...(payload && typeof payload === 'object' ? payload : {}) }
  }
  if (eventType === 'error') {
    return { type: 'error', message: typeof payload === 'string' ? payload : payload?.message || '回答失败' }
  }
  return payload
}

// 请求拦截器：自动带 Token
api.interceptors.request.use(config => {
  const token = getStoredAuthToken()
  if (token) {
    config.headers.Authorization = `Bearer ${token}`
  }
  return config
})

// 响应拦截器：统一处理错误
api.interceptors.response.use(
  res => unwrapApiResponse(res),
  err => {
    if (err.response?.status === 401 && shouldResetSessionOnUnauthorized(err.config?.url)) {
      localStorage.removeItem('user')
      window.location.reload()
    }
    return Promise.reject(err.response?.data || err)
  }
)

export default {
  // 用户
  register: (username, password, nickname) =>
    api.post('/user/register', { username, password, nickname }),
  login: (username, password) =>
    api.post('/user/login', { username, password }),
  getProfile: () => api.get('/user/profile'),

  // 媒体
  uploadFile: (file, onProgress) => {
    const form = new FormData()
    form.append('file', file)
    return api.post('/media/upload', form, {
      headers: { 'Content-Type': 'multipart/form-data' },
      onUploadProgress: onProgress,
    })
  },
  uploadByURL: (url) => api.post('/media/upload-url', { url }),
  listTasks: (page = 1, pageSize = 20, keyword = '') =>
    api.get('/media/list', { params: { page, page_size: pageSize, keyword } }),
  getTask: (id) => api.get(`/media/task/${id}`),
  deleteTask: (id) => api.delete(`/media/task/${id}`),
  analyze: (id) => api.post(`/media/analyze/${id}`),
  transcribe: (id) => api.post(`/media/transcribe/${id}`),
  downloadAudio: (id) => api.get(`/media/download-audio/${id}`),

  // 分片上传
  checkUpload: (fileMD5, fileSize, chunkSize, totalChunks) => api.get('/media/check-upload', {
    params: { file_md5: fileMD5, file_size: fileSize, chunk_size: chunkSize, total_chunks: totalChunks },
  }),
  uploadChunk: (fileMD5, chunkNumber, chunkData, onProgress) => {
    const form = new FormData()
    form.append('file_md5', fileMD5)
    form.append('chunk_number', chunkNumber)
    form.append('chunk', chunkData)
    return api.post('/media/upload-chunk', form, {
      headers: { 'Content-Type': 'multipart/form-data' },
      onUploadProgress: onProgress,
    })
  },
  mergeChunks: (fileMD5, filename, totalChunks, fileSize, chunkSize) =>
    api.post('/media/merge-chunks', {
      file_md5: fileMD5, filename, total_chunks: totalChunks, file_size: fileSize, chunk_size: chunkSize,
    }),

  // AI 配置
  getAIProfiles: () => api.get('/ai/profiles'),
  createAIProfile: (profile) => api.post('/ai/profiles', profile),
  updateAIProfile: (id, profile) => api.put(`/ai/profiles/${id}`, profile),
  deleteAIProfile: (id) => api.delete(`/ai/profiles/${id}`),
  testAIProfile: (profile) => api.post('/ai/profiles/test', profile),

  // RAG 索引
  buildRAGIndex: (taskId, rebuild = false) =>
    api.post(`/media/task/${taskId}/rag-index`, { rebuild }),
  getRAGIndexStatus: (taskId) =>
    api.get(`/media/task/${taskId}/rag-index`),

  // 聊天会话
  createChatSession: (taskId, title) =>
    api.post('/chat/sessions', { task_id: taskId, title }),
  getChatSessions: (taskId) =>
    api.get('/chat/sessions', { params: { task_id: taskId } }),
  getChatMessages: (sessionId) =>
    api.get(`/chat/sessions/${sessionId}/messages`),
  sendChatMessage: (sessionId, question, topK = 5, mode = 'video_assistant') =>
    api.post(`/chat/sessions/${sessionId}/messages`, { question, top_k: topK, mode }),

  // Agentic Video QA（非流式）：返回 { message_id, answer, template, citations, trace, model }
  sendAgentMessage: (sessionId, question, topK = 5) =>
    api.post(`/chat/sessions/${sessionId}/messages/agent`, { question, top_k: topK }),

  // 流式聊天（SSE）
  sendChatMessageStream: async (sessionId, question, topK = 5, modeOrOnEvent = 'video_assistant', maybeOnEvent, signal) => {
    const mode = typeof modeOrOnEvent === 'string' ? modeOrOnEvent : 'video_assistant'
    const onEvent = typeof modeOrOnEvent === 'function' ? modeOrOnEvent : maybeOnEvent
    const token = getStoredAuthToken()
    const response = await fetch(`/api/v1/chat/sessions/${sessionId}/messages/stream`, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        'Authorization': token ? `Bearer ${token}` : '',
      },
      body: JSON.stringify({ question, top_k: topK, mode }),
      signal,
    })

    if (!response.ok) {
      const err = await response.json().catch(() => ({ message: '请求失败' }))
      throw new Error(err.message || `HTTP ${response.status}`)
    }

    const reader = response.body.getReader()
    const decoder = new TextDecoder()
    let buffer = ''
    let eventType = 'message'
    let eventData = []

    const flushEvent = () => {
      if (!eventData.length) {
        eventType = 'message'
        return
      }
      const data = eventData.join('\n').trim()
      eventData = []
      const currentType = eventType
      eventType = 'message'
      if (!data) return

      let payload
      try {
        payload = JSON.parse(data)
      } catch (e) {
        if (currentType !== 'answer' && currentType !== 'error') {
          console.warn('解析 SSE 事件失败:', data, e)
          return
        }
        payload = data
      }
      onEvent(normalizeChatStreamEvent(currentType, payload))
    }

    while (true) {
      const { done, value } = await reader.read()
      if (done) {
        flushEvent()
        break
      }

      buffer += decoder.decode(value, { stream: true })
      const lines = buffer.split('\n')
      buffer = lines.pop() || ''

      for (const line of lines) {
        const text = line.replace(/\r$/, '')
        if (!text.trim()) {
          flushEvent()
          continue
        }
        if (text.startsWith('event:')) {
          eventType = text.slice(6).trim() || 'message'
          continue
        }
        if (text.startsWith('data:')) {
          eventData.push(text.slice(5).trimStart())
        }
      }
    }
  },

  deleteChatSession: (sessionId) =>
    api.delete(`/chat/sessions/${sessionId}`),
}
