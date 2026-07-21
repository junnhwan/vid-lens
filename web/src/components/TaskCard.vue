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
      <div class="action-stack">
        <button
          class="action-btn"
          :class="{ done: hasTx }"
          @click.stop="$emit('transcribe')"
          :disabled="txPrimaryDisabled"
          :title="hasTx ? '文字已提取，可在详情查看' : '提取视频文字'"
        >
          {{ txLabel }}
        </button>
        <button
          v-if="hasTx && !busy"
          type="button"
          class="rerun-link"
          @click.stop="$emit('retranscribe')"
          title="重新调用 ASR，覆盖已有文字"
        >
          重新提取
        </button>
      </div>
      <div class="action-stack">
        <button
          class="action-btn accent"
          :class="{ done: hasSum }"
          @click.stop="$emit('analyze')"
          :disabled="sumPrimaryDisabled"
          :title="hasSum ? '总结已生成，可在详情查看' : '生成 AI 总结'"
        >
          {{ sumLabel }}
        </button>
        <button
          v-if="hasSum && !busy"
          type="button"
          class="rerun-link"
          @click.stop="$emit('reanalyze')"
          title="重新调用模型，覆盖已有总结"
        >
          重新总结
        </button>
      </div>
      <button v-if="canChat" class="action-btn chat" @click.stop="$emit('chat')">
        去对话
      </button>
    </div>
  </div>
</template>

<script setup>
import { computed } from 'vue'
import {
  hasSummaryResult,
  hasTranscriptionResult,
  isPrimaryAnalyzeDisabled,
  isPrimaryTranscribeDisabled,
  isTaskActionDisabled,
  primaryAnalyzeLabel,
  primaryTranscribeLabel,
} from '../taskActionPolicy.js'
import { formatTime, formatFileSize, getDetailedStatus } from '../utils/format.js'

const props = defineProps({
  task: Object,
  loading: Boolean,
  compact: { type: Boolean, default: false },
  selected: { type: Boolean, default: false },
})

defineEmits(['click', 'delete', 'transcribe', 'analyze', 'retranscribe', 'reanalyze', 'chat'])

const busy = computed(() => isTaskActionDisabled(props.task, props.loading))
const hasTx = computed(() => hasTranscriptionResult(props.task))
const hasSum = computed(() => hasSummaryResult(props.task))
const txPrimaryDisabled = computed(() => isPrimaryTranscribeDisabled(props.task, props.loading))
const sumPrimaryDisabled = computed(() => isPrimaryAnalyzeDisabled(props.task, props.loading))
const txLabel = computed(() => primaryTranscribeLabel(props.task))
const sumLabel = computed(() => primaryAnalyzeLabel(props.task))
const detailedStatus = computed(() => getDetailedStatus(props.task))
const canChat = computed(() => hasTranscriptionResult(props.task) || props.task?.status === 3)
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
  border-color: var(--vl-primary-glow);
  background: var(--vl-surface-hover);
  box-shadow: var(--vl-shadow-sm);
  transform: translateY(-2px);
}

.task-card:hover::before,
.task-card.selected::before {
  opacity: 1;
}

.task-card.selected {
  border-color: var(--vl-border-focus);
  background: var(--vl-primary-dim);
  box-shadow: 0 0 0 1px color-mix(in srgb, var(--vl-primary) 15%, transparent), var(--vl-shadow-sm);
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
  border-color: color-mix(in srgb, var(--vl-danger) 35%, transparent);
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
  background: linear-gradient(145deg, var(--vl-primary-dim), var(--vl-info-dim));
  border: 1px solid color-mix(in srgb, var(--vl-primary) 22%, transparent);
}

.film-mark {
  width: 0.9rem;
  height: 0.9rem;
  border-radius: 0.2rem;
  background: linear-gradient(135deg, var(--vl-primary), var(--vl-info));
  box-shadow: 0 0 10px var(--vl-primary-glow);
  position: relative;
}

.film-mark::after {
  content: '';
  position: absolute;
  inset: 2px;
  border: 1px solid color-mix(in srgb, var(--vl-bg) 45%, transparent);
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
  background: var(--vl-border);
  color: var(--vl-text-secondary);
  border-color: var(--vl-border-strong);
}
.meta-status.queued {
  background: var(--vl-info-dim);
  color: var(--vl-info);
  border-color: color-mix(in srgb, var(--vl-info) 30%, transparent);
}
.meta-status.running {
  background: var(--vl-accent-dim);
  color: var(--vl-accent);
  border-color: color-mix(in srgb, var(--vl-accent) 30%, transparent);
}
.meta-status.running .status-dot {
  animation: vl-status-pulse 1.6s ease-in-out infinite;
}
.meta-status.completed {
  background: var(--vl-success-dim);
  color: var(--vl-success);
  border-color: color-mix(in srgb, var(--vl-success) 30%, transparent);
}
.meta-status.failed {
  background: var(--vl-danger-dim);
  color: var(--vl-danger);
  border-color: color-mix(in srgb, var(--vl-danger) 30%, transparent);
}
.meta-status.retrying {
  background: var(--vl-warning-dim);
  color: var(--vl-warning);
  border-color: color-mix(in srgb, var(--vl-warning) 30%, transparent);
}
.meta-status.dead {
  background: color-mix(in srgb, var(--vl-text-muted) 15%, transparent);
  color: var(--vl-text-secondary);
  border-color: color-mix(in srgb, var(--vl-text-muted) 30%, transparent);
}

.task-actions {
  display: flex;
  gap: 0.55rem;
  align-items: flex-start;
}

.action-stack {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  align-items: stretch;
  gap: 0.25rem;
}

.action-btn {
  width: 100%;
  min-width: 0;
  padding: 0.6rem 0.7rem;
  border-radius: var(--vl-radius-sm);
  border: 1px solid var(--vl-border);
  background: color-mix(in srgb, var(--vl-bg) 35%, transparent);
  color: var(--vl-text-secondary);
  cursor: pointer;
  font-weight: 600;
  font-size: 0.82rem;
  transition: border-color 0.2s, color 0.2s, background 0.2s, transform 0.2s, opacity 0.2s;
}

.action-btn:hover:not(:disabled) {
  border-color: var(--vl-border-focus);
  color: var(--vl-primary);
  background: var(--vl-primary-dim);
  transform: translateY(-1px);
}

.action-btn.accent {
  border-color: color-mix(in srgb, var(--vl-accent) 35%, transparent);
  color: var(--vl-accent);
  background: var(--vl-accent-dim);
}

.action-btn.accent:hover:not(:disabled) {
  border-color: color-mix(in srgb, var(--vl-accent) 55%, transparent);
  color: var(--vl-accent);
  background: var(--vl-accent-glow);
}

.action-btn.chat {
  flex: 1;
  align-self: stretch;
  border-color: color-mix(in srgb, var(--vl-info) 35%, transparent);
  color: var(--vl-info);
  background: var(--vl-info-dim);
}

.action-btn.chat:hover:not(:disabled) {
  border-color: color-mix(in srgb, var(--vl-info) 55%, transparent);
  background: color-mix(in srgb, var(--vl-info) 20%, transparent);
}

.action-btn:disabled {
  opacity: 0.35;
  cursor: not-allowed;
  transform: none;
}

.action-btn.done:disabled {
  opacity: 0.55;
  color: var(--vl-text-muted);
  border-color: var(--vl-border);
  background: color-mix(in srgb, var(--vl-bg) 50%, transparent);
  cursor: default;
}

.rerun-link {
  appearance: none;
  border: none;
  background: transparent;
  color: var(--vl-text-muted);
  font-size: 0.72rem;
  font-weight: 500;
  padding: 0.1rem 0.15rem;
  cursor: pointer;
  text-align: center;
  text-decoration: underline;
  text-underline-offset: 2px;
  opacity: 0.75;
  transition: color 0.15s, opacity 0.15s;
}

.rerun-link:hover {
  color: var(--vl-text-secondary);
  opacity: 1;
}
</style>
