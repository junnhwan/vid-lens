<template>
  <div v-if="!app.user" class="chat-gate">
    <div class="gate-mark" aria-hidden="true">
      <span class="gate-core"></span>
    </div>
    <h3>登录后即可与视频对话</h3>
    <p class="gate-sub">基于转写文本的 RAG 问答，带引用片段</p>
    <button class="btn-amber" @click="app.openAuth">登录 / 注册</button>
  </div>

  <div v-else class="chat-layout">
    <aside class="chat-sidebar" :class="{ 'mobile-collapsed': mobileListCollapsed && currentTask }">
      <div class="chat-sidebar-header">
        <div class="header-titles">
          <h3>对话视频</h3>
          <span class="header-count" v-if="chattableTasks.length">{{ chattableTasks.length }}</span>
        </div>
        <button class="back-link" type="button" @click="goLibrary" title="返回视频库">
          ← 视频库
        </button>
      </div>

      <div v-if="chattableTasks.length" class="video-list" role="listbox" aria-label="可对话视频">
        <button
          v-for="t in chattableTasks"
          :key="t.id"
          type="button"
          role="option"
          class="video-item"
          :class="{ active: isCurrent(t) }"
          :aria-selected="isCurrent(t)"
          @click="selectTask(t)"
        >
          <div class="video-item-icon" aria-hidden="true"></div>
          <div class="video-item-info">
            <div class="video-item-name">{{ t.title || t.filename }}</div>
            <div class="video-item-meta">{{ formatTime(t.created_at) }}</div>
          </div>
          <span v-if="isCurrent(t)" class="video-item-active-dot" aria-hidden="true"></span>
        </button>
      </div>

      <div v-else class="video-list-empty">
        <div class="empty-icon" aria-hidden="true">◎</div>
        <p>还没有可对话的视频</p>
        <p class="empty-hint">先在视频库提取文字，再回来提问</p>
        <button class="btn-amber small" type="button" @click="goLibrary">去提取文字</button>
      </div>
    </aside>

    <main class="chat-main">
      <!-- mobile: show current video strip when list is collapsed -->
      <div v-if="currentTask" class="mobile-current-bar">
        <button type="button" class="mobile-switch" @click="mobileListCollapsed = !mobileListCollapsed">
          <span class="mobile-switch-label">{{ mobileListCollapsed ? '切换视频' : '收起列表' }}</span>
          <span class="mobile-current-name">{{ currentTask.title || currentTask.filename }}</span>
        </button>
      </div>

      <VideoRAGChat
        v-if="currentTask"
        :key="currentTask.id"
        :task="currentTask"
        @error="onChatError"
      />
      <div v-else-if="resolving" class="chat-placeholder">
        <div class="spinner" aria-hidden="true"></div>
        <p>加载中…</p>
      </div>
      <div v-else-if="taskNotFound" class="chat-placeholder">
        <div class="placeholder-mark warn" aria-hidden="true"></div>
        <p class="placeholder-title">视频不存在或无权限访问</p>
        <p class="placeholder-sub">请从左侧选择其他视频，或返回视频库</p>
        <button class="btn-amber small" type="button" @click="goLibrary">返回视频库</button>
      </div>
      <div v-else class="chat-placeholder">
        <div class="placeholder-mark" aria-hidden="true"></div>
        <p class="placeholder-title">选择一个视频开始对话</p>
        <p class="placeholder-sub">从左侧列表点选；对话会记住你上次打开的视频</p>
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
import {
  parseChatTaskIdParam,
  readLastChatTaskId,
  resolveBareChatSelection,
  writeLastChatTaskId,
} from '../chatSelectionPolicy.js'

const app = inject('appCtx')
const route = useRoute()
const router = useRouter()

// 可对话：已提取文字或处理完成（与 TaskDetailPanel 的 canUseRAG 判定一致）
const chattableTasks = computed(() =>
  (app.tasks || []).filter((t) => t.transcription?.content || t.status === 3),
)

const currentTask = ref(null)
const taskNotFound = ref(false)
const resolving = ref(false)
const mobileListCollapsed = ref(true)
let resolveSeq = 0

const rememberTask = (task) => {
  if (task?.id != null) writeLastChatTaskId(task.id)
}

const findInList = (id) => (app.tasks || []).find((t) => Number(t.id) === Number(id)) || null

/**
 * 直达 /chat/:taskId：优先列表，否则 getTask；拉不到绝不回退到别的视频。
 * bare /chat：优先 lastChatTaskId，仅 1 个可对话视频时自动选；多个则等用户点选。
 * 若记忆命中且 URL 仍是 bare /chat，静默补全为 /chat/:id（replace，避免历史栈污染）。
 */
const resolveTask = async () => {
  const seq = ++resolveSeq
  const parsed = parseChatTaskIdParam(route.params.taskId)

  if (parsed.ok === false && parsed.invalid) {
    taskNotFound.value = true
    currentTask.value = null
    resolving.value = false
    return
  }

  if (parsed.ok === false && parsed.missing) {
    taskNotFound.value = false
    const preferredId = resolveBareChatSelection({
      lastTaskId: readLastChatTaskId(),
      chattableTaskIds: chattableTasks.value.map((t) => t.id),
    })
    if (preferredId != null) {
      const found = findInList(preferredId)
      if (found) {
        currentTask.value = found
        rememberTask(found)
        // 补全 URL，Navbar「对话」与刷新行为一致
        router.replace({ name: 'chat-task', params: { taskId: preferredId } })
        resolving.value = false
        return
      }
      // 列表尚未加载到该条：尝试拉详情
      resolving.value = true
      try {
        const detail = await api.getTask(preferredId)
        if (seq !== resolveSeq) return
        if (detail) {
          currentTask.value = detail
          rememberTask(detail)
          if (Array.isArray(app.tasks) && !app.tasks.some((t) => Number(t.id) === Number(detail.id))) {
            app.tasks.push(detail)
          }
          router.replace({ name: 'chat-task', params: { taskId: preferredId } })
        } else {
          currentTask.value = null
        }
      } catch {
        if (seq !== resolveSeq) return
        // 记忆失效：清掉并等用户选
        writeLastChatTaskId(null)
        currentTask.value = null
      } finally {
        if (seq === resolveSeq) resolving.value = false
      }
      return
    }
    currentTask.value = null
    resolving.value = false
    return
  }

  // explicit :taskId
  const id = parsed.id
  const found = findInList(id)
  if (found) {
    taskNotFound.value = false
    currentTask.value = found
    rememberTask(found)
    resolving.value = false
    return
  }

  resolving.value = true
  try {
    const detail = await api.getTask(id)
    if (seq !== resolveSeq) return
    taskNotFound.value = !detail
    currentTask.value = detail || null
    if (detail) {
      rememberTask(detail)
      if (Array.isArray(app.tasks) && !app.tasks.some((t) => Number(t.id) === Number(detail.id))) {
        app.tasks.push(detail)
      }
    }
  } catch (err) {
    if (seq !== resolveSeq) return
    taskNotFound.value = true
    currentTask.value = null
    app.showToast(err.message || '视频不存在或无权限', true)
  } finally {
    if (seq === resolveSeq) resolving.value = false
  }
}

watch(() => route.params.taskId, resolveTask, { immediate: true })
// 首次进入时任务列表可能还没加载完，加载后重新解析一次
watch(() => (app.tasks || []).length, () => {
  // 已有明确选中且仍在列表中则不动，避免覆盖用户选择
  if (currentTask.value && findInList(currentTask.value.id)) return
  resolveTask()
})

// 同步 currentTask 字段（轮询更新转写等）
watch(
  () => {
    const id = currentTask.value?.id
    if (id == null) return null
    return findInList(id)
  },
  (updated) => {
    if (updated && currentTask.value && Number(updated.id) === Number(currentTask.value.id)) {
      currentTask.value = { ...currentTask.value, ...updated }
    }
  },
)

const isCurrent = (task) => Number(currentTask.value?.id) === Number(task.id)

const selectTask = (task) => {
  rememberTask(task)
  mobileListCollapsed.value = true
  if (Number(route.params.taskId) === Number(task.id)) {
    currentTask.value = task
    return
  }
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
  animation: vl-fade-in-up 0.35s var(--vl-ease) both;
}

.chat-sidebar {
  width: 300px;
  flex-shrink: 0;
  display: flex;
  flex-direction: column;
  border-right: 1px solid var(--vl-border);
  background:
    linear-gradient(180deg, var(--vl-panel) 0%, var(--vl-panel) 100%);
  overflow: hidden;
}

.chat-sidebar-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 0.75rem;
  padding: 1.15rem 1.15rem 1rem;
  border-bottom: 1px solid var(--vl-border);
}

.header-titles {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  min-width: 0;
}

.chat-sidebar-header h3 {
  margin: 0;
  font-family: var(--vl-font-display);
  font-size: 0.95rem;
  font-weight: 700;
  color: var(--vl-text);
  letter-spacing: 0.03em;
}

.header-count {
  font-family: var(--vl-font-mono);
  font-size: 0.7rem;
  font-weight: 600;
  color: var(--vl-primary);
  background: var(--vl-primary-dim);
  border: 1px solid var(--vl-primary-glow);
  border-radius: 999px;
  padding: 0.12rem 0.45rem;
  line-height: 1.3;
}

.back-link {
  background: transparent;
  border: 1px solid var(--vl-border);
  color: var(--vl-text-muted);
  cursor: pointer;
  font-size: 0.75rem;
  padding: 0.35rem 0.65rem;
  border-radius: var(--vl-radius-sm);
  transition: border-color 0.2s, color 0.2s, background 0.2s;
  font-family: var(--vl-font-mono);
  white-space: nowrap;
}

.back-link:hover {
  border-color: var(--vl-border-focus);
  color: var(--vl-primary);
  background: var(--vl-primary-dim);
}

.video-list {
  flex: 1;
  overflow-y: auto;
  padding: 0.7rem;
  display: flex;
  flex-direction: column;
  gap: 0.35rem;
}

.video-item {
  display: flex;
  align-items: center;
  gap: 0.7rem;
  padding: 0.75rem 0.8rem;
  border-radius: var(--vl-radius);
  border: 1px solid transparent;
  background: transparent;
  cursor: pointer;
  transition: background 0.2s var(--vl-ease), border-color 0.2s, transform 0.15s;
  text-align: left;
  color: inherit;
  width: 100%;
}

.video-item:hover {
  background: var(--vl-white-a03);
  border-color: var(--vl-border);
}

.video-item.active {
  border-color: var(--vl-border-focus);
  background: linear-gradient(135deg, var(--vl-primary-dim), var(--vl-info-dim));
  box-shadow: inset 0 0 0 1px var(--vl-primary-dim);
}

.video-item-icon {
  width: 0.5rem;
  height: 0.5rem;
  border-radius: 2px;
  flex-shrink: 0;
  background: linear-gradient(135deg, var(--vl-primary), var(--vl-info));
  opacity: 0.55;
  transition: opacity 0.2s, box-shadow 0.2s;
}

.video-item.active .video-item-icon {
  opacity: 1;
  box-shadow: 0 0 10px var(--vl-primary-glow);
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
  margin-bottom: 0.18rem;
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
  gap: 0.55rem;
  padding: 1.5rem;
  text-align: center;
}

.video-list-empty .empty-icon {
  width: 2.4rem;
  height: 2.4rem;
  border-radius: 0.7rem;
  display: grid;
  place-items: center;
  font-size: 1.1rem;
  color: var(--vl-primary);
  background: var(--vl-primary-dim);
  border: 1px solid var(--vl-primary-glow);
  margin-bottom: 0.35rem;
}

.video-list-empty p {
  color: var(--vl-text-secondary);
  font-size: 0.88rem;
  margin: 0;
  font-weight: 500;
}

.video-list-empty .empty-hint {
  color: var(--vl-text-muted);
  font-size: 0.78rem;
  font-weight: 400;
  margin-bottom: 0.4rem;
}

.chat-main {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  overflow: hidden;
  background: var(--vl-surface);
}

.mobile-current-bar {
  display: none;
}

.chat-placeholder {
  flex: 1;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 0.55rem;
  color: var(--vl-text-muted);
  font-size: 0.92rem;
  padding: 2rem;
  text-align: center;
}

.placeholder-mark {
  width: 3.2rem;
  height: 3.2rem;
  border-radius: 1rem;
  margin-bottom: 0.5rem;
  background: linear-gradient(145deg, var(--vl-primary-dim), var(--vl-info-dim));
  border: 1px solid var(--vl-primary-glow);
  box-shadow: 0 0 28px var(--vl-primary-dim);
}

.placeholder-mark.warn {
  background: linear-gradient(145deg, var(--vl-danger-dim), var(--vl-danger-dim));
  border-color: color-mix(in srgb, var(--vl-danger) 30%, transparent);
  box-shadow: 0 0 28px var(--vl-danger-dim);
}

.placeholder-title {
  margin: 0;
  color: var(--vl-text);
  font-weight: 600;
  font-size: 1rem;
}

.placeholder-sub {
  margin: 0 0 0.65rem;
  color: var(--vl-text-muted);
  font-size: 0.84rem;
  max-width: 22rem;
  line-height: 1.5;
}

.spinner {
  width: 1.35rem;
  height: 1.35rem;
  border: 2.5px solid var(--vl-primary-dim);
  border-top-color: var(--vl-primary);
  border-radius: 50%;
  animation: vl-spin 0.8s linear infinite;
  margin-bottom: 0.35rem;
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
  animation: vl-fade-in-up 0.35s var(--vl-ease) both;
}

.gate-mark {
  width: 3.2rem;
  height: 3.2rem;
  border-radius: 1rem;
  background: linear-gradient(145deg, var(--vl-primary-glow), var(--vl-info-dim));
  border: 1px solid var(--vl-primary-glow);
  box-shadow: 0 0 28px var(--vl-primary-glow);
  margin-bottom: 0.5rem;
  display: grid;
  place-items: center;
}

.gate-core {
  width: 0.85rem;
  height: 0.85rem;
  border-radius: 50%;
  background: radial-gradient(circle at 30% 30%, var(--vl-primary), var(--vl-primary-deep) 70%, var(--vl-primary-deep));
  box-shadow: 0 0 12px var(--vl-primary-glow);
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
    width: 240px;
  }
}

@media (max-width: 640px) {
  .chat-layout {
    flex-direction: column;
  }

  .chat-sidebar {
    width: 100%;
    height: auto;
    max-height: 38vh;
    border-right: none;
    border-bottom: 1px solid var(--vl-border);
  }

  .chat-sidebar.mobile-collapsed {
    display: none;
  }

  .mobile-current-bar {
    display: block;
    padding: 0.55rem 0.85rem;
    border-bottom: 1px solid var(--vl-border);
    background: var(--vl-panel);
  }

  .mobile-switch {
    width: 100%;
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: 0.15rem;
    background: transparent;
    border: 1px solid var(--vl-border);
    border-radius: var(--vl-radius);
    padding: 0.55rem 0.75rem;
    color: inherit;
    cursor: pointer;
    text-align: left;
    transition: border-color 0.2s, background 0.2s;
  }

  .mobile-switch:hover {
    border-color: var(--vl-border-focus);
    background: var(--vl-primary-dim);
  }

  .mobile-switch-label {
    font-size: 0.68rem;
    font-family: var(--vl-font-mono);
    color: var(--vl-primary);
    letter-spacing: 0.04em;
  }

  .mobile-current-name {
    font-size: 0.86rem;
    font-weight: 600;
    color: var(--vl-text);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    max-width: 100%;
  }

  .chat-main {
    flex: 1;
    min-height: 0;
  }
}
</style>
