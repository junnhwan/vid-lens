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
  z-index: 2000;
}

.confirm-panel {
  background: linear-gradient(135deg, rgba(15, 25, 45, 0.97), rgba(20, 30, 50, 0.95));
  backdrop-filter: blur(32px) saturate(180%);
  border: 1px solid rgba(212, 175, 55, 0.3);
  border-radius: 1.5rem;
  padding: 2.5rem;
  max-width: 420px;
  width: 90%;
  text-align: center;
  box-shadow: 0 24px 64px rgba(0, 0, 0, 0.6);
}

.confirm-icon {
  font-size: 3rem;
  margin-bottom: 1rem;
  filter: drop-shadow(0 2px 8px rgba(212, 175, 55, 0.3));
}

.confirm-title {
  font-size: 1.25rem;
  font-weight: 700;
  color: #e8eef7;
  margin-bottom: 0.75rem;
}

.confirm-message {
  color: #8b95a8;
  font-size: 0.95rem;
  line-height: 1.7;
  margin-bottom: 2rem;
  white-space: pre-wrap;
}

.confirm-actions {
  display: flex;
  gap: 1rem;
  justify-content: center;
}

.btn-cancel {
  background: rgba(139, 149, 168, 0.1);
  border: 1px solid rgba(139, 149, 168, 0.3);
  color: #8b95a8;
  padding: 0.75rem 1.75rem;
  border-radius: 0.75rem;
  font-weight: 600;
  cursor: pointer;
  transition: all 0.3s;
  font-size: 0.95rem;
}

.btn-cancel:hover {
  border-color: rgba(139, 149, 168, 0.5);
  color: #e8eef7;
}

.btn-confirm {
  border: none;
  padding: 0.75rem 1.75rem;
  border-radius: 0.75rem;
  font-weight: 600;
  cursor: pointer;
  transition: all 0.3s;
  font-size: 0.95rem;
}

.btn-confirm.danger {
  background: linear-gradient(135deg, #ef4444, #dc2626);
  color: #fff;
}

.btn-confirm.danger:hover {
  box-shadow: 0 6px 24px rgba(239, 68, 68, 0.4);
  transform: translateY(-2px);
}

.btn-confirm.warning {
  background: linear-gradient(135deg, #f59e0b, #d97706);
  color: #0a0e1a;
}

.btn-confirm.warning:hover {
  box-shadow: 0 6px 24px rgba(245, 158, 11, 0.4);
  transform: translateY(-2px);
}

.btn-confirm.primary {
  background: linear-gradient(135deg, #d4af37, #f4e4a6);
  color: #0a0e1a;
}

.btn-confirm.primary:hover {
  box-shadow: 0 6px 24px rgba(212, 175, 55, 0.4);
  transform: translateY(-2px);
}

/* transition */
.confirm-enter-active, .confirm-leave-active { transition: opacity 0.25s ease; }
.confirm-enter-active .confirm-panel, .confirm-leave-active .confirm-panel { transition: transform 0.25s cubic-bezier(0.4, 0, 0.2, 1); }
.confirm-enter-from, .confirm-leave-to { opacity: 0; }
.confirm-enter-from .confirm-panel { transform: scale(0.9); }
.confirm-leave-to .confirm-panel { transform: scale(0.9); }
</style>
