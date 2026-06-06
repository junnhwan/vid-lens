<template>
  <div id="app">
    <Navbar :user="user" @logout="logout" @openAuth="openAuth" />

    <div class="app-layout">
      <Sidebar
        :user="user"
        :uploading="uploading"
        :uploadMsg="uploadMsg"
        :stats="taskStats"
        @uploadFile="handleFileUpload"
        @uploadUrl="handleUrlUpload"
        @openAuth="openAuth"
      />

      <main class="content-area">
        <TaskList
          :tasks="tasks"
          :loading="loading"
          @taskClick="openTaskDrawer"
          @deleteTask="deleteTask"
          @transcribe="doTranscribe"
          @analyze="doAnalyze"
        />
      </main>
    </div>

    <TaskDrawer
      :task="selectedTask"
      :loading="loading[selectedTask?.id]"
      @close="closeDrawer"
      @transcribe="doTranscribe(selectedTask)"
      @analyze="doAnalyze(selectedTask)"
    />

    <AuthModal
      :show="showAuth"
      :mode="authMode"
      :loading="authLoading"
      :message="authMsg"
      :isError="authError"
      @close="closeAuth"
      @switchMode="switchAuthMode"
      @submit="handleAuth"
    />

    <transition name="toast">
      <div v-if="toast" class="toast" :class="{ error: toastIsError }">{{ toast }}</div>
    </transition>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import Navbar from './components/Navbar.vue'
import Sidebar from './components/Sidebar.vue'
import TaskList from './components/TaskList.vue'
import TaskDrawer from './components/TaskDrawer.vue'
import AuthModal from './components/AuthModal.vue'
import api from './api'
import { buildStoredUser } from './authSession.js'
import { needsResultDetail, needsTaskDetail } from './taskDetailPolicy.js'
import { shouldStopPolling } from './taskPollingPolicy.js'

// 状态
const user = ref(null)
const tasks = ref([])
const uploading = ref(false)
const uploadMsg = ref('')
const toast = ref('')
const toastIsError = ref(false)
const selectedTask = ref(null)
const loading = ref({})
const showAuth = ref(false)
const authMode = ref('login')
const authLoading = ref(false)
const authMsg = ref('')
const authError = ref(false)
const pollingTimers = ref({})

// 计算属性
const taskStats = computed(() => ({
  total: tasks.value.length,
  completed: tasks.value.filter(t => t.status === 3).length,
  processing: tasks.value.filter(t => t.status < 3).length,
  failed: tasks.value.filter(t => t.status === 4).length
}))

// 工具函数
const showToast = (msg, isError = false) => {
  toast.value = msg
  toastIsError.value = isError
  setTimeout(() => { if (toast.value === msg) toast.value = '' }, 3500)
}

// 业务逻辑
const handleFileUpload = async (file) => {
  if (!file || !file.type.startsWith('video/')) {
    showToast('仅支持视频文件', true)
    return
  }
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

const handleUrlUpload = async (url) => {
  uploading.value = true
  uploadMsg.value = '正在下载并解析...'
  try {
    await api.uploadByURL(url)
    showToast('下载成功')
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
}
const closeAuth = () => { showAuth.value = false }
const switchAuthMode = () => {
  authMode.value = authMode.value === 'login' ? 'register' : 'login'
  authMsg.value = ''
}

const handleAuth = async (formData) => {
  if (!formData.username || !formData.password) {
    authMsg.value = '请输入用户名和密码'
    authError.value = true
    return
  }
  authLoading.value = true
  authMsg.value = ''
  try {
    const { username, password, nickname } = formData
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
@import url('https://fonts.googleapis.com/css2?family=Noto+Sans+SC:wght@400;500;700&family=JetBrains+Mono:wght@400;600&display=swap');

/* 全局样式 */
html, body {
  margin: 0;
  padding: 0;
  width: 100%;
  height: 100%;
  overflow-x: hidden;
}

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

.app-layout {
  display: flex;
  min-height: calc(100vh - 80px);
  max-width: 1600px;
  margin: 0 auto;
  padding: 0;
  position: relative;
  z-index: 2;
}

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
</style>
