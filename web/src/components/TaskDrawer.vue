<template>
  <transition name="drawer">
    <div v-if="task" class="drawer-backdrop" @click="$emit('close')" role="dialog" aria-modal="true" :aria-label="task.filename">
      <div class="task-drawer" ref="drawerPanel" @click.stop>
        <div class="drawer-header">
          <h3>{{ task.filename }}</h3>
          <button class="drawer-close" @click="$emit('close')" aria-label="关闭">×</button>
        </div>

        <div class="drawer-content">
          <div class="drawer-tabs">
            <button
              :class="['tab-btn', { active: activeTab === 'detail' }]"
              @click="activeTab = 'detail'"
            >
              📋 详情
            </button>
            <button
              :class="['tab-btn', { active: activeTab === 'chat' }]"
              @click="activeTab = 'chat'"
              :disabled="!canUseRAG"
              :title="canUseRAG ? '问问这个视频' : '需要先完成文字提取'"
            >
              💬 问问视频
            </button>
          </div>

          <div v-if="activeTab === 'detail'">
          <div class="drawer-meta">
            <div class="meta-item">
              <span class="meta-label">创建时间</span>
              <span class="meta-value">{{ formatTime(task.created_at) }}</span>
            </div>
            <div class="meta-item">
              <span class="meta-label">文件大小</span>
              <span class="meta-value">{{ formatFileSize(task.file_size) }}</span>
            </div>
            <div class="meta-item">
              <span class="meta-label">状态</span>
              <span class="meta-status" :class="detailedStatus.class">
                <span class="status-icon">{{ detailedStatus.icon }}</span>
                {{ detailedStatus.text }}
              </span>
            </div>
          </div>

          <!-- 失败 / 重试信息 -->
          <div v-if="errorMsg || task.next_retry_at" class="retry-info">
            <div v-if="errorMsg" class="retry-error">
              <span class="retry-label">错误:</span>
              <span class="retry-text">{{ errorMsg }}</span>
            </div>
            <div v-if="task.retry_count !== undefined" class="retry-count">
              <span class="retry-label">重试次数:</span>
              <span class="retry-text">{{ task.retry_count }} / {{ task.max_retries || 3 }}</span>
            </div>
            <div v-if="task.next_retry_at" class="retry-next">
              <span class="retry-label">下次重试:</span>
              <span class="retry-text">{{ formatRelativeTime(task.next_retry_at) }}</span>
            </div>
          </div>

          <!-- 任务处理明细 -->
          <div v-if="task.jobs && task.jobs.length" class="task-jobs-section">
            <h4 class="jobs-header">🔧 处理明细</h4>
            <div class="jobs-list">
              <div v-for="job in task.jobs" :key="job.id" class="job-item">
                <div class="job-header">
                  <span class="job-type">{{ jobTypeLabel(job.job_type) }}</span>
                  <span class="job-status" :class="jobStatusClass(job.status)">
                    {{ jobStatusLabel(job.status) }}
                  </span>
                </div>
                <div v-if="job.stage" class="job-stage">
                  <span class="job-label">阶段:</span>
                  <span class="job-value">{{ job.stage }}</span>
                </div>
                <div v-if="job.retry_count !== undefined" class="job-retry">
                  <span class="job-label">重试:</span>
                  <span class="job-value">{{ job.retry_count }} / {{ job.max_retries || 3 }}</span>
                </div>
                <div v-if="job.next_retry_at" class="job-next-retry">
                  <span class="job-label">下次重试:</span>
                  <span class="job-value">{{ formatRelativeTime(job.next_retry_at) }}</span>
                </div>
                <div v-if="job.last_error_msg" class="job-error">
                  <span class="job-label">错误:</span>
                  <span class="job-error-text">{{ job.last_error_msg }}</span>
                </div>
                <div v-if="job.trace_id" class="job-trace">
                  <span class="job-label">Trace ID:</span>
                  <span class="job-trace-id">{{ job.trace_id.slice(0, 16) }}...</span>
                </div>
              </div>
            </div>
          </div>

          <div class="drawer-actions">
            <button class="drawer-action-btn" @click.stop="$emit('transcribe')" :disabled="isActionDisabled">
              <span class="btn-icon">📄</span> 提取文字
            </button>
            <button class="drawer-action-btn amber" @click.stop="$emit('analyze')" :disabled="isActionDisabled">
              <span class="btn-icon">🤖</span> AI 总结
            </button>
          </div>

          <div v-if="loading" class="drawer-loading">
            <div class="spinner"></div>
            <span>处理中...</span>
          </div>

          <template v-else>
            <div v-if="failureMessage" class="drawer-result-block error-block">
              <h4>❌ 处理失败</h4>
              <p class="error-text">{{ failureMessage }}</p>
            </div>

            <div v-if="task.transcription?.content" class="drawer-result-block">
              <div class="result-header">
                <h4>📝 文字提取</h4>
                <div class="result-actions">
                  <button class="icon-btn" @click="copyText(task.transcription.content)" title="复制">
                    📋
                  </button>
                  <button class="icon-btn" @click="downloadText(task.transcription.content, task.filename)" title="下载为 TXT">
                    ⬇️
                  </button>
                </div>
              </div>
              <pre class="result-text">{{ transcriptionPreview }}</pre>
              <button
                v-if="showTranscriptionExpand"
                class="expand-btn"
                @click="transcriptionExpanded = !transcriptionExpanded"
              >
                {{ transcriptionExpanded ? '收起 ▲' : '展开全部 ▼' }}
              </button>
            </div>

            <div v-if="task.summary?.content" class="drawer-result-block">
              <div class="result-header">
                <h4>🤖 AI 总结</h4>
                <div class="result-actions">
                  <button class="icon-btn" @click="copyText(task.summary.content)" title="复制">
                    📋
                  </button>
                  <button class="icon-btn" @click="downloadMarkdown(task.summary.content, task.filename)" title="下载为 MD">
                    ⬇️
                  </button>
                </div>
              </div>
              <div class="result-markdown" v-html="renderMarkdown(summaryPreview)"></div>
              <button
                v-if="showSummaryExpand"
                class="expand-btn"
                @click="summaryExpanded = !summaryExpanded"
              >
                {{ summaryExpanded ? '收起 ▲' : '展开全部 ▼' }}
              </button>
            </div>
          </template>
          </div>

          <div v-if="activeTab === 'chat'" class="chat-tab">
            <VideoRAGChat :task="task" @error="handleChatError" />
          </div>
        </div>
      </div>
    </div>
  </transition>
</template>

<script setup>
import { ref, computed, watch, onMounted, onUnmounted, nextTick } from 'vue'
import { marked } from 'marked'
import DOMPurify from 'dompurify'
import { isTaskActionDisabled } from '../taskActionPolicy.js'
import { taskFailureMessage } from '../taskDetailPolicy.js'
import { formatTime, formatFileSize, getDetailedStatus, getErrorMessage, formatRelativeTime } from '../utils/format.js'
import VideoRAGChat from './VideoRAGChat.vue'

const props = defineProps({
  task: Object,
  loading: Boolean
})

const emit = defineEmits(['close', 'transcribe', 'analyze', 'chatError', 'toast'])

const activeTab = ref('detail')
const drawerPanel = ref(null)
let previouslyFocused = null

// 文本折叠状态
const transcriptionExpanded = ref(false)
const summaryExpanded = ref(false)

const transcriptionPreview = computed(() => {
  const content = props.task?.transcription?.content || ''
  if (!content || transcriptionExpanded.value) return content
  const lines = content.split('\n')
  return lines.length > 10 ? lines.slice(0, 10).join('\n') : content
})

const summaryPreview = computed(() => {
  const content = props.task?.summary?.content || ''
  if (!content || summaryExpanded.value) return content
  const lines = content.split('\n')
  return lines.length > 15 ? lines.slice(0, 15).join('\n') : content
})

const showTranscriptionExpand = computed(() => {
  const content = props.task?.transcription?.content || ''
  return content.split('\n').length > 10
})

const showSummaryExpand = computed(() => {
  const content = props.task?.summary?.content || ''
  return content.split('\n').length > 15
})

const canUseRAG = computed(() => {
  return props.task?.transcription?.content || props.task?.status === 3
})

const handleChatError = (msg) => {
  emit('chatError', msg)
}

const renderMarkdown = (content) => DOMPurify.sanitize(marked.parse(content || ''))

const isActionDisabled = computed(() => isTaskActionDisabled(props.task, props.loading))
const failureMessage = computed(() => taskFailureMessage(props.task))
const detailedStatus = computed(() => getDetailedStatus(props.task))
const errorMsg = computed(() => getErrorMessage(props.task))

// 复制和下载功能
const copyText = async (text) => {
  try {
    await navigator.clipboard.writeText(text)
    emit('toast', '已复制到剪贴板')
  } catch (err) {
    emit('chatError', '复制失败')
  }
}

const downloadText = (content, filename) => {
  const blob = new Blob([content], { type: 'text/plain;charset=utf-8' })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = `${filename}_文字提取.txt`
  a.click()
  URL.revokeObjectURL(url)
}

const downloadMarkdown = (content, filename) => {
  const blob = new Blob([content], { type: 'text/markdown;charset=utf-8' })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = `${filename}_AI总结.md`
  a.click()
  URL.revokeObjectURL(url)
}

// Job 辅助函数
const jobTypeLabel = (type) => {
  const labels = {
    'download': '下载',
    'transcribe': '转录',
    'analyze': '分析',
    'rag_index': 'RAG 索引'
  }
  return labels[type] || type
}

const jobStatusLabel = (status) => {
  const labels = {
    'queued': '排队中',
    'running': '运行中',
    'completed': '已完成',
    'failed': '失败',
    'retrying': '重试中'
  }
  return labels[status] || status
}

const jobStatusClass = (status) => {
  const classes = {
    'queued': 'job-status-queued',
    'running': 'job-status-running',
    'completed': 'job-status-completed',
    'failed': 'job-status-failed',
    'retrying': 'job-status-retrying'
  }
  return classes[status] || ''
}

// ESC 关闭 + Focus trap
const onKeyDown = (e) => {
  if (!props.task) return
  if (e.key === 'Escape') {
    e.preventDefault()
    emit('close')
    return
  }
  // 基本焦点捕获
  if (e.key === 'Tab' && drawerPanel.value) {
    const focusable = drawerPanel.value.querySelectorAll(
      'button, [href], input, select, textarea, [tabindex]:not([tabindex="-1"])'
    )
    if (focusable.length === 0) return
    const first = focusable[0]
    const last = focusable[focusable.length - 1]
    if (e.shiftKey && document.activeElement === first) {
      e.preventDefault()
      last.focus()
    } else if (!e.shiftKey && document.activeElement === last) {
      e.preventDefault()
      first.focus()
    }
  }
}

watch(() => props.task, (val) => {
  if (val) {
    previouslyFocused = document.activeElement
    nextTick(() => {
      const firstBtn = drawerPanel.value?.querySelector('button')
      firstBtn?.focus()
    })
  } else if (previouslyFocused) {
    previouslyFocused.focus()
    previouslyFocused = null
  }
})

onMounted(() => document.addEventListener('keydown', onKeyDown))
onUnmounted(() => document.removeEventListener('keydown', onKeyDown))
</script>

<style scoped>
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
  display: flex;
  flex-direction: column;
}

.drawer-tabs {
  display: flex;
  gap: 0.75rem;
  margin-bottom: 1.5rem;
  border-bottom: 1px solid rgba(139, 149, 168, 0.15);
  padding-bottom: 1rem;
}

.tab-btn {
  background: linear-gradient(135deg, rgba(15, 25, 45, 0.5), rgba(20, 30, 50, 0.4));
  border: 1px solid rgba(139, 149, 168, 0.2);
  padding: 0.75rem 1.5rem;
  border-radius: 0.75rem 0.75rem 0 0;
  color: #8b95a8;
  cursor: pointer;
  transition: all 0.3s;
  font-size: 0.95rem;
  font-weight: 600;
}

.tab-btn:hover:not(:disabled) {
  border-color: rgba(212, 175, 55, 0.3);
  color: #d4af37;
}

.tab-btn.active {
  background: linear-gradient(135deg, rgba(212, 175, 55, 0.15), rgba(41, 98, 255, 0.1));
  border-color: rgba(212, 175, 55, 0.4);
  color: #d4af37;
  box-shadow: 0 2px 8px rgba(212, 175, 55, 0.2);
}

.tab-btn:disabled {
  opacity: 0.3;
  cursor: not-allowed;
}

.chat-tab {
  flex: 1;
  display: flex;
  flex-direction: column;
  min-height: 0;
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

.meta-status {
  padding: 0.25rem 0.75rem;
  border-radius: 0.5rem;
  font-weight: 600;
  font-size: 0.8rem;
  letter-spacing: 0.5px;
  text-transform: uppercase;
  backdrop-filter: blur(8px);
  border: 1px solid;
  display: inline-flex;
  align-items: center;
  gap: 0.35rem;
  width: fit-content;
}

.status-icon {
  font-size: 0.9rem;
  line-height: 1;
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
  animation: vl-status-pulse 2s ease-in-out infinite;
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

.meta-status.retrying {
  background: rgba(245, 158, 11, 0.15);
  color: #fbbf24;
  border-color: rgba(245, 158, 11, 0.3);
  box-shadow: 0 0 12px rgba(245, 158, 11, 0.2);
}

.meta-status.dead {
  background: rgba(100, 116, 139, 0.15);
  color: #94a3b8;
  border-color: rgba(100, 116, 139, 0.3);
}

/* 重试信息 */
.retry-info {
  background: linear-gradient(135deg, rgba(245, 158, 11, 0.08), rgba(239, 68, 68, 0.05));
  border: 1px solid rgba(245, 158, 11, 0.3);
  border-radius: 0.875rem;
  padding: 1.25rem;
  margin-bottom: 2rem;
  backdrop-filter: blur(8px);
}

/* 任务处理明细 */
.task-jobs-section {
  background: linear-gradient(135deg, rgba(15, 25, 45, 0.5), rgba(20, 30, 50, 0.4));
  border: 1px solid rgba(139, 149, 168, 0.2);
  border-radius: 1rem;
  padding: 1.5rem;
  margin-bottom: 2rem;
}

.jobs-header {
  font-size: 1.1rem;
  background: linear-gradient(135deg, #d4af37, #f4e4a6);
  -webkit-background-clip: text;
  -webkit-text-fill-color: transparent;
  background-clip: text;
  font-weight: 700;
  margin-bottom: 1rem;
}

.jobs-list {
  display: flex;
  flex-direction: column;
  gap: 0.75rem;
}

.job-item {
  background: rgba(10, 14, 26, 0.5);
  border: 1px solid rgba(139, 149, 168, 0.15);
  border-radius: 0.75rem;
  padding: 1rem;
}

.job-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 0.75rem;
}

.job-type {
  font-weight: 600;
  color: #e8eef7;
  font-size: 0.95rem;
}

.job-status {
  padding: 0.25rem 0.65rem;
  border-radius: 0.4rem;
  font-size: 0.75rem;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.5px;
}

.job-status-queued {
  background: rgba(41, 98, 255, 0.15);
  color: #5b8fff;
  border: 1px solid rgba(41, 98, 255, 0.3);
}

.job-status-running {
  background: rgba(212, 175, 55, 0.15);
  color: #f4e4a6;
  border: 1px solid rgba(212, 175, 55, 0.3);
}

.job-status-completed {
  background: rgba(34, 197, 94, 0.15);
  color: #4ade80;
  border: 1px solid rgba(34, 197, 94, 0.3);
}

.job-status-failed {
  background: rgba(239, 68, 68, 0.15);
  color: #f87171;
  border: 1px solid rgba(239, 68, 68, 0.3);
}

.job-status-retrying {
  background: rgba(245, 158, 11, 0.15);
  color: #fbbf24;
  border: 1px solid rgba(245, 158, 11, 0.3);
}

.job-stage,
.job-retry,
.job-next-retry,
.job-error,
.job-trace {
  display: flex;
  gap: 0.5rem;
  margin-bottom: 0.5rem;
  font-size: 0.85rem;
}

.job-stage:last-child,
.job-retry:last-child,
.job-next-retry:last-child,
.job-error:last-child,
.job-trace:last-child {
  margin-bottom: 0;
}

.job-label {
  color: #8b95a8;
  font-weight: 600;
  min-width: 70px;
}

.job-value {
  color: #b8c5db;
  font-family: 'JetBrains Mono', monospace;
}

.job-error-text {
  color: #fca5a5;
  font-family: 'JetBrains Mono', monospace;
  flex: 1;
  word-break: break-word;
}

.job-trace-id {
  color: #8b95a8;
  font-family: 'JetBrains Mono', monospace;
  font-size: 0.75rem;
}

.retry-error,
.retry-count,
.retry-next {
  display: flex;
  gap: 0.75rem;
  margin-bottom: 0.65rem;
  font-size: 0.9rem;
}

.retry-error:last-child,
.retry-count:last-child,
.retry-next:last-child {
  margin-bottom: 0;
}

.retry-label {
  color: #fbbf24;
  font-weight: 600;
  min-width: 70px;
}

.retry-text {
  color: #e8eef7;
  font-family: 'JetBrains Mono', monospace;
  flex: 1;
  word-break: break-word;
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

.btn-icon {
  font-size: 1.35rem;
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

.spinner {
  width: 1.75rem;
  height: 1.75rem;
  border: 3px solid rgba(212, 175, 55, 0.15);
  border-top-color: #d4af37;
  border-radius: 50%;
  animation: vl-spin 0.8s linear infinite;
  box-shadow: 0 0 12px rgba(212, 175, 55, 0.3);
}

.drawer-result-block {
  margin-bottom: 2rem;
  animation: vl-fade-in-up 0.5s ease-out;
}

.drawer-result-block:last-child {
  margin-bottom: 0;
}

.result-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 1rem;
}

.result-actions {
  display: flex;
  gap: 0.5rem;
}

.icon-btn {
  background: linear-gradient(135deg, rgba(212, 175, 55, 0.1), rgba(41, 98, 255, 0.08));
  border: 1px solid rgba(212, 175, 55, 0.3);
  width: 2rem;
  height: 2rem;
  border-radius: 0.5rem;
  font-size: 1rem;
  cursor: pointer;
  transition: all 0.3s;
  display: flex;
  align-items: center;
  justify-content: center;
  backdrop-filter: blur(8px);
}

.icon-btn:hover {
  background: linear-gradient(135deg, rgba(212, 175, 55, 0.2), rgba(41, 98, 255, 0.12));
  border-color: rgba(212, 175, 55, 0.5);
  transform: translateY(-2px);
  box-shadow: 0 4px 12px rgba(212, 175, 55, 0.2);
}

.expand-btn {
  background: linear-gradient(135deg, rgba(15, 25, 45, 0.5), rgba(20, 30, 50, 0.4));
  border: 1px solid rgba(139, 149, 168, 0.25);
  padding: 0.65rem 1.25rem;
  border-radius: 0.65rem;
  color: #8b95a8;
  cursor: pointer;
  transition: all 0.3s;
  font-size: 0.875rem;
  font-weight: 500;
  margin-top: 1rem;
  width: 100%;
}

.expand-btn:hover {
  border-color: rgba(212, 175, 55, 0.4);
  color: #d4af37;
  background: linear-gradient(135deg, rgba(212, 175, 55, 0.1), rgba(41, 98, 255, 0.08));
}

.drawer-result-block h4 {
  font-size: 1.1rem;
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

/* 响应式 */
@media (max-width: 600px) {
  .task-drawer {
    width: 100%;
    max-width: 100vw;
  }
  .drawer-content {
    padding: 1.25rem;
  }
  .drawer-header {
    padding: 1.25rem;
  }
}
</style>
