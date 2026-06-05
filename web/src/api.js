import axios from 'axios'

const api = axios.create({
  baseURL: '/api/v1',
  timeout: 300000, // 5 分钟（大文件上传）
})

// 请求拦截器：自动带 Token
api.interceptors.request.use(config => {
  const user = JSON.parse(localStorage.getItem('vidlens_user') || '{}')
  if (user.token) {
    config.headers.Authorization = `Bearer ${user.token}`
  }
  return config
})

// 响应拦截器：统一处理错误
api.interceptors.response.use(
  res => res.data,
  err => {
    if (err.response?.status === 401) {
      localStorage.removeItem('vidlens_user')
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
  listTasks: (page = 1, pageSize = 20) =>
    api.get('/media/list', { params: { page, pageSize } }),
  getTask: (id) => api.get(`/media/task/${id}`),
  deleteTask: (id) => api.delete(`/media/task/${id}`),
  analyze: (id) => api.post(`/media/analyze/${id}`),
  transcribe: (id) => api.post(`/media/transcribe/${id}`),
  downloadAudio: (id) => api.get(`/media/download-audio/${id}`),

  // 分片上传
  checkUpload: (fileMD5) => api.get('/media/check-upload', { params: { file_md5: fileMD5 } }),
  uploadChunk: (fileMD5, chunkNumber, chunkData) => {
    const form = new FormData()
    form.append('file_md5', fileMD5)
    form.append('chunk_number', chunkNumber)
    form.append('chunk', chunkData)
    return api.post('/media/upload-chunk', form, {
      headers: { 'Content-Type': 'multipart/form-data' },
    })
  },
  mergeChunks: (fileMD5, filename, totalChunks) =>
    api.post('/media/merge-chunks', { file_md5: fileMD5, filename, total_chunks: totalChunks }),
}
