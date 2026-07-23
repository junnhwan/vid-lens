<template>
  <div id="app" class="app-shell">
    <ImmersiveCanvas />

    <div class="app-chrome">
      <Navbar
        :user="user"
        @logout="logout"
        @openAuth="openAuth"
        @openConfig="openConfig"
        @toggleSidebar="toggleSidebar"
      />

      <router-view v-slot="{ Component, route }">
        <transition name="page" mode="out-in">
          <component :is="Component" :key="route.meta.pageKey || route.name" />
        </transition>
      </router-view>
    </div>

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
      <div v-if="offlineToast" class="toast offline">
        <VlIcon :name="ICON.wifiOff" size="sm" />
        <span>网络已断开，部分功能不可用</span>
      </div>
    </transition>

    <transition name="toast">
      <div v-if="toast" class="toast" :class="{ error: toastIsError }">{{ toast }}</div>
    </transition>
  </div>
</template>

<script setup>
import { ref, computed, reactive, provide, onMounted, onUnmounted, watch } from 'vue'
import { useRouter } from 'vue-router'
import Navbar from './components/Navbar.vue'
import AuthModal from './components/AuthModal.vue'
import ConfirmDialog from './components/ConfirmDialog.vue'
import ImmersiveCanvas from './components/ImmersiveCanvas.vue'
import VlIcon from './components/VlIcon.vue'
import { ICON } from './icons.js'

import api from './api'
import { getStoredTheme, setTheme as applyStoredTheme, THEME_CHANGE_EVENT } from './theme.js'
import { normalizeListResponse } from './apiEnvelope.js'
import { formatUploadError, formatUploadProgressMessage, uploadFileInChunks } from './chunkedUpload.js'
import { buildStoredUser } from './authSession.js'
import { writeLastChatTaskId } from './chatSelectionPolicy.js'
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
const sidebarOpen = ref(false)
const offlineToast = ref(false)
const theme = ref(getStoredTheme())
const router = useRouter()

const setTheme = (id) => {
  theme.value = applyStoredTheme(id)
}

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
  icon: ICON.alert,
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
    const enrich = (item, prev) => {
      const hasTx = !!(
        item.has_transcription ??
        item.hasTranscription ??
        prev?.has_transcription ??
        prev?.transcription?.content
      )
      const hasSum = !!(
        item.has_summary ??
        item.hasSummary ??
        prev?.has_summary ??
        prev?.summary?.content
      )
      return {
        ...(prev || {}),
        ...item,
        has_transcription: hasTx,
        has_summary: hasSum,
        // 列表不带正文：保留本会话已拉过的 content，避免灰显闪回
        transcription: prev?.transcription?.content ? prev.transcription : item.transcription,
        summary: prev?.summary?.content ? prev.summary : item.summary,
      }
    }
    const byId = new Map(tasks.value.map((t) => [t.id, t]))
    const list = (res?.list || []).map((item) => enrich(item, byId.get(item.id)))

    if (append) {
      const existingIds = new Set(tasks.value.map((t) => t.id))
      const updates = new Map(list.map((t) => [t.id, t]))
      tasks.value = [
        ...tasks.value.map((t) => updates.get(t.id) || t),
        ...list.filter((t) => !existingIds.has(t.id)),
      ]
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
    icon: ICON.trash,
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

const doTranscribe = async (task, opts = {}) => {
  const force = !!opts.force
  if (!force) {
    // 已有结果（详情 content / 列表标记 / 需拉详情）：只打开并刷新，不重复提交
    if (
      task.transcription?.content ||
      task.has_transcription ||
      needsResultDetail(task, 'transcription')
    ) {
      selectedTask.value = task
      await refreshTaskDetail(task.id)
      return
    }
  }
  if (loading.value[task.id]) return
  loading.value[task.id] = true
  selectedTask.value = task
  try {
    await api.transcribe(task.id, { force })
    startPolling(task.id, 'transcription')
  } catch (err) {
    showToast(err.message || '请求失败', true)
    loading.value[task.id] = false
  }
}

const doAnalyze = async (task, opts = {}) => {
  const force = !!opts.force
  if (!force) {
    if (
      task.summary?.content ||
      task.has_summary ||
      needsResultDetail(task, 'summary')
    ) {
      selectedTask.value = task
      await refreshTaskDetail(task.id)
      return
    }
  }
  if (loading.value[task.id]) return
  loading.value[task.id] = true
  selectedTask.value = task
  try {
    await api.analyze(task.id, { force })
    startPolling(task.id, 'summary')
  } catch (err) {
    showToast(err.message || '请求失败', true)
    loading.value[task.id] = false
  }
}

const doRetranscribe = (task) => {
  openConfirm({
    title: '重新提取文字',
    message: '将重新调用语音识别并覆盖现有文字提取结果，是否继续？',
    confirmText: '重新提取',
    showCancel: true,
    type: 'primary',
    icon: ICON.rotate,
    onConfirm: () => doTranscribe(task, { force: true }),
  })
}

const doReanalyze = (task) => {
  openConfirm({
    title: '重新生成总结',
    message: '将重新调用模型并覆盖现有 AI 总结，是否继续？',
    confirmText: '重新总结',
    showCancel: true,
    type: 'primary',
    icon: ICON.rotate,
    onConfirm: () => doAnalyze(task, { force: true }),
  })
}

const mergeTaskIntoList = (detail) => {
  if (!detail?.id) return null
  // 详情带 content 时同步列表侧「已完成」标记，便于卡片灰显主按钮
  if (detail.transcription?.content) detail.has_transcription = true
  if (detail.summary?.content) detail.has_summary = true
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
      // 首登 / 无 AI 配置：引导去模型配置（主路径前置）
      await maybePromptAIConfig()
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
  writeLastChatTaskId(null) // 避免下一账号误开上一人的对话视频
  tasks.value = []
  tasksTotal.value = 0
  hasMore.value = false
  selectedTask.value = null
  searchKeyword.value = ''
  tasksLoadError.value = ''
  showToast('已退出')
}

// 设置 / AI 配置：独立页面（纯前端路由，复用既有 /ai/profiles API）
const openConfig = () => {
  router.push({ name: 'settings' })
}

/** 登录或恢复会话后：若无任何 AI profile，提示去配置（不强制阻塞） */
const maybePromptAIConfig = async () => {
  if (!user.value) return
  try {
    const list = normalizeListResponse(await api.getAIProfiles())
    if (list.length > 0) return
    openConfirm({
      title: '配置模型服务',
      message: '使用提取文字、总结和对话前，请先配置你的 AI 服务（自带 Key）。',
      confirmText: '去配置',
      showCancel: true,
      type: 'primary',
      icon: ICON.settings,
      onConfirm: () => openConfig(),
    })
  } catch {
    // 拉配置失败时不打扰；用户点「模型」仍可进
  }
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
  doRetranscribe,
  doReanalyze,
  loadMoreTasks,
  showToast,
  openAuth,
  openConfig,
  openConfirm,
  logout,
  toggleSidebar,
  closeSidebar,
  theme,
  setTheme,
})
provide('appCtx', appCtx)

// 键盘快捷键
const handleGlobalKeydown = (e) => {
  // ESC 关闭视频库详情分栏（弹层打开时不抢）
  if (
    e.key === 'Escape' &&
    selectedTask.value &&
    !showAuth.value &&
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

const handleThemeChange = (e) => {
  const next = e?.detail?.theme
  if (next) theme.value = next
  else theme.value = getStoredTheme()
}


// 弹层打开时锁住背景滚动，避免 fixed 层与页面滚动打架
watch(
  () => showAuth.value || confirmState.value.show,
  (open) => {
    document.body.style.overflow = open ? 'hidden' : ''
  },
)

onMounted(() => {
  const saved = localStorage.getItem('user')
  if (saved) {
    try {
      user.value = JSON.parse(saved)
      fetchTasks().then(() => maybePromptAIConfig())
    } catch (e) {}
  }
  theme.value = getStoredTheme()
  window.addEventListener('online', handleOnline)
  window.addEventListener('offline', handleOffline)
  window.addEventListener('keydown', handleGlobalKeydown)
  window.addEventListener(THEME_CHANGE_EVENT, handleThemeChange)
})

onUnmounted(() => {
  window.removeEventListener('online', handleOnline)
  window.removeEventListener('offline', handleOffline)
  window.removeEventListener('keydown', handleGlobalKeydown)
  window.removeEventListener(THEME_CHANGE_EVENT, handleThemeChange)
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

#app.app-shell {
  min-height: 100dvh;
  position: relative;
  color: var(--vl-text);
  font-family: var(--vl-font);
  font-feature-settings: 'ss01' on, 'kern' on;
  letter-spacing: 0.01em;
  overflow-x: hidden;
  background: var(--vl-bg);
}

.app-chrome {
  position: relative;
  z-index: 1;
  min-height: 100dvh;
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
  backdrop-filter: blur(18px) saturate(170%);
  background: color-mix(in srgb, var(--vl-success) 92%, #000);
  border-radius: var(--vl-radius);
  font-weight: 600;
  z-index: 1300;
  box-shadow: var(--vl-shadow);
  border: 1px solid var(--vl-white-a08);
  font-size: 0.88rem;
  letter-spacing: 0.02em;
  color: var(--vl-text-inverse);
  max-width: min(360px, calc(100vw - 2rem));
  display: inline-flex;
  align-items: center;
  gap: 0.5rem;
}
.toast.error {
  background: color-mix(in srgb, var(--vl-danger) 92%, #000);
}
.toast.offline {
  background: color-mix(in srgb, var(--vl-text-muted) 90%, #000);
  top: auto;
  bottom: 1.5rem;
  left: 50%;
  right: auto;
  transform: translateX(-50%);
}
.toast-enter-active, .toast-leave-active {
  transition: all 0.35s cubic-bezier(0.16, 1, 0.3, 1);
}
.toast-enter-from, .toast-leave-to {
  opacity: 0;
  transform: translateX(16px) scale(0.96);
}
.toast.offline.toast-enter-from,
.toast.offline.toast-leave-to {
  transform: translateX(-50%) translateY(12px) scale(0.96);
}

/* Thin scrollbar default */
* {
  scrollbar-width: thin;
  scrollbar-color: var(--vl-scrollbar-thumb) transparent;
}
*::-webkit-scrollbar {
  width: 8px;
  height: 8px;
}
*::-webkit-scrollbar-thumb {
  background: var(--vl-scrollbar-thumb);
  border-radius: 4px;
}
*::-webkit-scrollbar-thumb:hover {
  background: var(--vl-scrollbar-thumb-hover);
}

/* Library ↔ Chat page cross-fade (keyed by route.meta.pageKey so /chat/:id 不闪) */
.page-enter-active,
.page-leave-active {
  transition: opacity 0.28s cubic-bezier(0.16, 1, 0.3, 1), transform 0.32s cubic-bezier(0.16, 1, 0.3, 1);
}
.page-enter-from {
  opacity: 0;
  transform: translateY(10px) scale(0.995);
}
.page-leave-to {
  opacity: 0;
  transform: translateY(-6px) scale(0.998);
}
@media (prefers-reduced-motion: reduce) {
  .page-enter-active,
  .page-leave-active {
    transition: none;
  }
}
</style>
