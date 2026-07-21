<template>
  <transition name="modal">
    <div v-if="show" class="modal-backdrop" @mousedown.self="handleBackdropMouseDown">
      <div class="modal-panel">
        <button class="modal-close" @click="$emit('close')">×</button>
        <h2>{{ mode === 'login' ? '登录' : '注册' }}</h2>
        <form class="auth-form" @submit.prevent="handleSubmit">
          <input
            v-model="form.username"
            name="username"
            autocomplete="username"
            placeholder="用户名"
            class="form-input"
            :disabled="loading"
          />
          <input
            v-model="form.password"
            name="password"
            type="password"
            autocomplete="current-password"
            placeholder="密码"
            class="form-input"
            :disabled="loading"
          />
          <input
            v-if="mode === 'register'"
            v-model="form.nickname"
            name="nickname"
            autocomplete="nickname"
            placeholder="昵称"
            class="form-input"
            :disabled="loading"
          />
          <button type="submit" class="btn-amber full" :disabled="loading">
            {{ loading ? '处理中...' : (mode === 'login' ? '立即登录' : '提交注册') }}
          </button>
          <p class="auth-switch">
            {{ mode === 'login' ? '还没有账号？' : '已有账号？' }}
            <button type="button" class="link-btn" @click="$emit('switchMode')">
              {{ mode === 'login' ? '去注册' : '去登录' }}
            </button>
          </p>
          <p v-if="message" class="auth-msg" :class="{ error: isError }">{{ message }}</p>
        </form>
      </div>
    </div>
  </transition>
</template>

<script setup>
import { ref, watch } from 'vue'

const props = defineProps({
  show: Boolean,
  mode: String,
  loading: Boolean,
  message: String,
  isError: Boolean
})

const emit = defineEmits(['close', 'switchMode', 'submit'])

const form = ref({ username: '', password: '', nickname: '' })

watch(() => props.show, (newVal) => {
  if (newVal) {
    form.value = { username: '', password: '', nickname: '' }
  }
})

const handleSubmit = () => {
  if (!form.value.username || !form.value.password) {
    return
  }
  emit('submit', { ...form.value })
}

const handleBackdropMouseDown = (e) => {
  const startTarget = e.target
  const handleMouseUp = (upEvent) => {
    if (upEvent.target === startTarget && startTarget.classList.contains('modal-backdrop')) {
      emit('close')
    }
    document.removeEventListener('mouseup', handleMouseUp)
  }
  document.addEventListener('mouseup', handleMouseUp)
}
</script>

<style scoped>
.modal-backdrop {
  position: fixed;
  inset: 0;
  background: var(--vl-overlay-scrim);
  backdrop-filter: blur(10px);
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 1.25rem;
  z-index: 1100;
  overflow-y: auto;
}

.modal-panel {
  width: min(420px, 100%);
  margin: auto;
  background: var(--vl-panel);
  border: 1px solid var(--vl-border-strong);
  border-radius: var(--vl-radius-xl);
  padding: 2rem 1.75rem 1.75rem;
  position: relative;
  box-shadow: var(--vl-shadow);
}

.modal-close {
  position: absolute;
  top: 0.9rem;
  right: 0.9rem;
  width: 2rem;
  height: 2rem;
  border-radius: 50%;
  border: 1px solid var(--vl-border);
  background: transparent;
  color: var(--vl-text-muted);
  font-size: 1.25rem;
  cursor: pointer;
  display: grid;
  place-items: center;
  transition: color 0.2s, border-color 0.2s, background 0.2s;
}

.modal-close:hover {
  color: var(--vl-danger);
  border-color: color-mix(in srgb, var(--vl-danger) 40%, transparent);
  background: var(--vl-danger-dim);
}

.modal-panel h2 {
  margin: 0 0 1.35rem;
  text-align: center;
  font-family: var(--vl-font-display);
  font-size: 1.35rem;
  font-weight: 700;
  color: var(--vl-text);
  letter-spacing: 0.03em;
}

.auth-form {
  display: flex;
  flex-direction: column;
  gap: 0.85rem;
}

.form-input {
  width: 100%;
  background: var(--vl-surface);
  border: 1px solid var(--vl-border);
  padding: 0.75rem 0.95rem;
  border-radius: var(--vl-radius-sm);
  color: var(--vl-text);
  outline: none;
  transition: border-color 0.2s, box-shadow 0.2s;
  font-size: 0.92rem;
}

.form-input:focus {
  border-color: var(--vl-border-focus);
  box-shadow: 0 0 0 3px var(--vl-primary-dim);
}

.form-input::placeholder {
  color: var(--vl-text-muted);
}

.auth-switch {
  text-align: center;
  font-size: 0.86rem;
  color: var(--vl-text-muted);
  margin: 0.25rem 0 0;
}

.link-btn {
  background: none;
  border: none;
  color: var(--vl-primary);
  cursor: pointer;
  font-weight: 600;
  padding: 0 0.2rem;
}

.link-btn:hover {
  text-decoration: underline;
}

.auth-msg {
  text-align: center;
  font-size: 0.84rem;
  padding: 0.65rem 0.75rem;
  border-radius: var(--vl-radius-sm);
  margin: 0.15rem 0 0;
  background: var(--vl-success-dim);
  color: var(--vl-success);
  border: 1px solid color-mix(in srgb, var(--vl-success) 30%, transparent);
  font-weight: 500;
}

.auth-msg.error {
  background: var(--vl-danger-dim);
  color: var(--vl-danger);
  border-color: color-mix(in srgb, var(--vl-danger) 30%, transparent);
}

.modal-enter-active,
.modal-leave-active {
  transition: opacity 0.25s var(--vl-ease);
}
.modal-enter-active .modal-panel,
.modal-leave-active .modal-panel {
  transition: transform 0.25s var(--vl-ease), opacity 0.25s;
}
.modal-enter-from,
.modal-leave-to {
  opacity: 0;
}
.modal-enter-from .modal-panel,
.modal-leave-to .modal-panel {
  transform: scale(0.96) translateY(8px);
  opacity: 0;
}
</style>
