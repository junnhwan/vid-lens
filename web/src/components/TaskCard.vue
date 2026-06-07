<template>
  <div class="task-card" @click="$emit('click')">
    <button class="task-delete" @click.stop="$emit('delete')" title="删除" aria-label="删除任务">×</button>

    <div class="task-header">
      <div class="task-icon">🎬</div>
      <div class="task-info">
        <div class="task-name">{{ task.filename }}</div>
        <div class="task-meta">
          <span class="meta-time">{{ formatTime(task.created_at) }}</span>
          <span class="meta-dot">·</span>
          <span class="meta-status" :class="detailedStatus.class">{{ detailedStatus.text }}</span>
        </div>
      </div>
    </div>

    <div class="task-actions">
      <button class="action-btn" @click.stop="$emit('transcribe')" :disabled="isActionDisabled">
        <span class="btn-icon">📄</span> 提取文字
      </button>
      <button class="action-btn amber" @click.stop="$emit('analyze')" :disabled="isActionDisabled">
        <span class="btn-icon">🤖</span> AI 总结
      </button>
    </div>
  </div>
</template>

<script setup>
import { computed } from 'vue'
import { isTaskActionDisabled } from '../taskActionPolicy.js'
import { formatTime, getDetailedStatus } from '../utils/format.js'

const props = defineProps({
  task: Object,
  loading: Boolean
})

defineEmits(['click', 'delete', 'transcribe', 'analyze'])

const isActionDisabled = computed(() => isTaskActionDisabled(props.task, props.loading))
const detailedStatus = computed(() => getDetailedStatus(props.task))
</script>

<style scoped>
.task-card {
  backdrop-filter: blur(20px) saturate(180%);
  background: linear-gradient(135deg, rgba(15, 25, 45, 0.6), rgba(20, 30, 50, 0.4));
  border: 1px solid rgba(139, 149, 168, 0.2);
  border-radius: 1.5rem;
  padding: 2rem;
  cursor: pointer;
  transition: all 0.4s cubic-bezier(0.4, 0, 0.2, 1);
  position: relative;
  overflow: hidden;
  box-shadow: 0 4px 16px rgba(0, 0, 0, 0.2), inset 0 1px 0 rgba(255, 255, 255, 0.03);
}
.task-card::before {
  content: '';
  position: absolute;
  top: 0;
  left: 0;
  width: 4px;
  height: 100%;
  background: linear-gradient(180deg, #d4af37, #2962ff);
  opacity: 0;
  transition: opacity 0.3s;
}
.task-card:hover::before {
  opacity: 1;
}
.task-card:hover {
  border-color: rgba(212, 175, 55, 0.4);
  box-shadow: 0 8px 32px rgba(212, 175, 55, 0.15), 0 0 0 1px rgba(212, 175, 55, 0.2), inset 0 1px 0 rgba(255, 255, 255, 0.05);
  transform: translateY(-4px);
}
.task-delete {
  position: absolute;
  top: 1.25rem;
  right: 1.25rem;
  background: rgba(239, 68, 68, 0.1);
  border: 1px solid rgba(239, 68, 68, 0.3);
  width: 2.25rem;
  height: 2.25rem;
  border-radius: 50%;
  color: #ef4444;
  font-size: 1.25rem;
  cursor: pointer;
  opacity: 0;
  transition: all 0.3s cubic-bezier(0.4, 0, 0.2, 1);
  display: flex;
  align-items: center;
  justify-content: center;
  backdrop-filter: blur(8px);
}
.task-card:hover .task-delete {
  opacity: 1;
}
.task-delete:hover {
  background: rgba(239, 68, 68, 0.2);
  border-color: #ef4444;
  transform: rotate(90deg) scale(1.1);
  box-shadow: 0 2px 8px rgba(239, 68, 68, 0.3);
}
.task-header {
  display: flex;
  gap: 1.25rem;
  margin-bottom: 1.5rem;
}
.task-icon {
  width: 3.5rem;
  height: 3.5rem;
  background: linear-gradient(135deg, rgba(212, 175, 55, 0.12), rgba(41, 98, 255, 0.08));
  border: 1px solid rgba(212, 175, 55, 0.3);
  border-radius: 1rem;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 1.75rem;
  flex-shrink: 0;
  box-shadow: 0 4px 12px rgba(212, 175, 55, 0.15), inset 0 1px 0 rgba(255, 255, 255, 0.1);
  transition: all 0.3s;
}
.task-card:hover .task-icon {
  transform: scale(1.05);
  box-shadow: 0 6px 16px rgba(212, 175, 55, 0.25), inset 0 1px 0 rgba(255, 255, 255, 0.15);
}
.task-info {
  flex: 1;
  min-width: 0;
}
.task-name {
  font-size: 1.15rem;
  font-weight: 600;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  color: #e8eef7;
  letter-spacing: 0.3px;
  margin-bottom: 0.5rem;
}
.task-meta {
  display: flex;
  gap: 0.75rem;
  font-size: 0.875rem;
  color: #8b95a8;
  margin-top: 0.5rem;
  align-items: center;
}
.meta-dot {
  opacity: 0.4;
  font-size: 0.6rem;
}
.meta-time {
  font-family: 'JetBrains Mono', monospace;
  font-size: 0.85rem;
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

/* 任务操作按钮 */
.task-actions {
  display: flex;
  gap: 1rem;
}

.action-btn {
  flex: 1;
  background: linear-gradient(135deg, rgba(15, 25, 45, 0.5), rgba(20, 30, 50, 0.4));
  border: 1px solid rgba(139, 149, 168, 0.25);
  padding: 0.9rem 1.5rem;
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
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.1), inset 0 1px 0 rgba(255, 255, 255, 0.03);
  letter-spacing: 0.3px;
}

.action-btn:hover:not(:disabled) {
  border-color: rgba(212, 175, 55, 0.5);
  color: #d4af37;
  background: linear-gradient(135deg, rgba(212, 175, 55, 0.1), rgba(41, 98, 255, 0.08));
  transform: translateY(-2px);
  box-shadow: 0 4px 16px rgba(212, 175, 55, 0.2), inset 0 1px 0 rgba(255, 255, 255, 0.08);
}

.action-btn.amber {
  border-color: rgba(212, 175, 55, 0.35);
  color: #d4af37;
  background: linear-gradient(135deg, rgba(212, 175, 55, 0.12), rgba(41, 98, 255, 0.08));
  box-shadow: 0 2px 12px rgba(212, 175, 55, 0.15), inset 0 1px 0 rgba(255, 255, 255, 0.05);
}

.action-btn.amber:hover:not(:disabled) {
  border-color: rgba(212, 175, 55, 0.6);
  background: linear-gradient(135deg, rgba(212, 175, 55, 0.2), rgba(41, 98, 255, 0.12));
  box-shadow: 0 4px 20px rgba(212, 175, 55, 0.3), inset 0 1px 0 rgba(255, 255, 255, 0.1);
}

.action-btn:disabled {
  opacity: 0.35;
  cursor: not-allowed;
  filter: grayscale(0.5);
}

.btn-icon {
  font-size: 1.35rem;
  position: relative;
  z-index: 1;
}
</style>
