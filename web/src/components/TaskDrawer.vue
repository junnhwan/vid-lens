<template>
  <transition name="drawer">
    <div v-if="task" class="drawer-backdrop" @click="$emit('close')">
      <div class="task-drawer" @click.stop>
        <div class="drawer-header">
          <h3>{{ task.filename }}</h3>
          <button class="drawer-close" @click="$emit('close')">×</button>
        </div>

        <div class="drawer-content">
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
              <span class="meta-status" :class="statusClass(task.status)">
                {{ statusText(task.status) }}
              </span>
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
              <h4>📝 文字提取</h4>
              <pre class="result-text">{{ task.transcription.content }}</pre>
            </div>

            <div v-if="task.summary?.content" class="drawer-result-block">
              <h4>🤖 AI 总结</h4>
              <div class="result-markdown" v-html="renderMarkdown(task.summary.content)"></div>
            </div>
          </template>
        </div>
      </div>
    </div>
  </transition>
</template>

<script setup>
import { computed } from 'vue'
import { marked } from 'marked'
import { isTaskActionDisabled } from '../taskActionPolicy.js'
import { taskFailureMessage } from '../taskDetailPolicy.js'

const props = defineProps({
  task: Object,
  loading: Boolean
})

defineEmits(['close', 'transcribe', 'analyze'])

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
const renderMarkdown = (content) => marked.parse(content || '')

const isActionDisabled = computed(() => isTaskActionDisabled(props.task, props.loading))
const failureMessage = computed(() => taskFailureMessage(props.task))
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
  display: inline-block;
  width: fit-content;
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
  animation: spin 0.8s linear infinite;
  box-shadow: 0 0 12px rgba(212, 175, 55, 0.3);
}

@keyframes spin {
  to { transform: rotate(360deg); }
}

.drawer-result-block {
  margin-bottom: 2rem;
  animation: resultFadeIn 0.5s ease-out;
}

@keyframes resultFadeIn {
  from { opacity: 0; transform: translateY(10px); }
  to { opacity: 1; transform: translateY(0); }
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
</style>
