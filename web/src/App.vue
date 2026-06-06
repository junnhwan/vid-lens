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

    <!-- 主布局 -->
    <div class="app-layout">
      <!-- 左侧边栏 -->
      <aside class="sidebar">
        <!-- 上传区 -->
        <section class="sidebar-section">
          <h3 class="section-title">📤 上传视频</h3>

          <div class="upload-card" :class="{ disabled: !user, uploading }"
               @click="guardLocalUploadClick"
               @dragover.prevent="dragging = true"
               @dragleave.prevent="dragging = false"
               @drop.prevent="handleDrop">
            <div class="upload-icon">📁</div>
            <p class="upload-label">本地上传</p>
            <input type="file" accept="video/*" :disabled="!user" @change="handleFileSelect" hidden ref="fileInput" />
            <button class="upload-btn" @click="triggerFileInput" :disabled="!user">
              {{ dragging ? '松手上传' : '选择文件' }}
            </button>
          </div>

          <div class="upload-card" :class="{ disabled: !user, uploading }" @click="guardUnauthedClick">
            <div class="upload-icon">🌐</div>
            <p class="upload-label">链接下载</p>
            <div class="url-input-group">
              <input v-model="videoUrl" placeholder="粘贴链接..." @keyup.enter="handleUrlUpload" :disabled="!user || uploading" />
              <button class="upload-btn" @click="handleUrlUpload" :disabled="!user || uploading || !videoUrl">开始</button>
            </div>
          </div>

          <!-- 上传状态 -->
          <div v-if="uploading" class="upload-status">
            <div class="spinner small"></div>
            <span>{{ uploadMsg }}</span>
          </div>
        </section>

        <!-- 统计卡片 -->
        <section v-if="user" class="sidebar-section">
          <h3 class="section-title">📊 数据概览</h3>
          <div class="stats-grid">
            <div class="stat-card">
              <div class="stat-value">{{ tasks.length }}</div>
              <div class="stat-label">总任务数</div>
            </div>
            <div class="stat-card">
              <div class="stat-value">{{ tasks.filter(t => t.status === 3).length }}</div>
              <div class="stat-label">已完成</div>
            </div>
            <div class="stat-card">
              <div class="stat-value">{{ tasks.filter(t => t.status < 3).length }}</div>
              <div class="stat-label">处理中</div>
            </div>
            <div class="stat-card">
              <div class="stat-value">{{ tasks.filter(t => t.status === 4).length }}</div>
              <div class="stat-label">失败</div>
            </div>
          </div>
        </section>
      </aside>

      <!-- 主内容区 -->
      <main class="content-area">
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
          <div v-for="t in filteredTasks" :key="t.id" class="task-card" @click="openTaskDrawer(t)">
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
          </div>
        </div>
      </section>

      <!-- 空状态 -->
      <div v-else class="empty-state">
        <div class="empty-icon">📦</div>
        <h3>还没有任务</h3>
        <p>从左侧上传你的第一个视频开始分析吧</p>
      </div>
    </main>
  </div>

    <!-- 任务详情抽屉 -->
    <transition name="drawer">
      <div v-if="selectedTask" class="drawer-backdrop" @click="closeDrawer">
        <div class="task-drawer" @click.stop>
          <div class="drawer-header">
            <h3>{{ selectedTask.filename }}</h3>
            <button class="drawer-close" @click="closeDrawer">×</button>
          </div>

          <div class="drawer-content">
            <div class="drawer-meta">
              <div class="meta-item">
                <span class="meta-label">创建时间</span>
                <span class="meta-value">{{ formatTime(selectedTask.created_at) }}</span>
              </div>
              <div class="meta-item">
                <span class="meta-label">文件大小</span>
                <span class="meta-value">{{ formatFileSize(selectedTask.file_size) }}</span>
              </div>
              <div class="meta-item">
                <span class="meta-label">状态</span>
                <span class="meta-status" :class="statusClass(selectedTask.status)">
                  {{ statusText(selectedTask.status) }}
                </span>
              </div>
            </div>

            <div class="drawer-actions">
              <button class="drawer-action-btn" @click.stop="doTranscribe(selectedTask)" :disabled="isActionDisabled(selectedTask)">
                <span class="btn-icon">📄</span> 提取文字
              </button>
              <button class="drawer-action-btn amber" @click.stop="doAnalyze(selectedTask)" :disabled="isActionDisabled(selectedTask)">
                <span class="btn-icon">🤖</span> AI 总结
              </button>
            </div>

            <div v-if="loading[selectedTask.id]" class="drawer-loading">
              <div class="spinner"></div>
              <span>处理中...</span>
            </div>

            <template v-else>
              <div v-if="failureMessage(selectedTask)" class="drawer-result-block error-block">
                <h4>❌ 处理失败</h4>
                <p class="error-text">{{ failureMessage(selectedTask) }}</p>
              </div>

              <div v-if="selectedTask.transcription?.content" class="drawer-result-block">
                <h4>📝 文字提取</h4>
                <pre class="result-text">{{ selectedTask.transcription.content }}</pre>
              </div>

              <div v-if="selectedTask.summary?.content" class="drawer-result-block">
                <h4>🤖 AI 总结</h4>
                <div class="result-markdown" v-html="renderMarkdown(selectedTask.summary.content)"></div>
              </div>
            </template>
          </div>
        </div>
      </div>
    </transition>

    <!-- Toast 消息 -->
    <transition name="toast">
      <div v-if="toast" class="toast" :class="{ error: toastIsError }">{{ toast }}</div>
    </transition>

    <!-- 登录/注册弹窗 -->
    <transition name="modal">
      <div v-if="showAuth" class="modal-backdrop" @mousedown.self="handleBackdropMouseDown">
        <div class="modal-panel">
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
import { buildStoredUser } from './authSession.js'
import { isTaskActionDisabled } from './taskActionPolicy.js'
import { needsResultDetail, needsTaskDetail, taskFailureMessage } from './taskDetailPolicy.js'
import { shouldStopPolling } from './taskPollingPolicy.js'

// 状态
const user = ref(null)
const tasks = ref([])
const videoUrl = ref('')
const uploading = ref(false)
const uploadMsg = ref('')
const dragging = ref(false)
const toast = ref('')
const toastIsError = ref(false)
const selectedTask = ref(null)
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

const formatFileSize = (bytes) => {
  if (!bytes) return '--'
  const units = ['B', 'KB', 'MB', 'GB']
  let size = bytes
  let unitIndex = 0
  while (size >= 1024 && unitIndex < units.length - 1) {
    size /= 1024
    unitIndex++
  }
  return `${size.toFixed(2)} ${units[unitIndex]}`
}

const statusClass = (s) => ['pending', 'queued', 'running', 'completed', 'failed'][s] || 'pending'
const statusText = (s) => ['待处理', '排队中', '处理中', '已完成', '失败'][s] || '未知'
const isActionDisabled = (t) => isTaskActionDisabled(t, loading.value[t.id])
const failureMessage = (t) => taskFailureMessage(t)
const renderMarkdown = (content) => marked.parse(content || '')

// 业务逻辑
const guardUnauthedClick = () => {
  if (!user.value) openAuth()
}

const guardLocalUploadClick = (e) => {
  if (user.value) return
  e.preventDefault()
  e.stopPropagation()
  openAuth()
}

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
    await api.uploadFile(file)
    showToast('上传成功')
    await fetchTasks()
  } catch (err) {
    showToast(err.message || '上传失败', true)
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
    await api.uploadByURL(videoUrl.value)
    showToast('下载成功')
    videoUrl.value = ''
    await fetchTasks()
  } catch (err) {
    showToast(err.message || '下载失败', true)
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
    tasks.value = res?.list || []
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
    if (selectedTask.value?.id === task.id) {
      selectedTask.value = null
    }
  } catch (err) {
    showToast('删除失败', true)
  }
}

const openTaskDrawer = async (task) => {
  selectedTask.value = task
  if (needsTaskDetail(task)) {
    await refreshTaskDetail(task.id)
  }
}

const closeDrawer = () => {
  selectedTask.value = null
}

const doTranscribe = async (task) => {
  if (task.transcription?.content || needsResultDetail(task, 'transcription')) {
    selectedTask.value = task
    await refreshTaskDetail(task.id)
    return
  }
  if (loading.value[task.id]) return
  loading.value[task.id] = true
  selectedTask.value = task
  try {
    await api.transcribe(task.id)
    startPolling(task.id, 'transcription')
  } catch (err) {
    showToast(err.message || '请求失败', true)
    loading.value[task.id] = false
  }
}

const doAnalyze = async (task) => {
  if (task.summary?.content || needsResultDetail(task, 'summary')) {
    selectedTask.value = task
    await refreshTaskDetail(task.id)
    return
  }
  if (loading.value[task.id]) return
  loading.value[task.id] = true
  selectedTask.value = task
  try {
    await api.analyze(task.id)
    startPolling(task.id, 'summary')
  } catch (err) {
    showToast(err.message || '请求失败', true)
    loading.value[task.id] = false
  }
}

const startPolling = (taskId, type) => {
  if (pollingTimers.value[taskId]) clearInterval(pollingTimers.value[taskId])
  const timer = setInterval(async () => {
    await fetchTasks()
    const task = tasks.value.find(t => t.id === taskId)
    if (!task) return
    if (shouldStopPolling(task, type)) {
      clearInterval(timer)
      delete pollingTimers.value[taskId]
      loading.value[taskId] = false
      if (task.status === 3) {
        await refreshTaskDetail(taskId)
        showToast('处理完成')
      } else {
        showToast(task.error_msg || '处理失败', true)
      }
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

const refreshTaskDetail = async (taskId) => {
  try {
    const detail = await api.getTask(taskId)
    const index = tasks.value.findIndex(t => t.id === taskId)
    if (index >= 0) {
      tasks.value[index] = { ...tasks.value[index], ...detail }
    }
    if (selectedTask.value?.id === taskId) {
      selectedTask.value = { ...selectedTask.value, ...detail }
    }
  } catch (err) {
    console.error(err)
  }
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
    const { username, password, nickname } = authForm.value
    const res = authMode.value === 'login'
      ? await api.login(username, password)
      : await api.register(username, password, nickname)
    if (authMode.value === 'login') {
      const sessionUser = buildStoredUser(res.user, res.token)
      user.value = sessionUser
      localStorage.setItem('user', JSON.stringify(sessionUser))
      localStorage.setItem('token', res.token)
      closeAuth()
      showToast(`欢迎回来，${sessionUser.nickname || sessionUser.username}`)
      await fetchTasks()
    } else {
      authMsg.value = '注册成功，请登录'
      authError.value = false
      setTimeout(() => switchAuthMode(), 1500)
    }
  } catch (err) {
    authMsg.value = err.message || '操作失败'
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

const handleBackdropMouseDown = (e) => {
  const startTarget = e.target
  const handleMouseUp = (upEvent) => {
    if (upEvent.target === startTarget && startTarget.classList.contains('modal-backdrop')) {
      closeAuth()
    }
    document.removeEventListener('mouseup', handleMouseUp)
  }
  document.addEventListener('mouseup', handleMouseUp)
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

<style>
/* 全局样式（无 scoped，影响整个页面） */
html, body {
  margin: 0;
  padding: 0;
  width: 100%;
  height: 100%;
  overflow-x: hidden;
}
</style>

<style scoped>
* { box-sizing: border-box; }

@import url('https://fonts.googleapis.com/css2?family=Noto+Sans+SC:wght@400;500;700&family=JetBrains+Mono:wght@400;600&display=swap');

#app {
  min-height: 100vh;
  background: #0a0e1a;
  background-image:
    radial-gradient(circle at 20% 30%, rgba(41, 98, 255, 0.08) 0%, transparent 50%),
    radial-gradient(circle at 80% 70%, rgba(212, 175, 55, 0.06) 0%, transparent 50%),
    radial-gradient(circle at 50% 50%, rgba(0, 0, 0, 0.4) 0%, transparent 100%);
  position: relative;
  color: #e8eef7;
  font-family: 'Noto Sans SC', -apple-system, sans-serif;
  overflow-x: hidden;
}

#app::before {
  content: '';
  position: fixed;
  inset: 0;
  background:
    repeating-linear-gradient(0deg, rgba(255, 255, 255, 0.01) 0px, transparent 1px, transparent 2px),
    repeating-linear-gradient(90deg, rgba(255, 255, 255, 0.01) 0px, transparent 1px, transparent 2px);
  background-size: 80px 80px;
  pointer-events: none;
  z-index: 1;
}

#app::after {
  content: '';
  position: fixed;
  top: -50%;
  left: -50%;
  width: 200%;
  height: 200%;
  background: radial-gradient(circle, rgba(212, 175, 55, 0.02) 1px, transparent 1px);
  background-size: 40px 40px;
  animation: subtleFloat 60s linear infinite;
  pointer-events: none;
  z-index: 1;
}

@keyframes subtleFloat {
  from { transform: translate(0, 0) rotate(0deg); }
  to { transform: translate(10%, 10%) rotate(5deg); }
}

/* 导航栏 */
.navbar {
  backdrop-filter: blur(24px) saturate(180%);
  background: rgba(10, 14, 26, 0.85);
  border-bottom: 1px solid rgba(212, 175, 55, 0.15);
  box-shadow: 0 4px 24px rgba(0, 0, 0, 0.4), inset 0 1px 0 rgba(255, 255, 255, 0.05);
  padding: 1.25rem 0;
  position: sticky;
  top: 0;
  z-index: 100;
  position: relative;
}

.navbar::after {
  content: '';
  position: absolute;
  bottom: 0;
  left: 50%;
  transform: translateX(-50%);
  width: 60%;
  height: 1px;
  background: linear-gradient(90deg, transparent, rgba(212, 175, 55, 0.5), transparent);
}

.nav-container {
  max-width: 1400px;
  margin: 0 auto;
  padding: 0 3rem;
  display: flex;
  justify-content: space-between;
  align-items: center;
  position: relative;
  z-index: 2;
}

.brand {
  display: flex;
  align-items: center;
  gap: 1rem;
  font-size: 1.75rem;
  font-weight: 700;
  letter-spacing: 0.5px;
  position: relative;
}

.mirror-icon {
  font-size: 2.5rem;
  color: #d4af37;
  filter: drop-shadow(0 0 12px rgba(212, 175, 55, 0.7)) drop-shadow(0 0 4px rgba(41, 98, 255, 0.3));
  animation: iconPulse 3s ease-in-out infinite;
  transform-origin: center;
}

@keyframes iconPulse {
  0%, 100% { transform: scale(1) rotate(0deg); filter: drop-shadow(0 0 12px rgba(212, 175, 55, 0.7)) drop-shadow(0 0 4px rgba(41, 98, 255, 0.3)); }
  50% { transform: scale(1.05) rotate(5deg); filter: drop-shadow(0 0 18px rgba(212, 175, 55, 0.9)) drop-shadow(0 0 8px rgba(41, 98, 255, 0.5)); }
}

.brand-text {
  background: linear-gradient(135deg, #d4af37 0%, #f4e4a6 50%, #d4af37 100%);
  background-size: 200% auto;
  -webkit-background-clip: text;
  -webkit-text-fill-color: transparent;
  background-clip: text;
  animation: shimmer 4s linear infinite;
  position: relative;
  text-shadow: 0 0 20px rgba(212, 175, 55, 0.3);
}

@keyframes shimmer {
  to { background-position: 200% center; }
}

.brand-text .en {
  font-size: 0.65rem;
  opacity: 0.8;
  margin-left: 0.5rem;
  font-family: 'JetBrains Mono', monospace;
  letter-spacing: 1px;
  font-weight: 400;
}

.nav-right { display: flex; align-items: center; gap: 1.25rem; }
.user-badge {
  display: flex;
  align-items: center;
  gap: 0.75rem;
  padding: 0.65rem 1.25rem;
  background: linear-gradient(135deg, rgba(212, 175, 55, 0.08), rgba(41, 98, 255, 0.08));
  backdrop-filter: blur(12px);
  border-radius: 2rem;
  border: 1px solid rgba(212, 175, 55, 0.25);
  box-shadow: 0 2px 12px rgba(212, 175, 55, 0.15), inset 0 1px 0 rgba(255, 255, 255, 0.1);
  transition: all 0.3s cubic-bezier(0.4, 0, 0.2, 1);
}
.user-badge:hover {
  border-color: rgba(212, 175, 55, 0.45);
  box-shadow: 0 4px 20px rgba(212, 175, 55, 0.25), inset 0 1px 0 rgba(255, 255, 255, 0.15);
  transform: translateY(-1px);
}
.user-avatar {
  width: 2.25rem;
  height: 2.25rem;
  border-radius: 50%;
  background: linear-gradient(135deg, #d4af37, #f4e4a6);
  display: flex;
  align-items: center;
  justify-content: center;
  font-weight: 700;
  color: #0a0e1a;
  box-shadow: 0 0 12px rgba(212, 175, 55, 0.5), inset 0 1px 2px rgba(255, 255, 255, 0.3);
  font-size: 0.95rem;
}
.user-name { font-size: 0.95rem; font-weight: 500; color: #e8eef7; }
.btn-text {
  background: none;
  border: none;
  color: #8b95a8;
  cursor: pointer;
  font-size: 0.9rem;
  font-weight: 500;
  transition: all 0.3s;
  padding: 0.5rem 0.75rem;
  border-radius: 0.5rem;
}
.btn-text:hover {
  color: #d4af37;
  background: rgba(212, 175, 55, 0.08);
}
.btn-amber {
  background: linear-gradient(135deg, #d4af37 0%, #f4e4a6 50%, #d4af37 100%);
  background-size: 200% auto;
  color: #0a0e1a;
  border: none;
  padding: 0.75rem 1.75rem;
  border-radius: 0.75rem;
  font-weight: 600;
  cursor: pointer;
  transition: all 0.4s cubic-bezier(0.4, 0, 0.2, 1);
  box-shadow: 0 4px 16px rgba(212, 175, 55, 0.3), inset 0 1px 0 rgba(255, 255, 255, 0.3);
  font-size: 0.95rem;
  letter-spacing: 0.5px;
  position: relative;
  overflow: hidden;
}
.btn-amber::before {
  content: '';
  position: absolute;
  inset: 0;
  background: linear-gradient(135deg, transparent, rgba(255, 255, 255, 0.2), transparent);
  transform: translateX(-100%);
  transition: transform 0.6s;
}
.btn-amber:hover::before {
  transform: translateX(100%);
}
.btn-amber:hover {
  transform: translateY(-2px);
  box-shadow: 0 6px 24px rgba(212, 175, 55, 0.5), inset 0 1px 0 rgba(255, 255, 255, 0.4);
  background-position: 200% center;
}
.btn-amber.full { width: 100%; }

/* 主容器 */
.app-layout {
  display: flex;
  min-height: calc(100vh - 80px);
  max-width: 1600px;
  margin: 0 auto;
  padding: 0;
  position: relative;
  z-index: 2;
}

/* 侧边栏 */
.sidebar {
  width: 320px;
  flex-shrink: 0;
  padding: 2rem 1.5rem;
  backdrop-filter: blur(20px) saturate(180%);
  background: linear-gradient(135deg, rgba(10, 14, 26, 0.4), rgba(15, 25, 45, 0.3));
  border-right: 1px solid rgba(212, 175, 55, 0.15);
  overflow-y: auto;
  scrollbar-width: thin;
  scrollbar-color: rgba(212, 175, 55, 0.3) transparent;
}

.sidebar::-webkit-scrollbar {
  width: 6px;
}

.sidebar::-webkit-scrollbar-track {
  background: transparent;
}

.sidebar::-webkit-scrollbar-thumb {
  background: rgba(212, 175, 55, 0.3);
  border-radius: 3px;
}

.sidebar::-webkit-scrollbar-thumb:hover {
  background: rgba(212, 175, 55, 0.5);
}

.sidebar-section {
  margin-bottom: 2.5rem;
}

.sidebar-section:last-child {
  margin-bottom: 0;
}

.section-title {
  font-size: 1.1rem;
  font-weight: 700;
  background: linear-gradient(135deg, #d4af37, #f4e4a6);
  -webkit-background-clip: text;
  -webkit-text-fill-color: transparent;
  background-clip: text;
  margin-bottom: 1.25rem;
  letter-spacing: 0.5px;
}

/* 上传卡片（紧凑版） */
.upload-card {
  backdrop-filter: blur(20px) saturate(180%);
  background: linear-gradient(135deg, rgba(15, 25, 45, 0.5), rgba(20, 30, 50, 0.4));
  border: 1px solid rgba(212, 175, 55, 0.2);
  border-radius: 1rem;
  padding: 1.5rem;
  text-align: center;
  transition: all 0.3s cubic-bezier(0.4, 0, 0.2, 1);
  position: relative;
  overflow: hidden;
  cursor: pointer;
  box-shadow: 0 4px 16px rgba(0, 0, 0, 0.2), inset 0 1px 0 rgba(255, 255, 255, 0.03);
  margin-bottom: 1rem;
}

.upload-card:last-child {
  margin-bottom: 0;
}

.upload-card::before {
  content: '';
  position: absolute;
  top: -50%;
  left: -50%;
  width: 200%;
  height: 200%;
  background: radial-gradient(circle, rgba(212, 175, 55, 0.08) 0%, transparent 70%);
  opacity: 0;
  transition: opacity 0.4s, transform 0.6s;
  transform: scale(0.8);
}

.upload-card:hover:not(.disabled)::before {
  opacity: 1;
  transform: scale(1);
}

.upload-card:hover:not(.disabled) {
  transform: translateY(-4px);
  box-shadow: 0 8px 24px rgba(212, 175, 55, 0.2), 0 0 0 1px rgba(212, 175, 55, 0.4), inset 0 1px 0 rgba(255, 255, 255, 0.08);
  border-color: rgba(212, 175, 55, 0.4);
}

.upload-card.disabled {
  opacity: 0.4;
  cursor: not-allowed;
  filter: grayscale(0.5);
}

.upload-card.uploading {
  pointer-events: none;
  opacity: 0.6;
}

.upload-icon {
  font-size: 2.5rem;
  margin-bottom: 0.75rem;
  filter: drop-shadow(0 2px 8px rgba(212, 175, 55, 0.3));
  position: relative;
  z-index: 1;
}

.upload-label {
  font-size: 0.95rem;
  font-weight: 600;
  color: #d4af37;
  margin-bottom: 1rem;
  position: relative;
  z-index: 1;
}

.upload-btn {
  background: linear-gradient(135deg, rgba(212, 175, 55, 0.15), rgba(41, 98, 255, 0.1));
  border: 1px solid rgba(212, 175, 55, 0.3);
  color: #d4af37;
  padding: 0.6rem 1.5rem;
  border-radius: 0.65rem;
  cursor: pointer;
  transition: all 0.3s cubic-bezier(0.4, 0, 0.2, 1);
  font-weight: 600;
  font-size: 0.9rem;
  backdrop-filter: blur(8px);
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.2), inset 0 1px 0 rgba(255, 255, 255, 0.1);
  position: relative;
  z-index: 1;
  letter-spacing: 0.5px;
  width: 100%;
}

.upload-btn:hover:not(:disabled) {
  background: linear-gradient(135deg, rgba(212, 175, 55, 0.25), rgba(41, 98, 255, 0.15));
  border-color: #d4af37;
  box-shadow: 0 4px 12px rgba(212, 175, 55, 0.3), inset 0 1px 0 rgba(255, 255, 255, 0.15);
  transform: translateY(-2px);
}

.upload-btn:disabled {
  opacity: 0.4;
  cursor: not-allowed;
}

.url-input-group {
  display: flex;
  flex-direction: column;
  gap: 0.75rem;
  margin-top: 0.75rem;
  position: relative;
  z-index: 1;
}

.url-input-group input {
  width: 100%;
  background: rgba(10, 14, 26, 0.6);
  border: 1px solid rgba(139, 149, 168, 0.2);
  border-radius: 0.65rem;
  padding: 0.65rem 1rem;
  color: #e8eef7;
  outline: none;
  transition: all 0.3s;
  backdrop-filter: blur(8px);
  font-size: 0.9rem;
}

.url-input-group input:focus {
  border-color: #d4af37;
  box-shadow: 0 0 0 3px rgba(212, 175, 55, 0.15), 0 2px 8px rgba(212, 175, 55, 0.2);
}

.url-input-group input::placeholder {
  color: #5a6477;
}

/* 统计网格 */
.stats-grid {
  display: grid;
  grid-template-columns: repeat(2, 1fr);
  gap: 0.75rem;
}

.stat-card {
  backdrop-filter: blur(20px) saturate(180%);
  background: linear-gradient(135deg, rgba(15, 25, 45, 0.5), rgba(20, 30, 50, 0.4));
  border: 1px solid rgba(139, 149, 168, 0.2);
  border-radius: 0.875rem;
  padding: 1rem;
  text-align: center;
  transition: all 0.3s cubic-bezier(0.4, 0, 0.2, 1);
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.1), inset 0 1px 0 rgba(255, 255, 255, 0.03);
}

.stat-card:hover {
  transform: translateY(-2px);
  border-color: rgba(212, 175, 55, 0.3);
  box-shadow: 0 4px 16px rgba(212, 175, 55, 0.15), inset 0 1px 0 rgba(255, 255, 255, 0.05);
}

.stat-value {
  font-size: 1.75rem;
  font-weight: 700;
  background: linear-gradient(135deg, #d4af37, #f4e4a6);
  -webkit-background-clip: text;
  -webkit-text-fill-color: transparent;
  background-clip: text;
  font-family: 'JetBrains Mono', monospace;
  margin-bottom: 0.25rem;
}

.stat-label {
  font-size: 0.8rem;
  color: #8b95a8;
  font-weight: 500;
}

/* 主内容区 */
.content-area {
  flex: 1;
  padding: 2rem 3rem;
  overflow-y: auto;
  scrollbar-width: thin;
  scrollbar-color: rgba(212, 175, 55, 0.3) transparent;
}

.content-area::-webkit-scrollbar {
  width: 8px;
}

.content-area::-webkit-scrollbar-track {
  background: transparent;
}

.content-area::-webkit-scrollbar-thumb {
  background: rgba(212, 175, 55, 0.3);
  border-radius: 4px;
}

.content-area::-webkit-scrollbar-thumb:hover {
  background: rgba(212, 175, 55, 0.5);
}

/* 空状态 */
.empty-state {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  min-height: 400px;
  text-align: center;
  opacity: 0.6;
}

.empty-icon {
  font-size: 4rem;
  margin-bottom: 1rem;
  filter: drop-shadow(0 4px 12px rgba(212, 175, 55, 0.2));
}

.empty-state h3 {
  font-size: 1.5rem;
  margin-bottom: 0.5rem;
  background: linear-gradient(135deg, #d4af37, #f4e4a6);
  -webkit-background-clip: text;
  -webkit-text-fill-color: transparent;
  background-clip: text;
}

.empty-state p {
  color: #8b95a8;
  font-size: 0.95rem;
}

/* 上传状态 */
.upload-status {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 0.75rem;
  padding: 1rem;
  backdrop-filter: blur(20px) saturate(180%);
  background: linear-gradient(135deg, rgba(15, 25, 45, 0.7), rgba(20, 30, 50, 0.5));
  border: 1px solid rgba(212, 175, 55, 0.3);
  border-radius: 0.75rem;
  margin-top: 1rem;
  box-shadow: 0 4px 16px rgba(0, 0, 0, 0.2), inset 0 1px 0 rgba(255, 255, 255, 0.05);
  font-size: 0.9rem;
  font-weight: 500;
  color: #d4af37;
}

/* Toast */
.toast {
  position: fixed;
  top: 2.5rem;
  right: 2.5rem;
  padding: 1.25rem 2rem;
  backdrop-filter: blur(24px) saturate(180%);
  background: linear-gradient(135deg, rgba(34, 197, 94, 0.95), rgba(22, 163, 74, 0.95));
  border-radius: 1rem;
  font-weight: 600;
  z-index: 1000;
  box-shadow: 0 8px 32px rgba(0, 0, 0, 0.4), 0 0 0 1px rgba(255, 255, 255, 0.1), inset 0 1px 0 rgba(255, 255, 255, 0.2);
  border: 1px solid rgba(255, 255, 255, 0.15);
  font-size: 0.95rem;
  letter-spacing: 0.3px;
  color: #fff;
}
.toast.error {
  background: linear-gradient(135deg, rgba(239, 68, 68, 0.95), rgba(220, 38, 38, 0.95));
}
.toast-enter-active, .toast-leave-active {
  transition: all 0.4s cubic-bezier(0.4, 0, 0.2, 1);
}
.toast-enter-from, .toast-leave-to {
  opacity: 0;
  transform: translateX(100%) scale(0.8);
}

/* 任务区 */
.tasks-section {
  animation: fadeInUp 0.6s ease-out;
}
@keyframes fadeInUp {
  from { opacity: 0; transform: translateY(20px); }
  to { opacity: 1; transform: translateY(0); }
}
.section-header {
  display: flex;
  align-items: center;
  gap: 1.5rem;
  margin-bottom: 2rem;
  flex-wrap: wrap;
  padding-bottom: 1.25rem;
  border-bottom: 1px solid rgba(212, 175, 55, 0.1);
  position: relative;
}
.section-header::after {
  content: '';
  position: absolute;
  bottom: -1px;
  left: 0;
  width: 100px;
  height: 2px;
  background: linear-gradient(90deg, #d4af37, transparent);
}
.section-header h2 {
  font-size: 1.75rem;
  font-weight: 700;
  background: linear-gradient(135deg, #d4af37, #f4e4a6);
  -webkit-background-clip: text;
  -webkit-text-fill-color: transparent;
  background-clip: text;
  letter-spacing: 1px;
}
.filter-tabs {
  display: flex;
  gap: 0.75rem;
  flex: 1;
}
.tab {
  background: linear-gradient(135deg, rgba(15, 25, 45, 0.4), rgba(20, 30, 50, 0.3));
  border: 1px solid rgba(139, 149, 168, 0.2);
  padding: 0.65rem 1.25rem;
  border-radius: 0.75rem;
  color: #8b95a8;
  cursor: pointer;
  transition: all 0.3s cubic-bezier(0.4, 0, 0.2, 1);
  display: flex;
  align-items: center;
  gap: 0.65rem;
  backdrop-filter: blur(8px);
  font-weight: 500;
  font-size: 0.9rem;
  letter-spacing: 0.3px;
}
.tab:hover {
  border-color: rgba(212, 175, 55, 0.4);
  color: #d4af37;
  background: linear-gradient(135deg, rgba(212, 175, 55, 0.08), rgba(41, 98, 255, 0.05));
  transform: translateY(-2px);
}
.tab.active {
  background: linear-gradient(135deg, rgba(212, 175, 55, 0.15), rgba(41, 98, 255, 0.1));
  border-color: rgba(212, 175, 55, 0.5);
  color: #d4af37;
  box-shadow: 0 2px 8px rgba(212, 175, 55, 0.2), inset 0 1px 0 rgba(255, 255, 255, 0.1);
}
.tab-count {
  background: rgba(139, 149, 168, 0.2);
  padding: 0.2rem 0.6rem;
  border-radius: 0.4rem;
  font-size: 0.75rem;
  font-weight: 600;
  font-family: 'JetBrains Mono', monospace;
}
.tab.active .tab-count {
  background: rgba(212, 175, 55, 0.25);
  color: #f4e4a6;
}
.search-box {
  padding: 0.75rem 1.25rem;
  background: rgba(10, 14, 26, 0.5);
  border: 1px solid rgba(139, 149, 168, 0.2);
  border-radius: 0.75rem;
  color: #e8eef7;
  outline: none;
  min-width: 240px;
  transition: all 0.3s;
  backdrop-filter: blur(8px);
  font-size: 0.95rem;
}
.search-box:focus {
  border-color: #d4af37;
  box-shadow: 0 0 0 3px rgba(212, 175, 55, 0.15), 0 2px 8px rgba(212, 175, 55, 0.2);
}
.search-box::placeholder {
  color: #5a6477;
}

/* 任务列表 */
.tasks-list {
  display: flex;
  flex-direction: column;
  gap: 1.5rem;
}
.task-card {
  backdrop-filter: blur(20px) saturate(180%);
  background: linear-gradient(135deg, rgba(15, 25, 45, 0.6), rgba(20, 30, 50, 0.4));
  border: 1px solid rgba(139, 149, 168, 0.2);
  border-radius: 1.5rem;
  padding: 2rem;
  cursor: pointer;
  transition: all 0.4s cubic-bezier(0.4, 0, 0.2, 1);
  position: relative;
  overflow: hidden;
  box-shadow: 0 4px 16px rgba(0, 0, 0, 0.2), inset 0 1px 0 rgba(255, 255, 255, 0.03);
}
.task-card::before {
  content: '';
  position: absolute;
  top: 0;
  left: 0;
  width: 4px;
  height: 100%;
  background: linear-gradient(180deg, #d4af37, #2962ff);
  opacity: 0;
  transition: opacity 0.3s;
}
.task-card:hover::before {
  opacity: 1;
}
.task-card:hover {
  border-color: rgba(212, 175, 55, 0.4);
  box-shadow: 0 8px 32px rgba(212, 175, 55, 0.15), 0 0 0 1px rgba(212, 175, 55, 0.2), inset 0 1px 0 rgba(255, 255, 255, 0.05);
  transform: translateY(-4px);
}
.task-delete {
  position: absolute;
  top: 1.25rem;
  right: 1.25rem;
  background: rgba(239, 68, 68, 0.1);
  border: 1px solid rgba(239, 68, 68, 0.3);
  width: 2.25rem;
  height: 2.25rem;
  border-radius: 50%;
  color: #ef4444;
  font-size: 1.25rem;
  cursor: pointer;
  opacity: 0;
  transition: all 0.3s cubic-bezier(0.4, 0, 0.2, 1);
  display: flex;
  align-items: center;
  justify-content: center;
  backdrop-filter: blur(8px);
}
.task-card:hover .task-delete {
  opacity: 1;
}
.task-delete:hover {
  background: rgba(239, 68, 68, 0.2);
  border-color: #ef4444;
  transform: rotate(90deg) scale(1.1);
  box-shadow: 0 2px 8px rgba(239, 68, 68, 0.3);
}
.task-header {
  display: flex;
  gap: 1.25rem;
  margin-bottom: 1.5rem;
}
.task-icon {
  width: 3.5rem;
  height: 3.5rem;
  background: linear-gradient(135deg, rgba(212, 175, 55, 0.12), rgba(41, 98, 255, 0.08));
  border: 1px solid rgba(212, 175, 55, 0.3);
  border-radius: 1rem;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 1.75rem;
  flex-shrink: 0;
  box-shadow: 0 4px 12px rgba(212, 175, 55, 0.15), inset 0 1px 0 rgba(255, 255, 255, 0.1);
  transition: all 0.3s;
}
.task-card:hover .task-icon {
  transform: scale(1.05);
  box-shadow: 0 6px 16px rgba(212, 175, 55, 0.25), inset 0 1px 0 rgba(255, 255, 255, 0.15);
}
.task-info {
  flex: 1;
  min-width: 0;
}
.task-name {
  font-size: 1.15rem;
  font-weight: 600;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  color: #e8eef7;
  letter-spacing: 0.3px;
  margin-bottom: 0.5rem;
}
.task-meta {
  display: flex;
  gap: 0.75rem;
  font-size: 0.875rem;
  color: #8b95a8;
  margin-top: 0.5rem;
  align-items: center;
}
.meta-dot {
  opacity: 0.4;
  font-size: 0.6rem;
}
.meta-time {
  font-family: 'JetBrains Mono', monospace;
  font-size: 0.85rem;
}
.meta-status {
  padding: 0.25rem 0.75rem;
  border-radius: 0.5rem;
  font-weight: 600;
  font-size: 0.8rem;
  letter-spacing: 0.5px;
  text-transform: uppercase;
  backdrop-filter: blur(8px);
  border: 1px solid;
}
.meta-status.pending {
  background: rgba(139, 149, 168, 0.15);
  color: #8b95a8;
  border-color: rgba(139, 149, 168, 0.3);
}
.meta-status.queued {
  background: rgba(41, 98, 255, 0.15);
  color: #5b8fff;
  border-color: rgba(41, 98, 255, 0.3);
  box-shadow: 0 0 12px rgba(41, 98, 255, 0.2);
}
.meta-status.running {
  background: rgba(212, 175, 55, 0.15);
  color: #f4e4a6;
  border-color: rgba(212, 175, 55, 0.3);
  box-shadow: 0 0 12px rgba(212, 175, 55, 0.2);
  animation: statusPulse 2s ease-in-out infinite;
}
@keyframes statusPulse {
  0%, 100% { box-shadow: 0 0 12px rgba(212, 175, 55, 0.2); }
  50% { box-shadow: 0 0 20px rgba(212, 175, 55, 0.4); }
}
.meta-status.completed {
  background: rgba(34, 197, 94, 0.15);
  color: #4ade80;
  border-color: rgba(34, 197, 94, 0.3);
  box-shadow: 0 0 12px rgba(34, 197, 94, 0.2);
}
.meta-status.failed {
  background: rgba(239, 68, 68, 0.15);
  color: #f87171;
  border-color: rgba(239, 68, 68, 0.3);
  box-shadow: 0 0 12px rgba(239, 68, 68, 0.2);
}

/* 任务操作按钮 */
.task-actions {
  display: flex;
  gap: 1rem;
}

.action-btn {
  flex: 1;
  background: linear-gradient(135deg, rgba(15, 25, 45, 0.5), rgba(20, 30, 50, 0.4));
  border: 1px solid rgba(139, 149, 168, 0.25);
  padding: 0.9rem 1.5rem;
  border-radius: 0.875rem;
  color: #8b95a8;
  cursor: pointer;
  transition: all 0.3s cubic-bezier(0.4, 0, 0.2, 1);
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 0.65rem;
  font-weight: 600;
  font-size: 0.95rem;
  backdrop-filter: blur(8px);
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.1), inset 0 1px 0 rgba(255, 255, 255, 0.03);
  letter-spacing: 0.3px;
}

.action-btn:hover:not(:disabled) {
  border-color: rgba(212, 175, 55, 0.5);
  color: #d4af37;
  background: linear-gradient(135deg, rgba(212, 175, 55, 0.1), rgba(41, 98, 255, 0.08));
  transform: translateY(-2px);
  box-shadow: 0 4px 16px rgba(212, 175, 55, 0.2), inset 0 1px 0 rgba(255, 255, 255, 0.08);
}

.action-btn.amber {
  border-color: rgba(212, 175, 55, 0.35);
  color: #d4af37;
  background: linear-gradient(135deg, rgba(212, 175, 55, 0.12), rgba(41, 98, 255, 0.08));
  box-shadow: 0 2px 12px rgba(212, 175, 55, 0.15), inset 0 1px 0 rgba(255, 255, 255, 0.05);
}

.action-btn.amber:hover:not(:disabled) {
  border-color: rgba(212, 175, 55, 0.6);
  background: linear-gradient(135deg, rgba(212, 175, 55, 0.2), rgba(41, 98, 255, 0.12));
  box-shadow: 0 4px 20px rgba(212, 175, 55, 0.3), inset 0 1px 0 rgba(255, 255, 255, 0.1);
}

.action-btn:disabled {
  opacity: 0.35;
  cursor: not-allowed;
  filter: grayscale(0.5);
}

.btn-icon {
  font-size: 1.35rem;
  position: relative;
  z-index: 1;
}

/* 结果文本样式 */
.error-text {
  background: linear-gradient(135deg, rgba(239, 68, 68, 0.15), rgba(220, 38, 38, 0.1));
  border: 1px solid rgba(239, 68, 68, 0.3);
  color: #fecaca;
  padding: 1.25rem;
  border-radius: 0.875rem;
  line-height: 1.7;
  white-space: pre-wrap;
  font-size: 0.95rem;
  backdrop-filter: blur(8px);
  box-shadow: 0 4px 16px rgba(239, 68, 68, 0.1), inset 0 1px 0 rgba(255, 255, 255, 0.05);
}

.result-text {
  background: rgba(10, 14, 26, 0.6);
  padding: 1.5rem;
  border-radius: 0.875rem;
  font-size: 0.95rem;
  line-height: 1.8;
  white-space: pre-wrap;
  color: #b8c5db;
  max-height: 400px;
  overflow-y: auto;
  border: 1px solid rgba(139, 149, 168, 0.15);
  backdrop-filter: blur(8px);
  box-shadow: inset 0 2px 8px rgba(0, 0, 0, 0.3);
  font-family: 'JetBrains Mono', monospace;
  scrollbar-width: thin;
  scrollbar-color: rgba(212, 175, 55, 0.3) rgba(10, 14, 26, 0.5);
}

.result-text::-webkit-scrollbar {
  width: 8px;
}

.result-text::-webkit-scrollbar-track {
  background: rgba(10, 14, 26, 0.5);
  border-radius: 4px;
}

.result-text::-webkit-scrollbar-thumb {
  background: rgba(212, 175, 55, 0.3);
  border-radius: 4px;
}

.result-text::-webkit-scrollbar-thumb:hover {
  background: rgba(212, 175, 55, 0.5);
}

.result-markdown {
  background: rgba(10, 14, 26, 0.6);
  padding: 1.5rem;
  border-radius: 0.875rem;
  line-height: 1.9;
  max-height: 500px;
  overflow-y: auto;
  border: 1px solid rgba(139, 149, 168, 0.15);
  backdrop-filter: blur(8px);
  box-shadow: inset 0 2px 8px rgba(0, 0, 0, 0.3);
  scrollbar-width: thin;
  scrollbar-color: rgba(212, 175, 55, 0.3) rgba(10, 14, 26, 0.5);
}

.result-markdown::-webkit-scrollbar {
  width: 8px;
}

.result-markdown::-webkit-scrollbar-track {
  background: rgba(10, 14, 26, 0.5);
  border-radius: 4px;
}

.result-markdown::-webkit-scrollbar-thumb {
  background: rgba(212, 175, 55, 0.3);
  border-radius: 4px;
}

.result-markdown::-webkit-scrollbar-thumb:hover {
  background: rgba(212, 175, 55, 0.5);
}

.result-markdown :deep(h2), .result-markdown :deep(h3) {
  background: linear-gradient(135deg, #d4af37, #f4e4a6);
  -webkit-background-clip: text;
  -webkit-text-fill-color: transparent;
  background-clip: text;
  margin-top: 1.5rem;
  margin-bottom: 0.75rem;
  font-weight: 700;
  letter-spacing: 0.3px;
}

.result-markdown :deep(p) {
  margin-bottom: 1rem;
  color: #b8c5db;
  font-size: 0.95rem;
}

.result-markdown :deep(strong) {
  color: #f4e4a6;
  font-weight: 600;
}

.result-markdown :deep(ul) {
  padding-left: 2rem;
  margin-bottom: 1rem;
}

.result-markdown :deep(li) {
  margin-bottom: 0.65rem;
  color: #b8c5db;
  position: relative;
}

.result-markdown :deep(li::marker) {
  color: #d4af37;
}

.result-markdown :deep(code) {
  background: rgba(212, 175, 55, 0.1);
  padding: 0.2rem 0.5rem;
  border-radius: 0.375rem;
  font-family: 'JetBrains Mono', monospace;
  font-size: 0.875rem;
  color: #f4e4a6;
  border: 1px solid rgba(212, 175, 55, 0.2);
}

/* Spinner */
.spinner {
  width: 1.75rem;
  height: 1.75rem;
  border: 3px solid rgba(212, 175, 55, 0.15);
  border-top-color: #d4af37;
  border-radius: 50%;
  animation: spin 0.8s linear infinite;
  box-shadow: 0 0 12px rgba(212, 175, 55, 0.3);
}
.spinner.small {
  width: 1.25rem;
  height: 1.25rem;
  border-width: 2.5px;
}
@keyframes spin {
  to { transform: rotate(360deg); }
}

/* 任务详情抽屉 */
.drawer-backdrop {
  position: fixed;
  inset: 0;
  background: rgba(0, 0, 0, 0.6);
  backdrop-filter: blur(8px);
  z-index: 1001;
  display: flex;
  justify-content: flex-end;
}

.task-drawer {
  width: 600px;
  max-width: 90vw;
  height: 100vh;
  background: linear-gradient(135deg, rgba(10, 14, 26, 0.98), rgba(15, 25, 45, 0.98));
  backdrop-filter: blur(32px) saturate(180%);
  border-left: 1px solid rgba(212, 175, 55, 0.3);
  box-shadow: -8px 0 32px rgba(0, 0, 0, 0.6);
  display: flex;
  flex-direction: column;
  overflow: hidden;
}

.drawer-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 2rem;
  border-bottom: 1px solid rgba(212, 175, 55, 0.15);
  background: linear-gradient(135deg, rgba(15, 25, 45, 0.6), rgba(20, 30, 50, 0.4));
}

.drawer-header h3 {
  font-size: 1.25rem;
  font-weight: 700;
  background: linear-gradient(135deg, #d4af37, #f4e4a6);
  -webkit-background-clip: text;
  -webkit-text-fill-color: transparent;
  background-clip: text;
  flex: 1;
  padding-right: 2rem;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.drawer-close {
  background: rgba(239, 68, 68, 0.1);
  border: 1px solid rgba(239, 68, 68, 0.3);
  width: 2.5rem;
  height: 2.5rem;
  border-radius: 50%;
  color: #ef4444;
  font-size: 1.5rem;
  cursor: pointer;
  transition: all 0.3s cubic-bezier(0.4, 0, 0.2, 1);
  display: flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
}

.drawer-close:hover {
  background: rgba(239, 68, 68, 0.2);
  border-color: #ef4444;
  transform: rotate(90deg);
  box-shadow: 0 4px 16px rgba(239, 68, 68, 0.3);
}

.drawer-content {
  flex: 1;
  overflow-y: auto;
  padding: 2rem;
  scrollbar-width: thin;
  scrollbar-color: rgba(212, 175, 55, 0.3) rgba(10, 14, 26, 0.5);
}

.drawer-content::-webkit-scrollbar {
  width: 8px;
}

.drawer-content::-webkit-scrollbar-track {
  background: rgba(10, 14, 26, 0.5);
}

.drawer-content::-webkit-scrollbar-thumb {
  background: rgba(212, 175, 55, 0.3);
  border-radius: 4px;
}

.drawer-content::-webkit-scrollbar-thumb:hover {
  background: rgba(212, 175, 55, 0.5);
}

.drawer-meta {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(150px, 1fr));
  gap: 1rem;
  margin-bottom: 2rem;
  padding: 1.5rem;
  background: linear-gradient(135deg, rgba(15, 25, 45, 0.5), rgba(20, 30, 50, 0.4));
  border: 1px solid rgba(139, 149, 168, 0.2);
  border-radius: 1rem;
}

.meta-item {
  display: flex;
  flex-direction: column;
  gap: 0.5rem;
}

.meta-label {
  font-size: 0.8rem;
  color: #8b95a8;
  font-weight: 500;
}

.meta-value {
  font-size: 0.95rem;
  color: #e8eef7;
  font-weight: 600;
  font-family: 'JetBrains Mono', monospace;
}

.drawer-actions {
  display: flex;
  gap: 1rem;
  margin-bottom: 2rem;
}

.drawer-action-btn {
  flex: 1;
  background: linear-gradient(135deg, rgba(15, 25, 45, 0.5), rgba(20, 30, 50, 0.4));
  border: 1px solid rgba(139, 149, 168, 0.25);
  padding: 1rem 1.5rem;
  border-radius: 0.875rem;
  color: #8b95a8;
  cursor: pointer;
  transition: all 0.3s cubic-bezier(0.4, 0, 0.2, 1);
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 0.65rem;
  font-weight: 600;
  font-size: 0.95rem;
  backdrop-filter: blur(8px);
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.1);
}

.drawer-action-btn:hover:not(:disabled) {
  border-color: rgba(212, 175, 55, 0.5);
  color: #d4af37;
  background: linear-gradient(135deg, rgba(212, 175, 55, 0.1), rgba(41, 98, 255, 0.08));
  transform: translateY(-2px);
  box-shadow: 0 4px 16px rgba(212, 175, 55, 0.2);
}

.drawer-action-btn.amber {
  border-color: rgba(212, 175, 55, 0.35);
  color: #d4af37;
  background: linear-gradient(135deg, rgba(212, 175, 55, 0.12), rgba(41, 98, 255, 0.08));
}

.drawer-action-btn:disabled {
  opacity: 0.35;
  cursor: not-allowed;
}

.drawer-loading {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 1rem;
  padding: 2rem;
  background: linear-gradient(135deg, rgba(15, 25, 45, 0.5), rgba(20, 30, 50, 0.4));
  border: 1px solid rgba(212, 175, 55, 0.2);
  border-radius: 1rem;
  color: #d4af37;
  font-weight: 500;
}

.drawer-result-block {
  margin-bottom: 2rem;
  animation: resultFadeIn 0.5s ease-out;
}

.drawer-result-block:last-child {
  margin-bottom: 0;
}

.drawer-result-block h4 {
  font-size: 1.1rem;
  margin-bottom: 1rem;
  background: linear-gradient(135deg, #d4af37, #f4e4a6);
  -webkit-background-clip: text;
  -webkit-text-fill-color: transparent;
  background-clip: text;
  font-weight: 700;
  letter-spacing: 0.5px;
}

.drawer-result-block.error-block h4 {
  background: linear-gradient(135deg, #f87171, #fca5a5);
  -webkit-background-clip: text;
  -webkit-text-fill-color: transparent;
  background-clip: text;
}

.drawer-enter-active, .drawer-leave-active {
  transition: opacity 0.3s ease;
}

.drawer-enter-active .task-drawer,
.drawer-leave-active .task-drawer {
  transition: transform 0.3s cubic-bezier(0.4, 0, 0.2, 1);
}

.drawer-enter-from, .drawer-leave-to {
  opacity: 0;
}

.drawer-enter-from .task-drawer {
  transform: translateX(100%);
}

.drawer-leave-to .task-drawer {
  transform: translateX(100%);
}

/* 登录弹窗 */
.modal-backdrop {
  position: fixed;
  inset: 0;
  background: rgba(0, 0, 0, 0.8);
  backdrop-filter: blur(12px);
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: 1000;
}
.modal-panel {
  width: 90%;
  max-width: 450px;
  backdrop-filter: blur(32px) saturate(180%);
  background: linear-gradient(135deg, rgba(15, 25, 45, 0.95), rgba(20, 30, 50, 0.9));
  border: 1px solid rgba(212, 175, 55, 0.3);
  border-radius: 1.75rem;
  padding: 2.75rem;
  position: relative;
  box-shadow: 0 24px 64px rgba(0, 0, 0, 0.6), 0 0 0 1px rgba(255, 255, 255, 0.05), inset 0 1px 0 rgba(255, 255, 255, 0.1);
}
.modal-panel::before {
  content: '';
  position: absolute;
  inset: 0;
  border-radius: 1.75rem;
  padding: 1px;
  background: linear-gradient(135deg, rgba(212, 175, 55, 0.5), rgba(41, 98, 255, 0.3));
  -webkit-mask: linear-gradient(#fff 0 0) content-box, linear-gradient(#fff 0 0);
  -webkit-mask-composite: xor;
  mask-composite: exclude;
  pointer-events: none;
  opacity: 0.6;
}
.modal-close {
  position: absolute;
  top: 1.25rem;
  right: 1.25rem;
  background: rgba(239, 68, 68, 0.1);
  border: 1px solid rgba(239, 68, 68, 0.3);
  width: 2.5rem;
  height: 2.5rem;
  border-radius: 50%;
  color: #ef4444;
  font-size: 1.5rem;
  cursor: pointer;
  transition: all 0.3s cubic-bezier(0.4, 0, 0.2, 1);
  display: flex;
  align-items: center;
  justify-content: center;
  backdrop-filter: blur(8px);
}
.modal-close:hover {
  background: rgba(239, 68, 68, 0.2);
  border-color: #ef4444;
  transform: rotate(90deg) scale(1.1);
  box-shadow: 0 4px 16px rgba(239, 68, 68, 0.3);
}
.modal-panel h2 {
  font-size: 1.75rem;
  margin-bottom: 2rem;
  text-align: center;
  background: linear-gradient(135deg, #d4af37, #f4e4a6);
  -webkit-background-clip: text;
  -webkit-text-fill-color: transparent;
  background-clip: text;
  font-weight: 700;
  letter-spacing: 1px;
}
.auth-form {
  display: flex;
  flex-direction: column;
  gap: 1.25rem;
}
.form-input {
  background: rgba(10, 14, 26, 0.6);
  border: 1px solid rgba(139, 149, 168, 0.2);
  padding: 0.95rem 1.25rem;
  border-radius: 0.875rem;
  color: #e8eef7;
  outline: none;
  transition: all 0.3s;
  backdrop-filter: blur(8px);
  font-size: 0.95rem;
}
.form-input:focus {
  border-color: #d4af37;
  box-shadow: 0 0 0 3px rgba(212, 175, 55, 0.15), 0 2px 8px rgba(212, 175, 55, 0.2);
  background: rgba(10, 14, 26, 0.8);
}
.form-input::placeholder {
  color: #5a6477;
}
.auth-switch {
  text-align: center;
  font-size: 0.95rem;
  color: #8b95a8;
  margin-top: 0.5rem;
}
.link-btn {
  background: none;
  border: none;
  color: #d4af37;
  cursor: pointer;
  text-decoration: none;
  font-weight: 600;
  transition: all 0.3s;
  padding: 0 0.25rem;
  border-radius: 0.25rem;
}
.link-btn:hover {
  color: #f4e4a6;
  background: rgba(212, 175, 55, 0.1);
}
.auth-msg {
  text-align: center;
  font-size: 0.9rem;
  padding: 0.85rem;
  border-radius: 0.75rem;
  margin-top: 0.75rem;
  background: linear-gradient(135deg, rgba(34, 197, 94, 0.15), rgba(22, 163, 74, 0.1));
  color: #4ade80;
  border: 1px solid rgba(34, 197, 94, 0.3);
  backdrop-filter: blur(8px);
  font-weight: 500;
}
.auth-msg.error {
  background: linear-gradient(135deg, rgba(239, 68, 68, 0.15), rgba(220, 38, 38, 0.1));
  color: #f87171;
  border-color: rgba(239, 68, 68, 0.3);
}
.modal-enter-active, .modal-leave-active {
  transition: all 0.4s cubic-bezier(0.4, 0, 0.2, 1);
}
.modal-enter-from, .modal-leave-to {
  opacity: 0;
}
.modal-enter-from .modal-panel, .modal-leave-to .modal-panel {
  transform: scale(0.9) translateY(20px);
  opacity: 0;
}
</style>
