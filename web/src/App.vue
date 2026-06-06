<template>
  <div id="app">
    <!-- 顶部导航 -->
    <nav class="navbar">
      <div class="nav-container">
        <div class="brand">
          <span class="mirror-icon">◇</span>
          <span class="brand-text">镜知 <span class="en">VidLens</span></span>
        </div>
        <div class="nav-right">
          <template v-if="user">
            <div class="user-badge">
              <span class="user-avatar">{{ user.nickname?.[0] || 'U' }}</span>
              <span class="user-name">{{ user.nickname || user.username }}</span>
            </div>
            <button class="btn-text" @click="logout">退出</button>
          </template>
          <button v-else class="btn-amber" @click="openAuth">登录 / 注册</button>
        </div>
      </div>
    </nav>

    <!-- 主容器 -->
    <main class="main-container">
      <!-- 上传磁贴 -->
      <section class="upload-tiles">
        <div class="tile" :class="{ disabled: !user, uploading }"
             @dragover.prevent="dragging = true"
             @dragleave.prevent="dragging = false"
             @drop.prevent="handleDrop">
          <div class="tile-icon">📁</div>
          <h3>本地上传</h3>
          <p>拖拽视频文件或点击选择</p>
          <input type="file" accept="video/*" :disabled="!user" @change="handleFileSelect" hidden ref="fileInput" />
          <button class="tile-btn" @click="triggerFileInput" :disabled="!user">
            {{ dragging ? '松手上传' : '选择文件' }}
          </button>
        </div>

        <div class="tile" :class="{ disabled: !user, uploading }">
          <div class="tile-icon">🌐</div>
          <h3>链接下载</h3>
          <p>B站 / YouTube 视频链接</p>
          <div class="url-input-group">
            <input v-model="videoUrl" placeholder="粘贴链接..." @keyup.enter="handleUrlUpload" :disabled="!user || uploading" />
            <button class="tile-btn" @click="handleUrlUpload" :disabled="!user || uploading || !videoUrl">开始</button>
          </div>
        </div>
      </section>

      <!-- 上传状态提示 -->
      <div v-if="uploading" class="upload-status">
        <div class="spinner"></div>
        <span>{{ uploadMsg }}</span>
      </div>

      <!-- Toast 消息 -->
      <transition name="toast">
        <div v-if="toast" class="toast" :class="{ error: toastIsError }">{{ toast }}</div>
      </transition>

      <!-- 任务区 -->
      <section v-if="tasks.length" class="tasks-section">
        <div class="section-header">
          <h2>我的任务</h2>
          <div class="filter-tabs">
            <button v-for="tab in tabs" :key="tab.key"
                    :class="['tab', { active: activeTab === tab.key }]"
                    @click="activeTab = tab.key">
              {{ tab.label }} <span class="tab-count">{{ tab.count }}</span>
            </button>
          </div>
          <input v-model="searchQuery" class="search-box" placeholder="🔍 搜索文件名..." />
        </div>

        <div class="tasks-list">
          <div v-for="t in filteredTasks" :key="t.id" class="task-card" @click="toggleExpand(t.id)">
            <button class="task-delete" @click.stop="deleteTask(t)" title="删除">×</button>

            <div class="task-header">
              <div class="task-icon">🎬</div>
              <div class="task-info">
                <div class="task-name">{{ t.filename }}</div>
                <div class="task-meta">
                  <span class="meta-time">{{ formatTime(t.created_at) }}</span>
                  <span class="meta-dot">·</span>
                  <span class="meta-status" :class="statusClass(t.status)">{{ statusText(t.status) }}</span>
                </div>
              </div>
            </div>

            <div class="task-actions">
              <button class="action-btn" @click.stop="doTranscribe(t)" :disabled="isActionDisabled(t)">
                <span class="btn-icon">📄</span> 提取文字
              </button>
              <button class="action-btn amber" @click.stop="doAnalyze(t)" :disabled="isActionDisabled(t)">
                <span class="btn-icon">🤖</span> AI 总结
              </button>
            </div>

            <!-- 展开内容 -->
            <transition name="expand">
              <div v-if="expanded[t.id]" class="task-result" @click.stop>
                <div v-if="loading[t.id]" class="result-loading">
                  <div class="spinner small"></div> 处理中...
                </div>
                <template v-else>
                  <div v-if="t.transcription?.content" class="result-block">
                    <h4>📝 文字提取</h4>
                    <pre class="result-text">{{ t.transcription.content }}</pre>
                  </div>
                  <div v-if="t.summary?.content" class="result-block">
                    <h4>🤖 AI 总结</h4>
                    <div class="result-markdown" v-html="renderMarkdown(t.summary.content)"></div>
                  </div>
                </template>
              </div>
            </transition>
          </div>
        </div>
      </section>
    </main>

    <!-- 登录/注册弹窗 -->
    <transition name="modal">
      <div v-if="showAuth" class="modal-backdrop" @click="closeAuth">
        <div class="modal-panel" @click.stop>
          <button class="modal-close" @click="closeAuth">×</button>
          <h2>{{ authMode === 'login' ? '登录' : '注册' }}</h2>
          <div class="auth-form">
            <input v-model="authForm.username" placeholder="用户名" class="form-input" />
            <input v-model="authForm.password" type="password" placeholder="密码" class="form-input" />
            <input v-if="authMode === 'register'" v-model="authForm.nickname" placeholder="昵称" class="form-input" />
            <button class="btn-amber full" @click="handleAuth" :disabled="authLoading">
              {{ authLoading ? '处理中...' : (authMode === 'login' ? '立即登录' : '提交注册') }}
            </button>
            <p class="auth-switch">
              {{ authMode === 'login' ? '还没有账号？' : '已有账号？' }}
              <button class="link-btn" @click="switchAuthMode">
                {{ authMode === 'login' ? '去注册' : '去登录' }}
              </button>
            </p>
            <p v-if="authMsg" class="auth-msg" :class="{ error: authError }">{{ authMsg }}</p>
          </div>
        </div>
      </div>
    </transition>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { marked } from 'marked'
import api from './api'

// 状态
const user = ref(null)
const tasks = ref([])
const videoUrl = ref('')
const uploading = ref(false)
const uploadMsg = ref('')
const dragging = ref(false)
const toast = ref('')
const toastIsError = ref(false)
const expanded = ref({})
const loading = ref({})
const activeTab = ref('all')
const searchQuery = ref('')
const showAuth = ref(false)
const authMode = ref('login')
const authLoading = ref(false)
const authMsg = ref('')
const authError = ref(false)
const authForm = ref({ username: '', password: '', nickname: '' })
const fileInput = ref(null)
const pollingTimers = ref({})

// 计算属性
const tabs = computed(() => [
  { key: 'all', label: '全部', count: tasks.value.length },
  { key: 'processing', label: '处理中', count: tasks.value.filter(t => t.status < 3).length },
  { key: 'completed', label: '已完成', count: tasks.value.filter(t => t.status === 3).length }
])

const filteredTasks = computed(() => {
  let result = tasks.value
  if (activeTab.value === 'processing') result = result.filter(t => t.status < 3)
  if (activeTab.value === 'completed') result = result.filter(t => t.status === 3)
  if (searchQuery.value) {
    const q = searchQuery.value.toLowerCase()
    result = result.filter(t => t.filename?.toLowerCase().includes(q))
  }
  return result
})

// 工具函数
const showToast = (msg, isError = false) => {
  toast.value = msg
  toastIsError.value = isError
  setTimeout(() => { if (toast.value === msg) toast.value = '' }, 3500)
}

const formatTime = (str) => {
  if (!str) return '--'
  const d = new Date(str)
  return `${d.getMonth() + 1}/${d.getDate()} ${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`
}

const statusClass = (s) => ['pending', 'queued', 'running', 'completed', 'failed'][s] || 'pending'
const statusText = (s) => ['待处理', '排队中', '处理中', '已完成', '失败'][s] || '未知'
const isActionDisabled = (t) => t.status !== 3
const renderMarkdown = (content) => marked.parse(content || '')

// 业务逻辑
const triggerFileInput = () => {
  if (!user.value) {
    showToast('请先登录', true)
    openAuth()
    return
  }
  fileInput.value?.click()
}

const handleFileSelect = async (e) => {
  const file = e.target.files?.[0]
  if (!file) return
  await uploadFile(file)
  e.target.value = ''
}

const handleDrop = async (e) => {
  dragging.value = false
  if (!user.value) {
    showToast('请先登录', true)
    openAuth()
    return
  }
  const file = e.dataTransfer.files?.[0]
  if (!file || !file.type.startsWith('video/')) {
    showToast('仅支持视频文件', true)
    return
  }
  await uploadFile(file)
}

const uploadFile = async (file) => {
  uploading.value = true
  uploadMsg.value = '正在上传...'
  try {
    const formData = new FormData()
    formData.append('file', file)
    await api.uploadFile(formData)
    showToast('上传成功')
    await fetchTasks()
  } catch (err) {
    showToast(err.response?.data?.message || '上传失败', true)
  } finally {
    uploading.value = false
  }
}

const handleUrlUpload = async () => {
  if (!user.value) {
    showToast('请先登录', true)
    openAuth()
    return
  }
  if (!videoUrl.value || !videoUrl.value.startsWith('http')) {
    showToast('请输入合法的链接', true)
    return
  }
  uploading.value = true
  uploadMsg.value = '正在下载并解析...'
  try {
    const formData = new FormData()
    formData.append('url', videoUrl.value)
    await api.uploadByUrl(formData)
    showToast('下载成功')
    videoUrl.value = ''
    await fetchTasks()
  } catch (err) {
    showToast(err.response?.data?.message || '下载失败', true)
  } finally {
    uploading.value = false
  }
}

const fetchTasks = async () => {
  if (!user.value) {
    tasks.value = []
    return
  }
  try {
    const res = await api.listTasks()
    tasks.value = res.data || []
  } catch (err) {
    console.error(err)
  }
}

const deleteTask = async (task) => {
  if (!confirm(`确认删除「${task.filename}」？`)) return
  try {
    await api.deleteTask(task.id)
    showToast('删除成功')
    tasks.value = tasks.value.filter(t => t.id !== task.id)
  } catch (err) {
    showToast('删除失败', true)
  }
}

const toggleExpand = (id) => {
  expanded.value[id] = !expanded.value[id]
}

const doTranscribe = async (task) => {
  if (task.transcription?.content) {
    expanded.value[task.id] = true
    return
  }
  if (loading.value[task.id]) return
  loading.value[task.id] = true
  expanded.value[task.id] = true
  try {
    await api.requestTranscribe(task.id)
    startPolling(task.id, 'transcription')
  } catch (err) {
    showToast('请求失败', true)
    loading.value[task.id] = false
  }
}

const doAnalyze = async (task) => {
  if (task.summary?.content) {
    expanded.value[task.id] = true
    return
  }
  if (loading.value[task.id]) return
  loading.value[task.id] = true
  expanded.value[task.id] = true
  try {
    await api.requestAnalysis(task.id)
    startPolling(task.id, 'summary')
  } catch (err) {
    showToast(err.response?.data?.message || '请求失败', true)
    loading.value[task.id] = false
  }
}

const startPolling = (taskId, type) => {
  if (pollingTimers.value[taskId]) clearInterval(pollingTimers.value[taskId])
  const timer = setInterval(async () => {
    await fetchTasks()
    const task = tasks.value.find(t => t.id === taskId)
    if (!task) return
    const hasResult = type === 'transcription' ? task.transcription?.content : task.summary?.content
    if (hasResult) {
      clearInterval(timer)
      delete pollingTimers.value[taskId]
      loading.value[taskId] = false
      showToast('处理完成')
    }
  }, 3000)
  pollingTimers.value[taskId] = timer
  setTimeout(() => {
    if (pollingTimers.value[taskId]) {
      clearInterval(pollingTimers.value[taskId])
      delete pollingTimers.value[taskId]
      loading.value[taskId] = false
    }
  }, 300000)
}

// 认证相关
const openAuth = () => {
  showAuth.value = true
  authMsg.value = ''
  authForm.value = { username: '', password: '', nickname: '' }
}
const closeAuth = () => { showAuth.value = false }
const switchAuthMode = () => {
  authMode.value = authMode.value === 'login' ? 'register' : 'login'
  authMsg.value = ''
}

const handleAuth = async () => {
  if (!authForm.value.username || !authForm.value.password) {
    authMsg.value = '请输入用户名和密码'
    authError.value = true
    return
  }
  authLoading.value = true
  authMsg.value = ''
  try {
    const res = authMode.value === 'login'
      ? await api.login(authForm.value)
      : await api.register(authForm.value)
    if (authMode.value === 'login') {
      user.value = res.data
      localStorage.setItem('token', res.data.token)
      localStorage.setItem('user', JSON.stringify(res.data))
      closeAuth()
      showToast(`欢迎回来，${res.data.nickname || res.data.username}`)
      await fetchTasks()
    } else {
      authMsg.value = '注册成功，请登录'
      authError.value = false
      setTimeout(() => switchAuthMode(), 1500)
    }
  } catch (err) {
    authMsg.value = err.response?.data?.message || '操作失败'
    authError.value = true
  } finally {
    authLoading.value = false
  }
}

const logout = () => {
  user.value = null
  localStorage.removeItem('token')
  localStorage.removeItem('user')
  tasks.value = []
  showToast('已退出')
}

onMounted(() => {
  const saved = localStorage.getItem('user')
  if (saved) {
    try {
      user.value = JSON.parse(saved)
      fetchTasks()
    } catch (e) {}
  }
})
</script>

<style scoped>
* { box-sizing: border-box; }

#app {
  min-height: 100vh;
  background: linear-gradient(135deg, #0f172a 0%, #1e293b 100%);
  color: #e2e8f0;
  font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif;
}

/* 导航栏 */
.navbar {
  backdrop-filter: blur(20px);
  background: rgba(15, 23, 42, 0.7);
  border-bottom: 1px solid rgba(248, 113, 113, 0.1);
  padding: 1rem 0;
  position: sticky;
  top: 0;
  z-index: 100;
}

.nav-container {
  max-width: 1200px;
  margin: 0 auto;
  padding: 0 2rem;
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.brand {
  display: flex;
  align-items: center;
  gap: 0.75rem;
  font-size: 1.5rem;
  font-weight: 700;
}

.mirror-icon {
  font-size: 2rem;
  color: #f59e0b;
  filter: drop-shadow(0 0 8px rgba(245, 158, 11, 0.5));
}

.brand-text {
  background: linear-gradient(135deg, #f59e0b, #fbbf24);
  -webkit-background-clip: text;
  -webkit-text-fill-color: transparent;
  background-clip: text;
}

.brand-text .en {
  font-size: 0.75rem;
  opacity: 0.7;
  margin-left: 0.25rem;
}

.nav-right { display: flex; align-items: center; gap: 1rem; }
.user-badge { display: flex; align-items: center; gap: 0.5rem; padding: 0.5rem 1rem; background: rgba(245, 158, 11, 0.1); border-radius: 2rem; border: 1px solid rgba(245, 158, 11, 0.3); }
.user-avatar { width: 2rem; height: 2rem; border-radius: 50%; background: linear-gradient(135deg, #f59e0b, #fbbf24); display: flex; align-items: center; justify-content: center; font-weight: 700; color: #0f172a; }
.user-name { font-size: 0.9rem; }
.btn-text { background: none; border: none; color: #94a3b8; cursor: pointer; font-size: 0.9rem; transition: color 0.2s; }
.btn-text:hover { color: #f59e0b; }
.btn-amber { background: linear-gradient(135deg, #f59e0b, #fbbf24); color: #0f172a; border: none; padding: 0.6rem 1.5rem; border-radius: 0.5rem; font-weight: 600; cursor: pointer; transition: transform 0.2s, box-shadow 0.2s; }
.btn-amber:hover { transform: translateY(-2px); box-shadow: 0 4px 12px rgba(245, 158, 11, 0.4); }
.btn-amber.full { width: 100%; }

/* 主容器 */
.main-container { max-width: 1200px; margin: 0 auto; padding: 3rem 2rem; }

/* 上传磁贴 */
.upload-tiles { display: grid; grid-template-columns: repeat(auto-fit, minmax(300px, 1fr)); gap: 1.5rem; margin-bottom: 3rem; }
.tile { backdrop-filter: blur(20px); background: rgba(30, 41, 59, 0.5); border: 1px solid rgba(245, 158, 11, 0.2); border-radius: 1rem; padding: 2rem; text-align: center; transition: all 0.3s; }
.tile:hover:not(.disabled) { transform: translateY(-4px); box-shadow: 0 8px 24px rgba(245, 158, 11, 0.2); border-color: rgba(245, 158, 11, 0.5); }
.tile.disabled { opacity: 0.5; cursor: not-allowed; }
.tile.uploading { pointer-events: none; opacity: 0.6; }
.tile-icon { font-size: 3rem; margin-bottom: 1rem; }
.tile h3 { font-size: 1.25rem; margin-bottom: 0.5rem; color: #f59e0b; }
.tile p { font-size: 0.9rem; color: #94a3b8; margin-bottom: 1.5rem; }
.tile-btn { background: rgba(245, 158, 11, 0.1); border: 1px solid rgba(245, 158, 11, 0.3); color: #f59e0b; padding: 0.6rem 1.5rem; border-radius: 0.5rem; cursor: pointer; transition: all 0.2s; font-weight: 600; }
.tile-btn:hover:not(:disabled) { background: rgba(245, 158, 11, 0.2); border-color: #f59e0b; }
.tile-btn:disabled { opacity: 0.5; cursor: not-allowed; }
.url-input-group { display: flex; gap: 0.5rem; margin-top: 1rem; }
.url-input-group input { flex: 1; background: rgba(15, 23, 42, 0.5); border: 1px solid rgba(148, 163, 184, 0.2); border-radius: 0.5rem; padding: 0.6rem 1rem; color: #e2e8f0; outline: none; transition: border 0.2s; }
.url-input-group input:focus { border-color: #f59e0b; }

/* 上传状态 */
.upload-status { display: flex; align-items: center; justify-content: center; gap: 1rem; padding: 1rem; backdrop-filter: blur(20px); background: rgba(30, 41, 59, 0.5); border: 1px solid rgba(245, 158, 11, 0.3); border-radius: 0.75rem; margin-bottom: 2rem; }

/* Toast */
.toast { position: fixed; top: 2rem; right: 2rem; padding: 1rem 1.5rem; backdrop-filter: blur(20px); background: rgba(34, 197, 94, 0.9); border-radius: 0.5rem; font-weight: 600; z-index: 1000; box-shadow: 0 4px 12px rgba(0, 0, 0, 0.3); }
.toast.error { background: rgba(239, 68, 68, 0.9); }
.toast-enter-active, .toast-leave-active { transition: all 0.3s; }
.toast-enter-from, .toast-leave-to { opacity: 0; transform: translateX(100%); }

/* 任务区 */
.tasks-section { margin-top: 3rem; }
.section-header { display: flex; align-items: center; gap: 1.5rem; margin-bottom: 2rem; flex-wrap: wrap; }
.section-header h2 { font-size: 1.75rem; font-weight: 700; }
.filter-tabs { display: flex; gap: 0.5rem; flex: 1; }
.tab { background: rgba(30, 41, 59, 0.5); border: 1px solid rgba(148, 163, 184, 0.2); padding: 0.5rem 1rem; border-radius: 0.5rem; color: #94a3b8; cursor: pointer; transition: all 0.2s; display: flex; align-items: center; gap: 0.5rem; }
.tab:hover { border-color: rgba(245, 158, 11, 0.5); color: #f59e0b; }
.tab.active { background: rgba(245, 158, 11, 0.1); border-color: #f59e0b; color: #f59e0b; }
.tab-count { background: rgba(148, 163, 184, 0.2); padding: 0.125rem 0.5rem; border-radius: 0.25rem; font-size: 0.75rem; }
.search-box { padding: 0.6rem 1rem; background: rgba(15, 23, 42, 0.5); border: 1px solid rgba(148, 163, 184, 0.2); border-radius: 0.5rem; color: #e2e8f0; outline: none; min-width: 200px; }
.search-box:focus { border-color: #f59e0b; }

/* 任务列表 */
.tasks-list { display: flex; flex-direction: column; gap: 1rem; }
.task-card { backdrop-filter: blur(20px); background: rgba(30, 41, 59, 0.5); border: 1px solid rgba(148, 163, 184, 0.2); border-radius: 1rem; padding: 1.5rem; cursor: pointer; transition: all 0.3s; position: relative; }
.task-card:hover { border-color: rgba(245, 158, 11, 0.5); box-shadow: 0 4px 12px rgba(245, 158, 11, 0.15); }
.task-delete { position: absolute; top: 1rem; right: 1rem; background: none; border: none; color: #64748b; font-size: 1.5rem; cursor: pointer; opacity: 0; transition: all 0.2s; }
.task-card:hover .task-delete { opacity: 1; }
.task-delete:hover { color: #ef4444; transform: rotate(90deg); }
.task-header { display: flex; gap: 1rem; margin-bottom: 1rem; }
.task-icon { width: 3rem; height: 3rem; background: rgba(245, 158, 11, 0.1); border: 1px solid rgba(245, 158, 11, 0.3); border-radius: 0.75rem; display: flex; align-items: center; justify-content: center; font-size: 1.5rem; flex-shrink: 0; }
.task-info { flex: 1; min-width: 0; }
.task-name { font-size: 1.1rem; font-weight: 600; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.task-meta { display: flex; gap: 0.5rem; font-size: 0.85rem; color: #94a3b8; margin-top: 0.25rem; }
.meta-dot { opacity: 0.5; }
.meta-status { padding: 0.125rem 0.5rem; border-radius: 0.25rem; font-weight: 600; }
.meta-status.pending { background: rgba(148, 163, 184, 0.2); color: #94a3b8; }
.meta-status.queued { background: rgba(59, 130, 246, 0.2); color: #60a5fa; }
.meta-status.running { background: rgba(245, 158, 11, 0.2); color: #fbbf24; }
.meta-status.completed { background: rgba(34, 197, 94, 0.2); color: #4ade80; }
.meta-status.failed { background: rgba(239, 68, 68, 0.2); color: #f87171; }

/* 任务操作按钮 */
.task-actions { display: flex; gap: 0.75rem; }
.action-btn { flex: 1; background: rgba(30, 41, 59, 0.5); border: 1px solid rgba(148, 163, 184, 0.2); padding: 0.75rem 1rem; border-radius: 0.5rem; color: #94a3b8; cursor: pointer; transition: all 0.2s; display: flex; align-items: center; justify-content: center; gap: 0.5rem; font-weight: 600; }
.action-btn:hover:not(:disabled) { border-color: #f59e0b; color: #f59e0b; background: rgba(245, 158, 11, 0.1); }
.action-btn.amber { border-color: rgba(245, 158, 11, 0.3); color: #f59e0b; }
.action-btn:disabled { opacity: 0.4; cursor: not-allowed; }
.btn-icon { font-size: 1.25rem; }

/* 任务结果展开 */
.task-result { margin-top: 1rem; padding-top: 1rem; border-top: 1px solid rgba(148, 163, 184, 0.2); }
.result-loading { display: flex; align-items: center; gap: 0.75rem; padding: 1rem; color: #94a3b8; }
.result-block { margin-bottom: 1.5rem; }
.result-block:last-child { margin-bottom: 0; }
.result-block h4 { font-size: 1rem; margin-bottom: 0.75rem; color: #f59e0b; }
.result-text { background: rgba(15, 23, 42, 0.5); padding: 1rem; border-radius: 0.5rem; font-size: 0.9rem; line-height: 1.6; white-space: pre-wrap; color: #cbd5e1; max-height: 300px; overflow-y: auto; }
.result-markdown { background: rgba(15, 23, 42, 0.5); padding: 1rem; border-radius: 0.5rem; line-height: 1.8; max-height: 400px; overflow-y: auto; }
.result-markdown :deep(h2), .result-markdown :deep(h3) { color: #f59e0b; margin-top: 1rem; margin-bottom: 0.5rem; }
.result-markdown :deep(p) { margin-bottom: 0.75rem; color: #cbd5e1; }
.result-markdown :deep(strong) { color: #fbbf24; }
.result-markdown :deep(ul) { padding-left: 1.5rem; }
.result-markdown :deep(li) { margin-bottom: 0.5rem; }
.expand-enter-active, .expand-leave-active { transition: all 0.3s ease; }
.expand-enter-from, .expand-leave-to { opacity: 0; max-height: 0; overflow: hidden; }

/* Spinner */
.spinner { width: 1.5rem; height: 1.5rem; border: 3px solid rgba(245, 158, 11, 0.2); border-top-color: #f59e0b; border-radius: 50%; animation: spin 0.8s linear infinite; }
.spinner.small { width: 1rem; height: 1rem; border-width: 2px; }
@keyframes spin { to { transform: rotate(360deg); } }

/* 登录弹窗 */
.modal-backdrop { position: fixed; inset: 0; background: rgba(0, 0, 0, 0.7); backdrop-filter: blur(8px); display: flex; align-items: center; justify-content: center; z-index: 1000; }
.modal-panel { width: 90%; max-width: 400px; backdrop-filter: blur(20px); background: rgba(30, 41, 59, 0.9); border: 1px solid rgba(245, 158, 11, 0.3); border-radius: 1rem; padding: 2rem; position: relative; }
.modal-close { position: absolute; top: 1rem; right: 1rem; background: none; border: none; color: #64748b; font-size: 2rem; cursor: pointer; transition: color 0.2s; }
.modal-close:hover { color: #f59e0b; }
.modal-panel h2 { font-size: 1.5rem; margin-bottom: 1.5rem; text-align: center; }
.auth-form { display: flex; flex-direction: column; gap: 1rem; }
.form-input { background: rgba(15, 23, 42, 0.5); border: 1px solid rgba(148, 163, 184, 0.2); padding: 0.75rem 1rem; border-radius: 0.5rem; color: #e2e8f0; outline: none; transition: border 0.2s; }
.form-input:focus { border-color: #f59e0b; }
.auth-switch { text-align: center; font-size: 0.9rem; color: #94a3b8; }
.link-btn { background: none; border: none; color: #f59e0b; cursor: pointer; text-decoration: underline; font-weight: 600; }
.link-btn:hover { color: #fbbf24; }
.auth-msg { text-align: center; font-size: 0.85rem; padding: 0.5rem; border-radius: 0.5rem; margin-top: 0.5rem; background: rgba(34, 197, 94, 0.2); color: #4ade80; }
.auth-msg.error { background: rgba(239, 68, 68, 0.2); color: #f87171; }
.modal-enter-active, .modal-leave-active { transition: all 0.3s; }
.modal-enter-from, .modal-leave-to { opacity: 0; }
.modal-enter-from .modal-panel, .modal-leave-to .modal-panel { transform: scale(0.9); }
</style>
