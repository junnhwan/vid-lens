<template>
  <div id="app">
    <Navbar
      :user="user"
      @logout="logout"
      @openAuth="openAuth"
      @openConfig="openConfig"
      @toggleSidebar="toggleSidebar"
    />

    <router-view />

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

    <AIConfigModal
      ref="aiConfigModal"
      :show="showConfig"
      @close="closeConfig"
      @updated="onConfigUpdated"
      @showConfirm="openConfirm"
    />

    <ConfirmDialog
      :show="confirmState.show"
      :title="confirmState.title"
      :message="confirmState.message"
      :confirmText="confirmState.confirmText"
      :showCancel="confirmState.showCancel"
      :type="confirmState.type"
      :icon="confirmState.icon"
      @confirm="handleConfirm"
      @cancel="closeConfirm"
    />

    <!-- 离线提示 -->
    <transition name="toast">
      <div v-if="offlineToast" class="toast offline">📡 网络已断开，部分功能不可用</div>
    </transition>

    <transition name="toast">
      <div v-if="toast" class="toast" :class="{ error: toastIsError }">{{ toast }}</div>
    </transition>
  </div>
</template>

<script setup>
import { ref, computed, reactive, provide, onMounted, onUnmounted, defineAsyncComponent, watch } from 'vue'
import Navbar from './components/Navbar.vue'
import AuthModal from './components/AuthModal.vue'
import ConfirmDialog from './components/ConfirmDialog.vue'

// 懒加载低频组件
const AIConfigModal = defineAsyncComponent(() => import('./components/AIConfigModal.vue'))

import api from './api'
import { formatUploadError, formatUploadProgressMessage, uploadFileInChunks } from './chunkedUpload.js'
import { buildStoredUser } from './authSession.js'
import { needsResultDetail, needsTaskDetail } from './taskDetailPolicy.js'
import { isPollingSuccessful, shouldStopPolling } from './taskPollingPolicy.js'

// 状态
const user = ref(null)
const tasks = ref([])
const uploading = ref(false)
const uploadMsg = ref('')
const uploadProgress = ref(-1)
const toast = ref('')
const toastIsError = ref(false)
const selectedTask = ref(null)
const loading = ref({})
const tasksLoading = ref(false)
const showAuth = ref(false)
const authMode = ref('login')
const authLoading = ref(false)
const authMsg = ref('')
const authError = ref(false)
const pollingTimers = ref({})
const showConfig = ref(false)
const aiConfigModal = ref(null)
const sidebarOpen = ref(false)
const offlineToast = ref(false)

// 分页
const currentPage = ref(1)
const pageSize = 20
const hasMore = ref(false)
const loadingMore = ref(false)
const tasksTotal = ref(0) // 后端返回的全量任务数；已加载任务可能少于它（分页未加载完）
const tasksLoadError = ref('') // 列表加载失败原因（非空时前端展示重试入口）
const searchKeyword = ref('') // 任务搜索关键字（走后端全量搜索，不再只过滤已加载页）

// ConfirmDialog 状态
const confirmState = ref({
  show: false,
  title: '提示',
  message: '',
  confirmText: '确认',
  showCancel: true,
  type: 'danger',
  icon: '⚠️',
  onConfirm: null
})

const openConfirm = (opts) => {
  confirmState.value = { ...confirmState.value, show: true, ...opts }
}

const handleConfirm = () => {
  const cb = confirmState.value.onConfirm
  confirmState.value.show = false
  if (cb) cb()
}

const closeConfirm = () => {
  confirmState.value.show = false
}

// 计算属性
// total 是后端全量值；completed/processing/failed 仅基于当前已加载任务，
// 超过一页时分项可能不全，由 Sidebar 在 loaded < total 时提示。
const taskStats = computed(() => ({
  total: tasksTotal.value || tasks.value.length,
  loaded: tasks.value.length,
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
  uploadMsg.value = '正在计算文件指纹...'
  uploadProgress.value = 0
  try {
    await uploadFileInChunks({
      file,
      api,
      onProgress: (progress) => {
        const { stage, percent } = progress
        uploadProgress.value = percent
        if (stage === 'hashing') uploadMsg.value = '正在计算文件指纹...'
        if (stage === 'uploading') {
          uploadMsg.value = formatUploadProgressMessage(progress)
        }
        if (stage === 'merging') uploadMsg.value = '正在合并视频分片...'
        if (stage === 'completed') uploadMsg.value = '上传完成'
      },
    })
    showToast('上传成功')
    uploadProgress.value = 100
    await fetchTasks()
  } catch (err) {
    showToast(formatUploadError(err), true)
  } finally {
    uploading.value = false
    setTimeout(() => { uploadProgress.value = -1 }, 800)
  }
}

const handleUrlUpload = async (url) => {
  uploading.value = true
  uploadMsg.value = '正在创建下载任务...'
  uploadProgress.value = -1
  try {
    const result = await api.uploadByURL(url)
    showToast('已创建下载任务，正在后台下载视频')
    await fetchTasks()
    if (result?.task_id) {
      loading.value[result.task_id] = true
      startPolling(result.task_id, 'download')
    }
  } catch (err) {
    showToast(err.message || '创建任务失败', true)
  } finally {
    uploading.value = false
  }
}

const fetchTasks = async (page = 1, append = false, keyword = searchKeyword.value) => {
  if (!user.value) {
    tasks.value = []
    tasksLoading.value = false
    return
  }
  if (!append && tasks.value.length === 0) {
    tasksLoading.value = true
  }
  try {
    const res = await api.listTasks(page, pageSize, keyword)
    const list = res?.list || []
    if (append) {
      tasks.value = [...tasks.value, ...list]
    } else {
      tasks.value = list
    }
    currentPage.value = page
    tasksTotal.value = typeof res?.total === 'number' ? res.total : tasks.value.length
    hasMore.value = tasks.value.length < tasksTotal.value
    tasksLoadError.value = ''
  } catch (err) {
    console.error(err)
    // 首次加载（当前无数据）才弹 toast，避免轮询刷屏；同时记录错误供列表展示重试
    if (!append) {
      tasksLoadError.value = err.message || '加载任务列表失败'
      if (tasks.value.length === 0) {
        showToast(tasksLoadError.value, true)
      }
    }
  } finally {
    if (!append) {
      tasksLoading.value = false
    }
  }
}

const loadMoreTasks = async () => {
  if (loadingMore.value || !hasMore.value) return
  loadingMore.value = true
  try {
    await fetchTasks(currentPage.value + 1, true)
  } finally {
    loadingMore.value = false
  }
}

const retryLoadTasks = () => fetchTasks(1)

const onSearchTasks = (kw) => {
  searchKeyword.value = kw
  fetchTasks(1, false, kw)
}

const deleteTask = async (task) => {
  openConfirm({
    title: '确认删除',
    message: `确定要删除「${task.filename}」吗？此操作不可恢复。`,
    confirmText: '删除',
    showCancel: true,
    type: 'danger',
    icon: '🗑️',
    onConfirm: async () => {
      try {
        await api.deleteTask(task.id)
        showToast('删除成功')
        tasks.value = tasks.value.filter(t => t.id !== task.id)
        tasksTotal.value = Math.max(0, tasksTotal.value - 1)
        hasMore.value = tasks.value.length < tasksTotal.value
        if (selectedTask.value?.id === task.id) {
          selectedTask.value = null
        }
        if (pollingTimers.value[task.id]) {
          clearInterval(pollingTimers.value[task.id])
          delete pollingTimers.value[task.id]
        }
        delete loading.value[task.id]
      } catch (err) {
        showToast('删除失败', true)
      }
    }
  })
}

// 视频库 master-detail：再点同一条可取消选中
const openTaskDrawer = async (task) => {
  if (selectedTask.value?.id === task?.id) {
    selectedTask.value = null
    return
  }
  selectedTask.value = task
  sidebarOpen.value = false
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

const mergeTaskIntoList = (detail) => {
  if (!detail?.id) return null
  const index = tasks.value.findIndex((t) => t.id === detail.id)
  if (index >= 0) {
    const merged = { ...tasks.value[index], ...detail }
    tasks.value[index] = merged
    return merged
  }
  // 新下载任务可能还不在当前页：插到列表顶部，避免进度无处展示
  tasks.value = [detail, ...tasks.value]
  tasksTotal.value = Math.max(tasksTotal.value + 1, tasks.value.length)
  return detail
}

const refreshTaskDetail = async (taskId) => {
  try {
    const detail = await api.getTask(taskId)
    const merged = mergeTaskIntoList(detail)
    if (selectedTask.value?.id === taskId && merged) {
      selectedTask.value = { ...selectedTask.value, ...merged }
    }
    return merged
  } catch (err) {
    console.error(err)
    return null
  }
}

// 轮询单任务详情，避免 fetchTasks(page=1) 冲掉已加载分页 / 搜索结果
const startPolling = (taskId, type) => {
  if (pollingTimers.value[taskId]) clearInterval(pollingTimers.value[taskId])
  const timer = setInterval(async () => {
    // 若 timer 已被清理，跳过（防止竞态）
    if (!pollingTimers.value[taskId]) return
    const task = await refreshTaskDetail(taskId)
    if (!task) return
    if (shouldStopPolling(task, type)) {
      clearInterval(timer)
      delete pollingTimers.value[taskId]
      loading.value[taskId] = false
      if (isPollingSuccessful(task, type)) {
        // 完成时再拉一次，拿全量 transcription/summary
        await refreshTaskDetail(taskId)
        showToast(type === 'download' ? '下载完成' : '处理完成')
      } else {
        showToast(task.error_msg || task.last_error_msg || '处理失败', true)
      }
    }
  }, 3000)
  pollingTimers.value[taskId] = timer
  setTimeout(() => {
    if (pollingTimers.value[taskId] === timer) {
      clearInterval(pollingTimers.value[taskId])
      delete pollingTimers.value[taskId]
      loading.value[taskId] = false
      showToast('任务仍在后台处理，请稍后刷新查看进度', true)
    }
  }, 300000)
}

const clearAllPolling = () => {
  Object.values(pollingTimers.value).forEach((timer) => clearInterval(timer))
  pollingTimers.value = {}
  loading.value = {}
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
  clearAllPolling()
  user.value = null
  localStorage.removeItem('token')
  localStorage.removeItem('user')
  tasks.value = []
  tasksTotal.value = 0
  hasMore.value = false
  selectedTask.value = null
  searchKeyword.value = ''
  tasksLoadError.value = ''
  showToast('已退出')
}

// AI 配置相关
const openConfig = () => {
  showConfig.value = true
  aiConfigModal.value?.loadProfiles()
}

const closeConfig = () => {
  showConfig.value = false
}

const onConfigUpdated = () => {
  showToast('配置已更新')
}

// 侧边栏（移动端）
const toggleSidebar = () => { sidebarOpen.value = !sidebarOpen.value }
const closeSidebar = () => { sidebarOpen.value = false }

// 把共享状态/方法提供给路由视图组件。
// reactive 会自动 unwrap 顶层的 ref/computed，视图里可直接 app.tasks 这样用。
const appCtx = reactive({
  user,
  tasks,
  uploading,
  uploadMsg,
  uploadProgress,
  taskStats,
  selectedTask,
  loading,
  tasksLoading,
  loadingMore,
  hasMore,
  tasksTotal,
  tasksLoadError,
  retryLoadTasks,
  searchKeyword,
  onSearchTasks,
  sidebarOpen,
  handleFileUpload,
  handleUrlUpload,
  openTaskDrawer,
  closeDrawer,
  deleteTask,
  doTranscribe,
  doAnalyze,
  loadMoreTasks,
  showToast,
  openAuth,
  toggleSidebar,
  closeSidebar,
})
provide('appCtx', appCtx)

// 键盘快捷键
const handleGlobalKeydown = (e) => {
  // ESC 关闭视频库详情分栏（弹层打开时不抢）
  if (
    e.key === 'Escape' &&
    selectedTask.value &&
    !showAuth.value &&
    !showConfig.value &&
    !confirmState.value.show
  ) {
    e.preventDefault()
    closeDrawer()
    return
  }
  // Ctrl/Cmd + K 打开搜索
  if ((e.ctrlKey || e.metaKey) && e.key === 'k') {
    e.preventDefault()
    const searchBox = document.querySelector('.search-box')
    if (searchBox) {
      searchBox.focus()
    }
  }
  // N 键快速上传（非输入/可编辑区域时）
  if (
    e.key === 'n' &&
    !e.ctrlKey &&
    !e.metaKey &&
    !e.altKey &&
    !['INPUT', 'TEXTAREA', 'SELECT'].includes(e.target.tagName) &&
    !e.target.isContentEditable
  ) {
    e.preventDefault()
    const uploadBtn = document.querySelector('.upload-btn')
    if (uploadBtn && !uploadBtn.disabled) {
      uploadBtn.click()
    }
  }
}

// 网络状态检测
const handleOnline = () => {
  offlineToast.value = false
  showToast('网络已恢复')
}
const handleOffline = () => {
  offlineToast.value = true
}


// 弹层打开时锁住背景滚动，避免 fixed 层与页面滚动打架
watch(
  () => showAuth.value || showConfig.value || confirmState.value.show,
  (open) => {
    document.body.style.overflow = open ? 'hidden' : ''
  },
)

onMounted(() => {
  const saved = localStorage.getItem('user')
  if (saved) {
    try {
      user.value = JSON.parse(saved)
      fetchTasks()
    } catch (e) {}
  }
  window.addEventListener('online', handleOnline)
  window.addEventListener('offline', handleOffline)
  window.addEventListener('keydown', handleGlobalKeydown)
})

onUnmounted(() => {
  window.removeEventListener('online', handleOnline)
  window.removeEventListener('offline', handleOffline)
  window.removeEventListener('keydown', handleGlobalKeydown)
  clearAllPolling()
  document.body.style.overflow = ''
})
</script>

<style>
@import url('https://fonts.googleapis.com/css2?family=DM+Sans:ital,opsz,wght@0,9..40,400;0,9..40,500;0,9..40,600;0,9..40,700;1,9..40,400&family=IBM+Plex+Mono:wght@400;500;600&family=Noto+Sans+SC:wght@400;500;600;700&family=Syne:wght@600;700;800&display=swap');

*,
*::before,
*::after {
  box-sizing: border-box;
}

html, body {
  margin: 0;
  padding: 0;
  width: 100%;
  height: 100%;
  overflow-x: hidden;
  background: var(--vl-bg);
}

#app {
  min-height: 100vh;
  position: relative;
  color: var(--vl-text);
  font-family: var(--vl-font);
  font-feature-settings: 'ss01' on, 'kern' on;
  letter-spacing: 0.01em;
  overflow-x: hidden;
  background:
    radial-gradient(ellipse 80% 50% at 10% -10%, rgba(45, 212, 191, 0.12), transparent 55%),
    radial-gradient(ellipse 60% 40% at 95% 10%, rgba(96, 165, 250, 0.08), transparent 50%),
    radial-gradient(ellipse 50% 50% at 70% 100%, rgba(240, 180, 41, 0.05), transparent 55%),
    var(--vl-bg);
}

/* subtle film grain — keep pointer-events off; do NOT force position on children
   (that would override modal position:fixed via #app > * specificity) */
#app::before {
  content: '';
  position: fixed;
  inset: 0;
  pointer-events: none;
  z-index: 0;
  opacity: 0.35;
  background-image: url("data:image/svg+xml,%3Csvg viewBox='0 0 256 256' xmlns='http://www.w3.org/2000/svg'%3E%3Cfilter id='n'%3E%3CfeTurbulence type='fractalNoise' baseFrequency='0.85' numOctaves='4' stitchTiles='stitch'/%3E%3C/filter%3E%3Crect width='100%25' height='100%25' filter='url(%23n)' opacity='0.5'/%3E%3C/svg%3E");
  background-size: 180px 180px;
  mix-blend-mode: soft-light;
}

button, input, textarea, select {
  font-family: inherit;
}

a {
  color: var(--vl-primary);
}

/* Toast */
.toast {
  position: fixed;
  top: calc(var(--vl-nav-h) + 1rem);
  right: 1.5rem;
  padding: 0.85rem 1.25rem;
  backdrop-filter: blur(16px) saturate(160%);
  background: rgba(16, 185, 129, 0.92);
  border-radius: var(--vl-radius);
  font-weight: 600;
  z-index: 1300;
  box-shadow: var(--vl-shadow);
  border: 1px solid rgba(255, 255, 255, 0.12);
  font-size: 0.88rem;
  letter-spacing: 0.02em;
  color: #fff;
  max-width: min(360px, calc(100vw - 2rem));
}
.toast.error {
  background: rgba(239, 68, 68, 0.94);
}
.toast.offline {
  background: rgba(71, 85, 105, 0.95);
  top: auto;
  bottom: 1.5rem;
}
.toast-enter-active, .toast-leave-active {
  transition: all 0.3s var(--vl-ease);
}
.toast-enter-from, .toast-leave-to {
  opacity: 0;
  transform: translateX(16px) scale(0.96);
}

/* Thin scrollbar default */
* {
  scrollbar-width: thin;
  scrollbar-color: rgba(45, 212, 191, 0.28) transparent;
}
*::-webkit-scrollbar {
  width: 8px;
  height: 8px;
}
*::-webkit-scrollbar-thumb {
  background: rgba(45, 212, 191, 0.28);
  border-radius: 4px;
}
*::-webkit-scrollbar-thumb:hover {
  background: rgba(45, 212, 191, 0.45);
}
</style>
