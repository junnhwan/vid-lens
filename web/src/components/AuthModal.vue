<template>
  <transition name="modal">
    <div v-if="show" class="modal-backdrop" @mousedown.self="handleBackdropMouseDown">
      <div class="modal-panel">
        <button class="modal-close" @click="$emit('close')">×</button>
        <h2>{{ mode === 'login' ? '登录' : '注册' }}</h2>
        <div class="auth-form">
          <input v-model="form.username" placeholder="用户名" class="form-input" />
          <input v-model="form.password" type="password" placeholder="密码" class="form-input" />
          <input v-if="mode === 'register'" v-model="form.nickname" placeholder="昵称" class="form-input" />
          <button class="btn-amber full" @click="handleSubmit" :disabled="loading">
            {{ loading ? '处理中...' : (mode === 'login' ? '立即登录' : '提交注册') }}
          </button>
          <p class="auth-switch">
            {{ mode === 'login' ? '还没有账号？' : '已有账号？' }}
            <button class="link-btn" @click="$emit('switchMode')">
              {{ mode === 'login' ? '去注册' : '去登录' }}
            </button>
          </p>
          <p v-if="message" class="auth-msg" :class="{ error: isError }">{{ message }}</p>
        </div>
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
/* 登录弹窗 */
.modal-backdrop {
  position: fixed;
  inset: 0;
  background: rgba(0, 0, 0, 0.8);
  backdrop-filter: blur(12px);
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: 1000;
}
.modal-panel {
  width: 90%;
  max-width: 450px;
  backdrop-filter: blur(32px) saturate(180%);
  background: linear-gradient(135deg, rgba(15, 25, 45, 0.95), rgba(20, 30, 50, 0.9));
  border: 1px solid rgba(212, 175, 55, 0.3);
  border-radius: 1.75rem;
  padding: 2.75rem;
  position: relative;
  box-shadow: 0 24px 64px rgba(0, 0, 0, 0.6), 0 0 0 1px rgba(255, 255, 255, 0.05), inset 0 1px 0 rgba(255, 255, 255, 0.1);
}
.modal-panel::before {
  content: '';
  position: absolute;
  inset: 0;
  border-radius: 1.75rem;
  padding: 1px;
  background: linear-gradient(135deg, rgba(212, 175, 55, 0.5), rgba(41, 98, 255, 0.3));
  -webkit-mask: linear-gradient(#fff 0 0) content-box, linear-gradient(#fff 0 0);
  -webkit-mask-composite: xor;
  mask-composite: exclude;
  pointer-events: none;
  opacity: 0.6;
}
.modal-close {
  position: absolute;
  top: 1.25rem;
  right: 1.25rem;
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
  backdrop-filter: blur(8px);
}
.modal-close:hover {
  background: rgba(239, 68, 68, 0.2);
  border-color: #ef4444;
  transform: rotate(90deg) scale(1.1);
  box-shadow: 0 4px 16px rgba(239, 68, 68, 0.3);
}
.modal-panel h2 {
  font-size: 1.75rem;
  margin-bottom: 2rem;
  text-align: center;
  background: linear-gradient(135deg, #d4af37, #f4e4a6);
  -webkit-background-clip: text;
  -webkit-text-fill-color: transparent;
  background-clip: text;
  font-weight: 700;
  letter-spacing: 1px;
}
.auth-form {
  display: flex;
  flex-direction: column;
  gap: 1.25rem;
}
.form-input {
  background: rgba(10, 14, 26, 0.6);
  border: 1px solid rgba(139, 149, 168, 0.2);
  padding: 0.95rem 1.25rem;
  border-radius: 0.875rem;
  color: #e8eef7;
  outline: none;
  transition: all 0.3s;
  backdrop-filter: blur(8px);
  font-size: 0.95rem;
}
.form-input:focus {
  border-color: #d4af37;
  box-shadow: 0 0 0 3px rgba(212, 175, 55, 0.15), 0 2px 8px rgba(212, 175, 55, 0.2);
  background: rgba(10, 14, 26, 0.8);
}
.form-input::placeholder {
  color: #5a6477;
}
.auth-switch {
  text-align: center;
  font-size: 0.95rem;
  color: #8b95a8;
  margin-top: 0.5rem;
}
.link-btn {
  background: none;
  border: none;
  color: #d4af37;
  cursor: pointer;
  text-decoration: none;
  font-weight: 600;
  transition: all 0.3s;
  padding: 0 0.25rem;
  border-radius: 0.25rem;
}
.link-btn:hover {
  color: #f4e4a6;
  background: rgba(212, 175, 55, 0.1);
}
.auth-msg {
  text-align: center;
  font-size: 0.9rem;
  padding: 0.85rem;
  border-radius: 0.75rem;
  margin-top: 0.75rem;
  background: linear-gradient(135deg, rgba(34, 197, 94, 0.15), rgba(22, 163, 74, 0.1));
  color: #4ade80;
  border: 1px solid rgba(34, 197, 94, 0.3);
  backdrop-filter: blur(8px);
  font-weight: 500;
}
.auth-msg.error {
  background: linear-gradient(135deg, rgba(239, 68, 68, 0.15), rgba(220, 38, 38, 0.1));
  color: #f87171;
  border-color: rgba(239, 68, 68, 0.3);
}
.modal-enter-active, .modal-leave-active {
  transition: all 0.4s cubic-bezier(0.4, 0, 0.2, 1);
}
.modal-enter-from, .modal-leave-to {
  opacity: 0;
}
.modal-enter-from .modal-panel, .modal-leave-to .modal-panel {
  transform: scale(0.9) translateY(20px);
  opacity: 0;
}

.btn-amber {
  background: linear-gradient(135deg, #d4af37 0%, #f4e4a6 50%, #d4af37 100%);
  background-size: 200% auto;
  color: #0a0e1a;
  border: none;
  padding: 0.75rem 1.75rem;
  border-radius: 0.75rem;
  font-weight: 600;
  cursor: pointer;
  transition: all 0.4s cubic-bezier(0.4, 0, 0.2, 1);
  box-shadow: 0 4px 16px rgba(212, 175, 55, 0.3), inset 0 1px 0 rgba(255, 255, 255, 0.3);
  font-size: 0.95rem;
  letter-spacing: 0.5px;
  position: relative;
  overflow: hidden;
}
.btn-amber::before {
  content: '';
  position: absolute;
  inset: 0;
  background: linear-gradient(135deg, transparent, rgba(255, 255, 255, 0.2), transparent);
  transform: translateX(-100%);
  transition: transform 0.6s;
}
.btn-amber:hover::before {
  transform: translateX(100%);
}
.btn-amber:hover {
  transform: translateY(-2px);
  box-shadow: 0 6px 24px rgba(212, 175, 55, 0.5), inset 0 1px 0 rgba(255, 255, 255, 0.4);
  background-position: 200% center;
}
.btn-amber.full { width: 100%; }
</style>
