<template>
  <div id="app">
    <!-- 导航栏 -->
    <nav class="navbar">
      <div class="nav-brand">
        <span class="brand-vid">Vid</span><span class="brand-lens">Lens</span>
        <span class="badge">PRO</span>
      </div>
      <div class="nav-right">
        <template v-if="user">
          <span class="user-name">{{ user.nickname || user.username }}</span>
          <button class="btn-ghost" @click="logout">退出</button>
        </template>
        <button v-else class="btn-primary" @click="showAuth = true">登录 / 注册</button>
      </div>
    </nav>

    <!-- 主区域 -->
    <main class="container">
      <!-- 上传区 -->
      <section class="upload-section">
        <h2 class="title">DECODE YOUR VIDEO</h2>
        <p class="subtitle">智能视频解析 · 算力赋能</p>

        <div class="upload-area" :class="{ dragging }"
             @dragover.prevent="dragging = true"
             @dragleave.prevent="dragging = false"
             @drop.prevent="handleDrop">
          <div class="upload-grid">
            <!-- 本地文件 -->
            <label class="upload-pane" :class="{ disabled: !user }">
              <input type="file" accept="video/*" @change="handleFileSelect" hidden />
              <div class="pane-icon">📁</div>
              <div class="pane-title">LOCAL FILE</div>
              <div class="pane-desc">{{ dragging ? '松手上传' : '点击或拖拽本地文件' }}</div>
            </label>

            <!-- URL 链接 -->
            <div class="upload-pane" :class="{ disabled: !user }">
              <div class="pane-icon">🌐</div>
              <div class="pane-title">WEB LINK</div>
              <div class="url-box">
                <input v-model="videoUrl" placeholder="粘贴视频链接..." @keyup.enter="handleUrlUpload" />
                <button class="btn-go" @click="handleUrlUpload">→</button>
              </div>
            </div>
          </div>
        </div>

        <div v-if="uploading" class="upload-progress">
          <div class="spinner"></div>
          <span>{{ uploadMsg }}</span>
        </div>

        <div v-if="toast" class="toast" :class="{ error: toastIsError }">{{ toast }}</div>
      </section>

      <!-- 工作台 -->
      <section v-if="tasks.length" class="workspace">
        <div class="section-header">
          <h3>工作台</h3>
          <span class="count">{{ tasks.length }} TASKS</span>
        </div>

        <div class="card-grid">
          <div v-for="t in tasks" :key="t.id" class="card">
            <button class="card-delete" @click="deleteTask(t)" title="删除">×</button>
            <div class="card-header">
              <span class="card-name">{{ t.filename }}</span>
              <span class="card-time">{{ formatTime(t.created_at) }}</span>
            </div>
            <div class="card-status" :class="statusClass(t.status)">{{ statusText(t.status) }}</div>
            <div class="card-actions">
              <button class="btn-action" @click="doTranscribe(t)" :disabled="t.status < 3 && t.status !== 1">
                📄 提取文字
              </button>
              <button class="btn-action ai" @click="doAnalyze(t)" :disabled="t.status < 3 && t.status !== 1">
                🤖 AI 总结
              </button>
            </div>

            <!-- 展开结果 -->
            <div v-if="expanded[t.id]" class="card-result">
              <div v-if="loading[t.id]" class="loading-inline">
                <div class="spinner small"></div> 处理中...
              </div>
              <div v-else-if="t.transcription && t.transcription.content" class="result-block">
                <h4>📝 文字提取</h4>
                <pre>{{ t.transcription.content }}</pre>
              </div>
              <div v-if="t.summary && t.summary.content" class="result-block">
                <h4>🤖 AI 总结</h4>
                <div class="markdown" v-html="renderMarkdown(t.summary.content)"></div>
              </div>
            </div>
          </div>
        </div>
      </section>
    </main>

    <!-- 登录/注册弹窗 -->
    <div v-if="showAuth" class="modal-backdrop" @click.self="showAuth = false">
      <div class="modal">
        <h3>{{ authMode === 'login' ? '用户登录' : '新用户注册' }}</h3>
        <input v-model="authForm.username" placeholder="用户名" />
        <input v-model="authForm.password" type="password" placeholder="密码" />
        <input v-if="authMode === 'register'" v-model="authForm.nickname" placeholder="昵称（可选）" />
        <button class="btn-primary full" @click="doAuth" :disabled="authLoading">
          {{ authLoading ? '请求中...' : (authMode === 'login' ? '登录' : '注册') }}
        </button>
        <p class="auth-switch">
          {{ authMode === 'login' ? '没有账号？' : '已有账号？' }}
          <a href="#" @click.prevent="authMode = authMode === 'login' ? 'register' : 'login'">
            {{ authMode === 'login' ? '去注册' : '去登录' }}
          </a>
        </p>
        <p v-if="authMsg" class="auth-msg" :class="{ error: authError }">{{ authMsg }}</p>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { marked } from 'marked'
import api from './api.js'

const user = ref(null)
const showAuth = ref(false)
const authMode = ref('login')
const authLoading = ref(false)
const authMsg = ref('')
const authError = ref(false)
const authForm = ref({ username: '', password: '', nickname: '' })

const tasks = ref([])
const uploading = ref(false)
const uploadMsg = ref('')
const videoUrl = ref('')
const dragging = ref(false)
const toast = ref('')
const toastIsError = ref(false)
const expanded = ref({})
const loading = ref({})
const pollingTimers = ref({})

onMounted(() => {
  const saved = localStorage.getItem('vidlens_user')
  if (saved) {
    try { user.value = JSON.parse(saved) } catch {}
  }
  if (user.value) fetchTasks()
})

// ===== 认证 =====
async function doAuth() {
  authLoading.value = true
  authMsg.value = ''
  try {
    const fn = authMode.value === 'login' ? api.login : api.register
    const res = await fn(authForm.value.username, authForm.value.password, authForm.value.nickname)
    if (res.code === 200) {
      if (authMode.value === 'login') {
        const userData = { ...res.data.user, token: res.data.token }
        user.value = userData
        localStorage.setItem('vidlens_user', JSON.stringify(userData))
        showAuth.value = false
        fetchTasks()
      } else {
        authMsg.value = '注册成功，请登录'
        authError.value = false
        authMode.value = 'login'
      }
    } else {
      authMsg.value = res.message || '操作失败'
      authError.value = true
    }
  } catch (e) {
    authMsg.value = e.message || '网络错误'
    authError.value = true
  } finally {
    authLoading.value = false
  }
}

function logout() {
  user.value = null
  localStorage.removeItem('vidlens_user')
  tasks.value = []
}

// ===== 上传 =====
async function handleFileSelect(e) {
  if (!user.value) { showAuth.value = true; return }
  const file = e.target.files[0]
  if (!file) return
  await uploadFile(file)
}

async function handleDrop(e) {
  if (!user.value) { showAuth.value = true; return }
  const file = e.dataTransfer.files[0]
  if (file) await uploadFile(file)
}

async function uploadFile(file) {
  uploading.value = true
  uploadMsg.value = '正在上传...'
  try {
    const res = await api.uploadFile(file, (e) => {
      uploadMsg.value = `上传中... ${Math.round(e.loaded / e.total * 100)}%`
    })
    if (res.code === 200) {
      showToast('✅ 上传完成')
      fetchTasks()
    } else {
      showToast('❌ ' + (res.message || '上传失败'), true)
    }
  } catch (e) {
    showToast('❌ 上传失败: ' + (e.message || '网络错误'), true)
  } finally {
    uploading.value = false
  }
}

async function handleUrlUpload() {
  if (!user.value) { showAuth.value = true; return }
  if (!videoUrl.value || !videoUrl.value.startsWith('http')) {
    showToast('⚠️ 请输入有效的视频链接', true); return
  }
  uploading.value = true
  uploadMsg.value = '正在下载视频...'
  try {
    const res = await api.uploadByURL(videoUrl.value)
    if (res.code === 200) {
      showToast('✅ 视频已入库')
      videoUrl.value = ''
      fetchTasks()
    } else {
      showToast('❌ ' + (res.message || '下载失败'), true)
    }
  } catch (e) {
    showToast('❌ 下载失败: ' + (e.message || '网络错误'), true)
  } finally {
    uploading.value = false
  }
}

// ===== 任务列表 =====
async function fetchTasks() {
  if (!user.value) return
  try {
    const res = await api.listTasks()
    if (res.code === 200) {
      tasks.value = (res.data.list || []).reverse()
    }
  } catch {}
}

async function deleteTask(t) {
  if (!confirm(`确认删除 "${t.filename}" ?`)) return
  await api.deleteTask(t.id)
  tasks.value = tasks.value.filter(i => i.id !== t.id)
}

// ===== AI 操作 =====
async function doAnalyze(t) {
  expanded.value[t.id] = true
  loading.value[t.id] = true
  try {
    const res = await api.analyze(t.id)
    if (res.code === 200) {
      startPolling(t.id)
    } else {
      showToast(res.message, true)
      loading.value[t.id] = false
    }
  } catch (e) {
    showToast('❌ 提交失败', true)
    loading.value[t.id] = false
  }
}

async function doTranscribe(t) {
  expanded.value[t.id] = true
  loading.value[t.id] = true
  try {
    const res = await api.transcribe(t.id)
    if (res.code === 200) {
      startPolling(t.id)
    } else {
      showToast(res.message, true)
      loading.value[t.id] = false
    }
  } catch (e) {
    loading.value[t.id] = false
  }
}

function startPolling(taskId) {
  if (pollingTimers.value[taskId]) clearInterval(pollingTimers.value[taskId])

  const timer = setInterval(async () => {
    try {
      const res = await api.getTask(taskId)
      if (res.code === 200) {
        const task = res.data
        const idx = tasks.value.findIndex(i => i.id === taskId)
        if (idx >= 0) tasks.value[idx] = task

        if (task.status === 3 || task.status === 4) { // completed or failed
          clearInterval(timer)
          delete pollingTimers.value[taskId]
          loading.value[taskId] = false
        }
      }
    } catch {}
  }, 3000)

  pollingTimers.value[taskId] = timer

  // 5分钟兜底停止
  setTimeout(() => {
    if (pollingTimers.value[taskId]) {
      clearInterval(pollingTimers.value[taskId])
      delete pollingTimers.value[taskId]
      loading.value[taskId] = false
    }
  }, 300000)
}

// ===== 工具 =====
function showToast(msg, isError = false) {
  toast.value = msg
  toastIsError.value = isError
  setTimeout(() => { if (toast.value === msg) toast.value = '' }, 4000)
}

function formatTime(t) {
  if (!t) return '--'
  const d = new Date(t)
  return `${d.getMonth()+1}/${d.getDate()} ${String(d.getHours()).padStart(2,'0')}:${String(d.getMinutes()).padStart(2,'0')}`
}

const STATUS_MAP = { 0: '待处理', 1: '排队中', 2: '处理中', 3: '已完成', 4: '失败' }
function statusText(s) { return STATUS_MAP[s] || '未知' }
function statusClass(s) { return ['pending','queued','running','done','fail'][s] || '' }

function renderMarkdown(content) {
  if (!content) return ''
  // 清理 DeepSeek R1 的 <think/> 标签
  let clean = content.replace(/<think[\s\S]*?<\/think>/gi, '')
  return marked.parse(clean)
}
</script>

<style>
:root {
  --bg: #0a0a0f;
  --card: #13141a;
  --border: #2a2d35;
  --accent: #c5f946;
  --text: #e0e0e0;
  --dim: #6b7280;
  --danger: #ef4444;
}

* { box-sizing: border-box; margin: 0; padding: 0; }
body { background: var(--bg); color: var(--text); font-family: -apple-system, 'Segoe UI', sans-serif; }

.navbar {
  display: flex; justify-content: space-between; align-items: center;
  padding: 1rem 2rem; border-bottom: 1px solid var(--border);
  background: rgba(10,10,15,0.9); backdrop-filter: blur(8px); position: sticky; top: 0; z-index: 100;
}
.nav-brand { font-size: 1.6rem; font-weight: 800; }
.brand-vid { color: var(--text); }
.brand-lens { color: var(--accent); font-weight: 300; }
.badge { font-size: 0.6rem; background: var(--accent); color: var(--bg); padding: 2px 6px; border-radius: 3px; margin-left: 8px; vertical-align: super; }
.nav-right { display: flex; align-items: center; gap: 12px; }
.user-name { color: var(--accent); font-family: monospace; font-size: 0.9rem; }

.btn-primary {
  background: var(--accent); color: var(--bg); border: none; padding: 8px 20px;
  font-weight: 700; border-radius: 6px; cursor: pointer; font-size: 0.85rem;
}
.btn-primary:hover { filter: brightness(1.1); }
.btn-ghost { background: none; border: 1px solid var(--border); color: var(--dim); padding: 6px 14px; border-radius: 6px; cursor: pointer; }
.btn-ghost:hover { color: var(--danger); border-color: var(--danger); }
.full { width: 100%; padding: 12px; }

.container { max-width: 1000px; margin: 0 auto; padding: 3rem 1.5rem; }

/* 上传区 */
.upload-section { text-align: center; margin-bottom: 4rem; }
.title { font-size: 2.5rem; font-weight: 800; letter-spacing: 2px; margin-bottom: 0.3rem; }
.subtitle { color: var(--dim); font-size: 0.95rem; letter-spacing: 3px; margin-bottom: 2.5rem; }

.upload-area {
  max-width: 700px; margin: 0 auto; padding: 2.5rem; border-radius: 16px;
  background: var(--card); border: 2px dashed var(--border); transition: all 0.3s;
}
.upload-area.dragging { border-color: var(--accent); background: rgba(197,249,70,0.03); }
.upload-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 2rem; }

.upload-pane {
  display: flex; flex-direction: column; align-items: center; gap: 8px;
  padding: 1.5rem; border-radius: 12px; cursor: pointer; transition: all 0.3s;
  background: rgba(255,255,255,0.02);
}
.upload-pane:hover { background: rgba(197,249,70,0.05); }
.upload-pane.disabled { opacity: 0.3; pointer-events: none; }
.pane-icon { font-size: 2rem; }
.pane-title { font-weight: 700; font-size: 1rem; letter-spacing: 1px; }
.pane-desc { font-size: 0.8rem; color: var(--dim); }

.url-box { display: flex; margin-top: 10px; border-bottom: 2px solid var(--border); }
.url-box input {
  background: none; border: none; outline: none; color: var(--text);
  padding: 8px 4px; width: 180px; font-size: 0.85rem; font-family: monospace;
}
.btn-go {
  background: none; border: none; color: var(--accent); cursor: pointer;
  font-size: 1.2rem; padding: 0 8px;
}
.btn-go:hover { transform: translateX(2px); }

.upload-progress { margin-top: 1.5rem; display: flex; align-items: center; justify-content: center; gap: 12px; color: var(--accent); }

.spinner {
  width: 28px; height: 28px; border: 3px solid var(--border); border-top-color: var(--accent);
  border-radius: 50%; animation: spin 0.8s linear infinite;
}
.spinner.small { width: 18px; height: 18px; border-width: 2px; }

.toast {
  margin-top: 1.5rem; display: inline-block; background: var(--accent); color: var(--bg);
  padding: 8px 24px; font-weight: 700; border-radius: 4px;
}
.toast.error { background: var(--danger); color: #fff; }

/* 工作台 */
.workspace { margin-top: 3rem; }
.section-header { display: flex; align-items: center; gap: 12px; margin-bottom: 1.5rem; border-bottom: 2px solid var(--border); padding-bottom: 10px; }
.section-header h3 { font-size: 1.3rem; }
.count { background: var(--border); padding: 3px 10px; border-radius: 4px; font-size: 0.75rem; font-family: monospace; }

.card-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(300px, 1fr)); gap: 16px; }

.card {
  background: var(--card); border-radius: 12px; border: 1px solid var(--border);
  padding: 1.2rem; position: relative; transition: all 0.2s;
}
.card:hover { border-color: var(--accent); transform: translateY(-2px); }
.card-delete {
  position: absolute; top: 8px; right: 10px; background: none; border: none;
  color: var(--dim); cursor: pointer; font-size: 1.2rem; opacity: 0; transition: all 0.2s;
}
.card:hover .card-delete { opacity: 1; }
.card-delete:hover { color: var(--danger); }

.card-header { display: flex; justify-content: space-between; align-items: center; margin-bottom: 8px; }
.card-name { font-weight: 600; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; max-width: 200px; }
.card-time { font-size: 0.8rem; color: var(--dim); font-family: monospace; }

.card-status {
  display: inline-block; font-size: 0.75rem; font-weight: 600; padding: 3px 10px;
  border-radius: 4px; margin-bottom: 12px;
}
.card-status.done { color: var(--accent); border: 1px solid var(--accent); background: rgba(197,249,70,0.08); }
.card-status.running { color: #818cf8; border: 1px solid #818cf8; animation: blink 1s infinite; }
.card-status.queued { color: #fbbf24; border: 1px solid #fbbf24; }
.card-status.pending { color: var(--dim); border: 1px solid var(--border); }
.card-status.fail { color: var(--danger); border: 1px solid var(--danger); }

.card-actions { display: flex; gap: 8px; }
.btn-action {
  flex: 1; padding: 10px; border: 1px solid var(--border); background: var(--bg);
  color: var(--dim); border-radius: 8px; cursor: pointer; font-size: 0.85rem; transition: all 0.2s;
}
.btn-action:hover:not(:disabled) { color: var(--accent); border-color: var(--accent); }
.btn-action.ai { border-color: #818cf8; color: #818cf8; }
.btn-action.ai:hover:not(:disabled) { background: var(--accent); color: var(--bg); border-color: var(--accent); }
.btn-action:disabled { opacity: 0.3; cursor: not-allowed; }

.card-result { margin-top: 12px; padding-top: 12px; border-top: 1px solid var(--border); }
.loading-inline { display: flex; align-items: center; gap: 10px; justify-content: center; color: var(--dim); padding: 1rem; }
.result-block { margin-bottom: 1rem; }
.result-block h4 { font-size: 0.9rem; color: var(--accent); margin-bottom: 8px; }
.result-block pre {
  white-space: pre-wrap; font-size: 0.8rem; background: #000; padding: 12px;
  border-radius: 8px; border: 1px solid var(--border); max-height: 300px; overflow-y: auto;
}
.markdown { line-height: 1.7; font-size: 0.9rem; }
.markdown h2, .markdown h3 { color: var(--accent); margin-top: 1em; margin-bottom: 0.4em; }
.markdown strong { color: var(--accent); }
.markdown ul { padding-left: 20px; }
.markdown li { margin-bottom: 4px; }
.markdown blockquote { border-left: 3px solid var(--accent); padding-left: 12px; color: var(--dim); }

/* 弹窗 */
.modal-backdrop {
  position: fixed; inset: 0; background: rgba(0,0,0,0.8); backdrop-filter: blur(4px);
  display: flex; align-items: center; justify-content: center; z-index: 1000;
}
.modal {
  width: 380px; background: var(--card); border: 1px solid var(--border);
  border-top: 2px solid var(--accent); padding: 2rem; border-radius: 8px;
}
.modal h3 { margin-bottom: 1.5rem; text-align: center; }
.modal input {
  width: 100%; background: #000; border: 1px solid var(--border); padding: 10px 12px;
  color: var(--text); border-radius: 4px; margin-bottom: 12px; font-size: 0.9rem; outline: none;
}
.modal input:focus { border-color: var(--accent); }
.auth-switch { text-align: center; font-size: 0.85rem; color: var(--dim); margin-top: 1rem; }
.auth-switch a { color: var(--accent); text-decoration: underline; }
.auth-msg { text-align: center; margin-top: 10px; font-size: 0.8rem; color: var(--accent); }
.auth-msg.error { color: var(--danger); }

@keyframes spin { to { transform: rotate(360deg); } }
@keyframes blink { 50% { opacity: 0.4; } }
</style>
