<template>
  <div v-if="!app.user" class="chat-gate">
    <div class="gate-mark" aria-hidden="true"></div>
    <h3>登录后即可与视频对话</h3>
    <p class="gate-sub">基于转写文本的 RAG 问答，带引用片段</p>
    <button class="btn-amber" @click="app.openAuth">登录 / 注册</button>
  </div>

  <div v-else class="chat-layout">
    <aside class="chat-sidebar">
      <div class="chat-sidebar-header">
        <h3>对话视频</h3>
        <button class="back-link" @click="goLibrary" title="返回视频处理">
          ← 处理
        </button>
      </div>

      <div v-if="chattableTasks.length" class="video-list">
        <button
          v-for="t in chattableTasks"
          :key="t.id"
          class="video-item"
          :class="{ active: isCurrent(t) }"
          @click="selectTask(t)"
        >
          <div class="video-item-icon" aria-hidden="true"></div>
          <div class="video-item-info">
            <div class="video-item-name">{{ t.title || t.filename }}</div>
            <div class="video-item-meta">{{ formatTime(t.created_at) }}</div>
          </div>
          <span v-if="isCurrent(t)" class="video-item-active-dot"></span>
        </button>
      </div>

      <div v-else class="video-list-empty">
        <p>还没有可对话的视频</p>
        <button class="btn-amber small" @click="goLibrary">去提取文字</button>
      </div>
    </aside>

    <main class="chat-main">
      <VideoRAGChat
        v-if="currentTask"
        :key="currentTask.id"
        :task="currentTask"
        @error="onChatError"
      />
      <div v-else-if="taskNotFound" class="chat-placeholder">
        <p>视频不存在或无权限访问</p>
        <button class="btn-amber small" @click="goLibrary">返回视频处理</button>
      </div>
      <div v-else class="chat-placeholder">
        <p>从左侧选择一个视频开始对话</p>
      </div>
    </main>
  </div>
</template>

<script setup>
import { computed, inject, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import api from '../api'
import VideoRAGChat from '../components/VideoRAGChat.vue'
import { formatTime } from '../utils/format.js'

const app = inject('appCtx')
const route = useRoute()
const router = useRouter()

// 可对话：已提取文字或处理完成（与 TaskDrawer 的 canUseRAG 判定一致）
const chattableTasks = computed(() =>
  (app.tasks || []).filter((t) => t.transcription?.content || t.status === 3),
)

const currentTask = ref(null)
const taskNotFound = ref(false)

// 直达 /chat/:taskId 时：优先用已加载列表；分页没加载到就单独 getTask 拉详情；
// 拉不到（不存在 / 无权限）就显示提示，绝不回退到别的视频。
const resolveTask = async () => {
  const idParam = route.params.taskId
  if (idParam == null || idParam === '') {
    taskNotFound.value = false
    currentTask.value = chattableTasks.value[0] || null
    return
  }
  const id = Number(idParam)
  const found = (app.tasks || []).find((t) => t.id === id)
  if (found) {
    taskNotFound.value = false
    currentTask.value = found
    return
  }
  try {
    const detail = await api.getTask(id)
    taskNotFound.value = false
    currentTask.value = detail || null
    // 让该视频也出现在左侧列表（若尚未加载）
    if (detail && Array.isArray(app.tasks) && !app.tasks.some((t) => t.id === detail.id)) {
      app.tasks.push(detail)
    }
  } catch (err) {
    taskNotFound.value = true
    currentTask.value = null
    app.showToast(err.message || '视频不存在或无权限', true)
  }
}

watch(() => route.params.taskId, resolveTask, { immediate: true })
// 首次进入时任务列表可能还没加载完，加载后重新解析一次
watch(() => (app.tasks || []).length, resolveTask)

const isCurrent = (task) => currentTask.value?.id === task.id

const selectTask = (task) => {
  router.push({ name: 'chat-task', params: { taskId: task.id } })
}

const goLibrary = () => router.push({ name: 'library' })

const onChatError = (msg) => app.showToast(msg, true)
</script>

<style scoped>
.chat-layout {
  display: flex;
  height: calc(100vh - var(--vl-nav-h));
  max-width: 1440px;
  margin: 0 auto;
}

.chat-sidebar {
  width: 280px;
  flex-shrink: 0;
  display: flex;
  flex-direction: column;
  border-right: 1px solid var(--vl-border);
  background: rgba(8, 11, 18, 0.55);
  overflow: hidden;
}

.chat-sidebar-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 1.1rem 1.1rem 0.9rem;
  border-bottom: 1px solid var(--vl-border);
}

.chat-sidebar-header h3 {
  margin: 0;
  font-family: var(--vl-font-display);
  font-size: 0.95rem;
  font-weight: 700;
  color: var(--vl-text);
  letter-spacing: 0.03em;
}

.back-link {
  background: transparent;
  border: 1px solid var(--vl-border);
  color: var(--vl-text-muted);
  cursor: pointer;
  font-size: 0.75rem;
  padding: 0.3rem 0.55rem;
  border-radius: var(--vl-radius-sm);
  transition: all 0.2s;
  font-family: var(--vl-font-mono);
}

.back-link:hover {
  border-color: rgba(45, 212, 191, 0.4);
  color: var(--vl-primary);
}

.video-list {
  flex: 1;
  overflow-y: auto;
  padding: 0.65rem;
  display: flex;
  flex-direction: column;
  gap: 0.4rem;
}

.video-item {
  display: flex;
  align-items: center;
  gap: 0.65rem;
  padding: 0.7rem 0.75rem;
  border-radius: var(--vl-radius);
  border: 1px solid transparent;
  background: transparent;
  cursor: pointer;
  transition: all 0.2s var(--vl-ease);
  text-align: left;
  color: inherit;
  width: 100%;
}

.video-item:hover {
  background: rgba(255, 255, 255, 0.03);
  border-color: var(--vl-border);
}

.video-item.active {
  border-color: rgba(45, 212, 191, 0.4);
  background: var(--vl-primary-dim);
}

.video-item-icon {
  width: 0.55rem;
  height: 0.55rem;
  border-radius: 2px;
  flex-shrink: 0;
  background: linear-gradient(135deg, var(--vl-primary), #38bdf8);
  opacity: 0.7;
}

.video-item.active .video-item-icon {
  opacity: 1;
  box-shadow: 0 0 8px var(--vl-primary-glow);
}

.video-item-info {
  flex: 1;
  min-width: 0;
}

.video-item-name {
  font-size: 0.88rem;
  font-weight: 600;
  color: var(--vl-text);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  margin-bottom: 0.15rem;
}

.video-item-meta {
  font-size: 0.7rem;
  color: var(--vl-text-muted);
  font-family: var(--vl-font-mono);
}

.video-item-active-dot {
  width: 0.4rem;
  height: 0.4rem;
  border-radius: 50%;
  background: var(--vl-primary);
  box-shadow: 0 0 8px var(--vl-primary-glow);
  flex-shrink: 0;
}

.video-list-empty {
  flex: 1;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 0.85rem;
  padding: 1.5rem;
  text-align: center;
}

.video-list-empty p {
  color: var(--vl-text-muted);
  font-size: 0.86rem;
  margin: 0;
}

.chat-main {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  overflow: hidden;
}

.chat-placeholder {
  flex: 1;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 0.9rem;
  color: var(--vl-text-muted);
  font-size: 0.92rem;
}

.chat-gate {
  min-height: calc(100vh - var(--vl-nav-h));
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 0.75rem;
  padding: 2rem;
  text-align: center;
}

.gate-mark {
  width: 3rem;
  height: 3rem;
  border-radius: 1rem;
  background: linear-gradient(145deg, rgba(45, 212, 191, 0.25), rgba(96, 165, 250, 0.12));
  border: 1px solid rgba(45, 212, 191, 0.35);
  box-shadow: 0 0 28px rgba(45, 212, 191, 0.2);
  margin-bottom: 0.5rem;
}

.chat-gate h3 {
  margin: 0;
  color: var(--vl-text);
  font-family: var(--vl-font-display);
  font-weight: 700;
  letter-spacing: 0.02em;
}

.gate-sub {
  margin: 0 0 0.5rem;
  color: var(--vl-text-muted);
  font-size: 0.88rem;
}

@media (max-width: 900px) {
  .chat-sidebar {
    width: 220px;
  }
}

@media (max-width: 640px) {
  .chat-layout {
    flex-direction: column;
  }
  .chat-sidebar {
    width: 100%;
    height: 36vh;
    border-right: none;
    border-bottom: 1px solid var(--vl-border);
  }
  .chat-main {
    height: 64vh;
  }
}
</style>
