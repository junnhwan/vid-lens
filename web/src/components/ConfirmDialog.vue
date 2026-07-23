<template>
  <transition name="confirm">
    <div
      v-if="show"
      class="confirm-backdrop"
      @mousedown.self="handleBackdropMouseDown"
      role="dialog"
      aria-modal="true"
      :aria-label="title"
    >
      <div class="confirm-panel" ref="panel">
        <div class="confirm-icon" :class="type">
          <VlIcon :name="resolvedIcon" size="xl" :aria-hidden="true" />
        </div>
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
import { computed, ref, onMounted, onUnmounted } from 'vue'
import VlIcon from './VlIcon.vue'
import { ICON, resolveIconKey } from '../icons.js'

const props = defineProps({
  show: Boolean,
  title: { type: String, default: '提示' },
  message: String,
  confirmText: { type: String, default: '确认' },
  showCancel: { type: Boolean, default: true },
  type: { type: String, default: 'danger' }, // danger | warning | primary
  /** Lucide key or legacy symbol; rendered via VlIcon */
  icon: { type: String, default: ICON.alert },
})

const emit = defineEmits(['confirm', 'cancel'])

const panel = ref(null)
const resolvedIcon = computed(() => resolveIconKey(props.icon, ICON.alert))

// ESC → 取消（危险操作不直接确认）
const onKeyDown = (e) => {
  if (e.key === 'Escape' && props.show) {
    e.preventDefault()
    emit('cancel')
  }
}
onMounted(() => document.addEventListener('keydown', onKeyDown))
onUnmounted(() => document.removeEventListener('keydown', onKeyDown))

// 背景点击：有取消时等同取消；仅确认时不关
const handleBackdropMouseDown = (e) => {
  const start = e.target
  const up = (ev) => {
    if (ev.target === start && start.classList.contains('confirm-backdrop') && props.showCancel) {
      emit('cancel')
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
  background: var(--vl-overlay-scrim);
  backdrop-filter: blur(12px) saturate(140%);
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
  padding: 1.85rem 1.6rem 1.55rem;
  max-width: 400px;
  width: min(400px, 100%);
  margin: auto;
  text-align: center;
  box-shadow: var(--vl-shadow), 0 0 0 1px color-mix(in srgb, var(--vl-primary) 8%, transparent);
  position: relative;
  overflow: hidden;
}

.confirm-panel::before {
  content: '';
  position: absolute;
  inset: 0 0 auto 0;
  height: 2px;
  background: linear-gradient(90deg, transparent, var(--vl-primary), transparent);
  opacity: 0.7;
}

.confirm-icon {
  width: 3.25rem;
  height: 3.25rem;
  margin: 0 auto 0.85rem;
  border-radius: 1rem;
  display: grid;
  place-items: center;
  border: 1px solid var(--vl-border);
  background: var(--vl-white-a03);
  color: var(--vl-text-secondary);
}

.confirm-icon.danger {
  color: var(--vl-danger);
  background: var(--vl-danger-dim);
  border-color: color-mix(in srgb, var(--vl-danger) 35%, transparent);
}

.confirm-icon.warning {
  color: var(--vl-warning);
  background: var(--vl-warning-dim);
  border-color: color-mix(in srgb, var(--vl-warning) 35%, transparent);
}

.confirm-icon.primary {
  color: var(--vl-primary);
  background: var(--vl-primary-dim);
  border-color: color-mix(in srgb, var(--vl-primary) 35%, transparent);
}

.confirm-title {
  margin: 0 0 0.5rem;
  font-size: 1.15rem;
  font-weight: 700;
  font-family: var(--vl-font-display);
  letter-spacing: -0.02em;
  color: var(--vl-text);
}

.confirm-message {
  margin: 0 0 1.35rem;
  color: var(--vl-text-secondary);
  font-size: 0.9rem;
  line-height: 1.65;
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
  transition: all 0.2s var(--vl-ease);
  font-size: 0.88rem;
}

.btn-cancel:hover {
  border-color: var(--vl-border-strong);
  color: var(--vl-text);
  background: var(--vl-white-a03);
}

.btn-confirm {
  border: none;
  padding: 0.6rem 1.2rem;
  border-radius: var(--vl-radius-sm);
  font-weight: 600;
  cursor: pointer;
  transition: transform 0.2s var(--vl-ease), box-shadow 0.2s;
  font-size: 0.88rem;
}

.btn-confirm:active {
  transform: scale(0.98);
}

.btn-confirm.danger {
  background: linear-gradient(135deg, var(--vl-danger), color-mix(in srgb, var(--vl-danger) 80%, #000));
  color: var(--vl-text-inverse);
}

.btn-confirm.danger:hover {
  box-shadow: 0 6px 18px color-mix(in srgb, var(--vl-danger) 35%, transparent);
  transform: translateY(-1px);
}

.btn-confirm.warning {
  background: linear-gradient(135deg, var(--vl-warning), var(--vl-accent));
  color: var(--vl-text-inverse);
}

.btn-confirm.warning:hover {
  box-shadow: 0 6px 18px var(--vl-accent-glow);
  transform: translateY(-1px);
}

.btn-confirm.primary {
  background: linear-gradient(135deg, var(--vl-primary), var(--vl-primary-deep));
  color: var(--vl-text-inverse);
}

.btn-confirm.primary:hover {
  box-shadow: 0 6px 18px var(--vl-primary-glow);
  transform: translateY(-1px);
}

.confirm-enter-active,
.confirm-leave-active {
  transition: opacity 0.22s var(--vl-ease);
}
.confirm-enter-active .confirm-panel,
.confirm-leave-active .confirm-panel {
  transition: transform 0.28s cubic-bezier(0.16, 1, 0.3, 1);
}
.confirm-enter-from,
.confirm-leave-to {
  opacity: 0;
}
.confirm-enter-from .confirm-panel {
  transform: scale(0.94) translateY(8px);
}
.confirm-leave-to .confirm-panel {
  transform: scale(0.96) translateY(4px);
}
</style>
