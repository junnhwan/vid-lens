<template>
  <div v-if="!app.user" class="chat-gate">
    <div class="gate-icon">💬</div>
    <h3>登录后即可与视频对话</h3>
    <button class="btn-amber" @click="app.openAuth">登录 / 注册</button>
  </div>

  <div v-else class="chat-layout">
    <aside class="chat-sidebar">
      <div class="chat-sidebar-header">
        <h3>💬 对话视频</h3>
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
          <div class="video-item-icon">🎬</div>
          <div class="video-item-info">
            <div class="video-item-name">{{ t.filename }}</div>
            <div class="video-item-meta">{{ formatTime(t.created_at) }}</div>
          </div>
          <span v-if="isCurrent(t)" class="video-item-active-dot"></span>
        </button>
      </div>

      <div v-else class="video-list-empty">
        <div class="empty-icon">📭</div>
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
      <div v-else class="chat-placeholder">
        <div class="placeholder-icon">💬</div>
        <p>从左侧选择一个视频开始对话</p>
      </div>
    </main>
  </div>
</template>

<script setup>
import { computed, inject } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import VideoRAGChat from '../components/VideoRAGChat.vue'
import { formatTime } from '../utils/format.js'

const app = inject('appCtx')
const route = useRoute()
const router = useRouter()

// 可对话：已提取文字或处理完成（与 TaskDrawer 的 canUseRAG 判定一致）
const chattableTasks = computed(() =>
  (app.tasks || []).filter((t) => t.transcription?.content || t.status === 3),
)

const currentTask = computed(() => {
  const idParam = route.params.taskId
  if (idParam != null) {
    const id = Number(idParam)
    const found = (app.tasks || []).find((t) => t.id === id)
    if (found) return found
  }
  return chattableTasks.value[0] || null
})

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
  height: calc(100vh - 80px);
  max-width: 1600px;
  margin: 0 auto;
  position: relative;
  z-index: 2;
}

/* 左栏：视频选择 */
.chat-sidebar {
  width: 300px;
  flex-shrink: 0;
  display: flex;
  flex-direction: column;
  border-right: 1px solid rgba(212, 175, 55, 0.15);
  background: linear-gradient(135deg, rgba(10, 14, 26, 0.4), rgba(15, 25, 45, 0.3));
  backdrop-filter: blur(20px) saturate(180%);
  overflow: hidden;
}

.chat-sidebar-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 1.5rem 1.5rem 1rem;
  border-bottom: 1px solid rgba(139, 149, 168, 0.15);
}

.chat-sidebar-header h3 {
  font-size: 1.05rem;
  font-weight: 700;
  background: linear-gradient(135deg, #d4af37, #f4e4a6);
  -webkit-background-clip: text;
  -webkit-text-fill-color: transparent;
  background-clip: text;
  letter-spacing: 0.5px;
}

.back-link {
  background: transparent;
  border: 1px solid rgba(139, 149, 168, 0.2);
  color: #8b95a8;
  cursor: pointer;
  font-size: 0.78rem;
  padding: 0.35rem 0.6rem;
  border-radius: 0.5rem;
  transition: all 0.2s;
  font-family: 'JetBrains Mono', monospace;
}

.back-link:hover {
  border-color: rgba(212, 175, 55, 0.4);
  color: #d4af37;
}

.video-list {
  flex: 1;
  overflow-y: auto;
  padding: 0.75rem;
  display: flex;
  flex-direction: column;
  gap: 0.5rem;
  scrollbar-width: thin;
  scrollbar-color: rgba(212, 175, 55, 0.3) transparent;
}

.video-item {
  display: flex;
  align-items: center;
  gap: 0.75rem;
  padding: 0.75rem 0.85rem;
  border-radius: 0.75rem;
  border: 1px solid rgba(139, 149, 168, 0.15);
  background: linear-gradient(135deg, rgba(15, 25, 45, 0.5), rgba(20, 30, 50, 0.3));
  cursor: pointer;
  transition: all 0.25s cubic-bezier(0.4, 0, 0.2, 1);
  text-align: left;
  color: inherit;
  position: relative;
}

.video-item:hover {
  border-color: rgba(212, 175, 55, 0.4);
  transform: translateX(2px);
  box-shadow: 0 4px 16px rgba(212, 175, 55, 0.12);
}

.video-item.active {
  border-color: rgba(212, 175, 55, 0.55);
  background: linear-gradient(135deg, rgba(212, 175, 55, 0.14), rgba(41, 98, 255, 0.08));
  box-shadow: 0 2px 12px rgba(212, 175, 55, 0.2), inset 0 1px 0 rgba(255, 255, 255, 0.06);
}

.video-item-icon {
  font-size: 1.25rem;
  flex-shrink: 0;
}

.video-item-info {
  flex: 1;
  min-width: 0;
}

.video-item-name {
  font-size: 0.9rem;
  font-weight: 600;
  color: #e8eef7;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  margin-bottom: 0.2rem;
}

.video-item-meta {
  font-size: 0.72rem;
  color: #8b95a8;
  font-family: 'JetBrains Mono', monospace;
}

.video-item-active-dot {
  width: 0.5rem;
  height: 0.5rem;
  border-radius: 50%;
  background: #d4af37;
  box-shadow: 0 0 8px rgba(212, 175, 55, 0.7);
  flex-shrink: 0;
}

.video-list-empty {
  flex: 1;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 0.75rem;
  padding: 2rem 1.5rem;
  text-align: center;
}

.video-list-empty .empty-icon {
  font-size: 2.5rem;
  opacity: 0.6;
}

.video-list-empty p {
  color: #8b95a8;
  font-size: 0.88rem;
  margin: 0;
}

/* 右栏：对话区 */
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
  gap: 1rem;
  color: #8b95a8;
}

.placeholder-icon {
  font-size: 3.5rem;
  opacity: 0.5;
}

/* 未登录引导 */
.chat-gate {
  min-height: calc(100vh - 80px);
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 1.25rem;
  position: relative;
  z-index: 2;
}

.chat-gate .gate-icon {
  font-size: 4rem;
}

.chat-gate h3 {
  color: #e8eef7;
  font-weight: 600;
  letter-spacing: 0.5px;
}

.btn-amber {
  background: linear-gradient(135deg, #d4af37, #f4e4a6);
  color: #0a0e1a;
  border: none;
  padding: 0.75rem 1.75rem;
  border-radius: 0.75rem;
  font-weight: 600;
  cursor: pointer;
  transition: all 0.3s;
  font-size: 0.95rem;
}

.btn-amber:hover {
  transform: translateY(-2px);
  box-shadow: 0 6px 24px rgba(212, 175, 55, 0.4);
}

.btn-amber.small {
  padding: 0.5rem 1.1rem;
  font-size: 0.82rem;
}

/* 响应式 */
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
    height: 38vh;
    border-right: none;
    border-bottom: 1px solid rgba(212, 175, 55, 0.15);
  }
  .chat-main {
    height: 62vh;
  }
}
</style>
