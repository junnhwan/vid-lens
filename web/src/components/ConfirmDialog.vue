<template>
  <transition name="confirm">
    <div v-if="show" class="confirm-backdrop" @mousedown.self="handleBackdropMouseDown" role="dialog" aria-modal="true" :aria-label="title">
      <div class="confirm-panel" ref="panel">
        <div class="confirm-icon">{{ icon }}</div>
        <h3 class="confirm-title">{{ title }}</h3>
        <p v-if="message" class="confirm-message">{{ message }}</p>
        <div class="confirm-actions">
          <button v-if="showCancel" class="btn-cancel" @click="$emit('cancel')">取消</button>
          <button class="btn-confirm" :class="type" @click="$emit('confirm')">{{ confirmText }}</button>
        </div>
      </div>
    </div>
  </transition>
</template>

<script setup>
import { ref, onMounted, onUnmounted, nextTick } from 'vue'

const props = defineProps({
  show: Boolean,
  title: { type: String, default: '提示' },
  message: String,
  confirmText: { type: String, default: '确认' },
  showCancel: { type: Boolean, default: true },
  type: { type: String, default: 'danger' },   // danger | warning | primary
  icon: { type: String, default: '⚠️' },
})

defineEmits(['confirm', 'cancel'])

const panel = ref(null)

// ESC 关闭
const onKeyDown = (e) => {
  if (e.key === 'Escape' && props.show) {
    e.preventDefault()
  }
}
onMounted(() => document.addEventListener('keydown', onKeyDown))
onUnmounted(() => document.removeEventListener('keydown', onKeyDown))

// 点击背景关闭（拖拽安全）
const handleBackdropMouseDown = (e) => {
  const start = e.target
  const up = (ev) => {
    if (ev.target === start && start.classList.contains('confirm-backdrop')) {
      // 不自动关闭，让用户点"取消"
    }
    document.removeEventListener('mouseup', up)
  }
  document.addEventListener('mouseup', up)
}
</script>

<style scoped>
.confirm-backdrop {
  position: fixed;
  inset: 0;
  background: rgba(0, 0, 0, 0.7);
  backdrop-filter: blur(8px);
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 1.25rem;
  z-index: 1200;
  overflow-y: auto;
}

.confirm-panel {
  background: var(--vl-panel);
  border: 1px solid var(--vl-border-strong);
  border-radius: var(--vl-radius-xl);
  padding: 1.75rem 1.5rem 1.5rem;
  max-width: 400px;
  width: min(400px, 100%);
  margin: auto;
  text-align: center;
  box-shadow: var(--vl-shadow);
}

.confirm-icon {
  font-size: 1.75rem;
  margin-bottom: 0.65rem;
}

.confirm-title {
  margin: 0 0 0.5rem;
  font-size: 1.1rem;
  font-weight: 700;
  font-family: var(--vl-font-display);
  color: var(--vl-text);
}

.confirm-message {
  margin: 0 0 1.35rem;
  color: var(--vl-text-secondary);
  font-size: 0.9rem;
  line-height: 1.6;
  white-space: pre-wrap;
}

.confirm-actions {
  display: flex;
  gap: 0.65rem;
  justify-content: center;
}

.btn-cancel {
  background: transparent;
  border: 1px solid var(--vl-border);
  color: var(--vl-text-secondary);
  padding: 0.6rem 1.2rem;
  border-radius: var(--vl-radius-sm);
  font-weight: 600;
  cursor: pointer;
  transition: all 0.2s;
  font-size: 0.88rem;
}

.btn-cancel:hover {
  border-color: var(--vl-border-strong);
  color: var(--vl-text);
  background: rgba(255, 255, 255, 0.03);
}

.btn-confirm {
  border: none;
  padding: 0.6rem 1.2rem;
  border-radius: var(--vl-radius-sm);
  font-weight: 600;
  cursor: pointer;
  transition: transform 0.2s, box-shadow 0.2s;
  font-size: 0.88rem;
}

.btn-confirm.danger {
  background: linear-gradient(135deg, #ef4444, #dc2626);
  color: #fff;
}

.btn-confirm.danger:hover {
  box-shadow: 0 6px 18px rgba(239, 68, 68, 0.35);
  transform: translateY(-1px);
}

.btn-confirm.warning {
  background: linear-gradient(135deg, #f59e0b, #d97706);
  color: var(--vl-text-inverse);
}

.btn-confirm.warning:hover {
  box-shadow: 0 6px 18px rgba(245, 158, 11, 0.35);
  transform: translateY(-1px);
}

.btn-confirm.primary {
  background: linear-gradient(135deg, var(--vl-primary), #14b8a6);
  color: var(--vl-text-inverse);
}

.btn-confirm.primary:hover {
  box-shadow: 0 6px 18px var(--vl-primary-glow);
  transform: translateY(-1px);
}

.confirm-enter-active, .confirm-leave-active { transition: opacity 0.2s ease; }
.confirm-enter-active .confirm-panel, .confirm-leave-active .confirm-panel { transition: transform 0.2s var(--vl-ease); }
.confirm-enter-from, .confirm-leave-to { opacity: 0; }
.confirm-enter-from .confirm-panel { transform: scale(0.96); }
.confirm-leave-to .confirm-panel { transform: scale(0.96); }
</style>
