<template>
  <div
    class="task-card"
    :class="{
      'task-failed': task.status === 4 || task.status === 5,
      compact,
      selected,
    }"
    :aria-selected="selected"
    @click="$emit('click')"
  >
    <button class="task-delete" @click.stop="$emit('delete')" title="删除" aria-label="删除任务">×</button>

    <div class="task-header">
      <div class="task-icon" aria-hidden="true">
        <span class="film-mark"></span>
      </div>
      <div class="task-info">
        <div class="task-name">{{ task.title || task.filename }}</div>
        <div class="task-meta">
          <span class="meta-time">{{ formatTime(task.created_at) }}</span>
          <span class="meta-dot">·</span>
          <span class="meta-size">{{ formatFileSize(task.file_size) }}</span>
          <span class="meta-dot">·</span>
          <span class="meta-status" :class="detailedStatus.class">
            <span class="status-dot" aria-hidden="true"></span>
            {{ detailedStatus.text }}
          </span>
        </div>
      </div>
    </div>

    <div v-if="!compact" class="task-actions">
      <button class="action-btn" @click.stop="$emit('transcribe')" :disabled="isActionDisabled">
        提取文字
      </button>
      <button class="action-btn accent" @click.stop="$emit('analyze')" :disabled="isActionDisabled">
        AI 总结
      </button>
      <button v-if="canChat" class="action-btn chat" @click.stop="$emit('chat')">
        去对话
      </button>
    </div>
  </div>
</template>

<script setup>
import { computed } from 'vue'
import { isTaskActionDisabled } from '../taskActionPolicy.js'
import { formatTime, formatFileSize, getDetailedStatus } from '../utils/format.js'

const props = defineProps({
  task: Object,
  loading: Boolean,
  compact: { type: Boolean, default: false },
  selected: { type: Boolean, default: false },
})

defineEmits(['click', 'delete', 'transcribe', 'analyze', 'chat'])

const isActionDisabled = computed(() => isTaskActionDisabled(props.task, props.loading))
const detailedStatus = computed(() => getDetailedStatus(props.task))
const canChat = computed(() => props.task?.transcription?.content || props.task?.status === 3)
</script>

<style scoped>
.task-card {
  background: var(--vl-surface);
  border: 1px solid var(--vl-border);
  border-radius: var(--vl-radius-lg);
  padding: 1.15rem 1.25rem;
  cursor: pointer;
  transition: border-color 0.2s, box-shadow 0.2s, transform 0.2s, background 0.2s;
  position: relative;
  overflow: hidden;
}

.task-card::before {
  content: '';
  position: absolute;
  left: 0;
  top: 0;
  bottom: 0;
  width: 3px;
  background: linear-gradient(180deg, var(--vl-primary), transparent 85%);
  opacity: 0;
  transition: opacity 0.2s;
}

.task-card.task-failed::before {
  background: linear-gradient(180deg, var(--vl-danger), transparent 85%);
  opacity: 1;
}

.task-card:hover {
  border-color: rgba(45, 212, 191, 0.35);
  background: var(--vl-surface-hover);
  box-shadow: var(--vl-shadow-sm);
  transform: translateY(-2px);
}

.task-card:hover::before,
.task-card.selected::before {
  opacity: 1;
}

.task-card.selected {
  border-color: rgba(45, 212, 191, 0.5);
  background: rgba(45, 212, 191, 0.08);
  box-shadow: 0 0 0 1px rgba(45, 212, 191, 0.15), var(--vl-shadow-sm);
  transform: none;
}

.task-card.compact {
  padding: 0.9rem 1rem;
}

.task-card.compact .task-header {
  margin-bottom: 0;
}

.task-card.compact .task-icon {
  width: 2.2rem;
  height: 2.2rem;
}

.task-card.compact .task-name {
  font-size: 0.95rem;
  margin-bottom: 0.15rem;
}

.task-delete {
  position: absolute;
  top: 0.85rem;
  right: 0.85rem;
  width: 1.85rem;
  height: 1.85rem;
  border-radius: 50%;
  border: 1px solid transparent;
  background: transparent;
  color: var(--vl-text-muted);
  font-size: 1.15rem;
  line-height: 1;
  cursor: pointer;
  /* 触屏也要看得见；桌面 hover 再加强 */
  opacity: 0.55;
  transition: opacity 0.2s, color 0.2s, background 0.2s, border-color 0.2s;
  display: grid;
  place-items: center;
}

.task-card:hover .task-delete,
.task-delete:focus-visible {
  opacity: 1;
}

@media (hover: none) {
  .task-delete {
    opacity: 0.85;
  }
}

.task-delete:hover {
  color: var(--vl-danger);
  background: var(--vl-danger-dim);
  border-color: rgba(248, 113, 113, 0.35);
}

.task-header {
  display: flex;
  gap: 0.9rem;
  margin-bottom: 1rem;
  padding-right: 1.75rem;
}

.task-icon {
  width: 2.75rem;
  height: 2.75rem;
  border-radius: 0.7rem;
  flex-shrink: 0;
  display: grid;
  place-items: center;
  background: linear-gradient(145deg, rgba(45, 212, 191, 0.14), rgba(96, 165, 250, 0.08));
  border: 1px solid rgba(45, 212, 191, 0.22);
}

.film-mark {
  width: 0.9rem;
  height: 0.9rem;
  border-radius: 0.2rem;
  background: linear-gradient(135deg, var(--vl-primary), #38bdf8);
  box-shadow: 0 0 10px var(--vl-primary-glow);
  position: relative;
}

.film-mark::after {
  content: '';
  position: absolute;
  inset: 2px;
  border: 1px solid rgba(7, 9, 15, 0.45);
  border-radius: 0.1rem;
}

.task-info {
  flex: 1;
  min-width: 0;
}

.task-name {
  font-size: 1.02rem;
  font-weight: 600;
  color: var(--vl-text);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  margin-bottom: 0.35rem;
  letter-spacing: 0.01em;
}

.task-meta {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 0.4rem;
  font-size: 0.78rem;
  color: var(--vl-text-muted);
}

.meta-dot {
  opacity: 0.5;
}

.meta-time,
.meta-size {
  font-family: var(--vl-font-mono);
  font-size: 0.75rem;
}

.meta-status {
  display: inline-flex;
  align-items: center;
  gap: 0.3rem;
  padding: 0.15rem 0.5rem;
  border-radius: 999px;
  font-weight: 600;
  font-size: 0.72rem;
  letter-spacing: 0.02em;
  border: 1px solid transparent;
}

.status-dot {
  width: 0.4rem;
  height: 0.4rem;
  border-radius: 50%;
  background: currentColor;
}

.meta-status.pending {
  background: rgba(148, 163, 184, 0.12);
  color: var(--vl-text-secondary);
  border-color: rgba(148, 163, 184, 0.2);
}
.meta-status.queued {
  background: var(--vl-info-dim);
  color: var(--vl-info);
  border-color: rgba(96, 165, 250, 0.3);
}
.meta-status.running {
  background: var(--vl-accent-dim);
  color: var(--vl-accent);
  border-color: rgba(240, 180, 41, 0.3);
}
.meta-status.running .status-dot {
  animation: vl-status-pulse 1.6s ease-in-out infinite;
}
.meta-status.completed {
  background: var(--vl-success-dim);
  color: var(--vl-success);
  border-color: rgba(52, 211, 153, 0.3);
}
.meta-status.failed {
  background: var(--vl-danger-dim);
  color: var(--vl-danger);
  border-color: rgba(248, 113, 113, 0.3);
}
.meta-status.retrying {
  background: var(--vl-warning-dim);
  color: var(--vl-warning);
  border-color: rgba(251, 191, 36, 0.3);
}
.meta-status.dead {
  background: rgba(100, 116, 139, 0.15);
  color: #94a3b8;
  border-color: rgba(100, 116, 139, 0.3);
}

.task-actions {
  display: flex;
  gap: 0.55rem;
}

.action-btn {
  flex: 1;
  min-width: 0;
  padding: 0.6rem 0.7rem;
  border-radius: var(--vl-radius-sm);
  border: 1px solid var(--vl-border);
  background: rgba(7, 9, 15, 0.35);
  color: var(--vl-text-secondary);
  cursor: pointer;
  font-weight: 600;
  font-size: 0.82rem;
  transition: border-color 0.2s, color 0.2s, background 0.2s, transform 0.2s;
}

.action-btn:hover:not(:disabled) {
  border-color: rgba(45, 212, 191, 0.4);
  color: var(--vl-primary);
  background: var(--vl-primary-dim);
  transform: translateY(-1px);
}

.action-btn.accent {
  border-color: rgba(240, 180, 41, 0.35);
  color: var(--vl-accent);
  background: var(--vl-accent-dim);
}

.action-btn.accent:hover:not(:disabled) {
  border-color: rgba(240, 180, 41, 0.55);
  color: #fcd34d;
  background: rgba(240, 180, 41, 0.2);
}

.action-btn.chat {
  border-color: rgba(96, 165, 250, 0.35);
  color: var(--vl-info);
  background: var(--vl-info-dim);
}

.action-btn.chat:hover:not(:disabled) {
  border-color: rgba(96, 165, 250, 0.55);
  background: rgba(96, 165, 250, 0.2);
}

.action-btn:disabled {
  opacity: 0.35;
  cursor: not-allowed;
  transform: none;
}
</style>
