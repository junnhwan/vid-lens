<template>
  <nav class="navbar" role="navigation" aria-label="主导航">
    <div class="nav-container">
      <router-link :to="{ name: 'library' }" class="brand">
        <span class="mirror-mark" aria-hidden="true">
          <span class="mirror-core"></span>
        </span>
        <span class="brand-text">
          映知
        </span>
      </router-link>

      <nav v-if="user" class="nav-links" aria-label="页面切换">
        <router-link :to="{ name: 'library' }" class="nav-link">视频库</router-link>
        <router-link :to="chatLink" class="nav-link" :class="{ 'is-chat-active': isChatRoute }">对话</router-link>
      </nav>

      <!-- only useful on library (upload sidebar); chat has its own layout -->
      <button
        v-if="user && showUploadMenu"
        class="mobile-menu-btn"
        @click="$emit('toggleSidebar')"
        aria-label="切换上传侧栏"
      >
        <span class="menu-bars" aria-hidden="true"></span>
      </button>

      <div class="nav-right">
        <div class="theme-wrap" ref="themeWrapRef">
          <button
            type="button"
            class="btn-ghost theme-btn"
            :aria-expanded="themeOpen"
            aria-haspopup="listbox"
            aria-label="切换主题"
            title="切换主题"
            @click="themeOpen = !themeOpen"
          >
            <span class="theme-icon" aria-hidden="true">◐</span>
            <span class="btn-label">{{ currentThemeLabel }}</span>
          </button>
          <div
            v-if="themeOpen"
            class="theme-menu"
            role="listbox"
            aria-label="主题列表"
          >
            <button
              v-for="opt in themeOptions"
              :key="opt.id"
              type="button"
              role="option"
              class="theme-option"
              :class="{ active: currentTheme === opt.id }"
              :aria-selected="currentTheme === opt.id"
              @click="pickTheme(opt.id)"
            >
              <span class="theme-swatch" :style="{ background: opt.swatch }" aria-hidden="true"></span>
              <span class="theme-option-text">
                <span class="theme-option-label">{{ opt.label }}</span>
                <span class="theme-option-hint">{{ opt.hint }}</span>
              </span>
            </button>
          </div>
        </div>
        <template v-if="user">
          <button class="btn-ghost" @click="$emit('openConfig')" title="设置：账号与模型" aria-label="设置">
            <span class="gear" aria-hidden="true">⚙</span>
            <span class="btn-label">设置</span>
          </button>
          <div class="user-badge">
            <span class="user-avatar">{{ user.nickname?.[0] || user.username?.[0] || 'U' }}</span>
            <span class="user-name">{{ user.nickname || user.username }}</span>
          </div>
          <button class="btn-text" @click="$emit('logout')">退出</button>
        </template>
        <button v-else class="btn-amber" @click="$emit('openAuth')">登录 / 注册</button>
      </div>
    </div>
  </nav>
</template>

<script setup>
import { computed, inject, onMounted, onUnmounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import { readLastChatTaskId } from '../chatSelectionPolicy.js'
import { THEME_OPTIONS, getStoredTheme } from '../theme.js'

defineProps({
  user: Object
})

defineEmits(['logout', 'openAuth', 'openConfig', 'toggleSidebar'])

const app = inject('appCtx', null)
const route = useRoute()
const themeOpen = ref(false)
const themeWrapRef = ref(null)
const themeOptions = THEME_OPTIONS

const showUploadMenu = computed(() => route.name === 'library')
const isChatRoute = computed(() => route.name === 'chat' || route.name === 'chat-task')

const currentTheme = computed(() => app?.theme || getStoredTheme())
const currentThemeLabel = computed(() => {
  const hit = THEME_OPTIONS.find((o) => o.id === currentTheme.value)
  return hit?.label || '主题'
})

const pickTheme = (id) => {
  if (app?.setTheme) app.setTheme(id)
  themeOpen.value = false
}

const onDocClick = (e) => {
  if (!themeOpen.value) return
  const el = themeWrapRef.value
  if (el && !el.contains(e.target)) themeOpen.value = false
}

const onKeydown = (e) => {
  if (e.key === 'Escape' && themeOpen.value) themeOpen.value = false
}

onMounted(() => {
  document.addEventListener('click', onDocClick)
  document.addEventListener('keydown', onKeydown)
})
onUnmounted(() => {
  document.removeEventListener('click', onDocClick)
  document.removeEventListener('keydown', onKeydown)
})

// 顶栏「对话」优先回到上次视频，避免总是落到 bare /chat → 第一个视频
const chatLink = computed(() => {
  // 已在某个对话里时，点「对话」保持当前任务（别跳回 bare）
  if (route.name === 'chat-task' && route.params.taskId != null && route.params.taskId !== '') {
    return { name: 'chat-task', params: { taskId: route.params.taskId } }
  }
  const lastId = readLastChatTaskId()
  if (lastId != null) {
    return { name: 'chat-task', params: { taskId: lastId } }
  }
  return { name: 'chat' }
})
</script>

<style scoped>
.navbar {
  height: var(--vl-nav-h);
  backdrop-filter: blur(18px) saturate(160%);
  background: var(--vl-nav-bg);
  border-bottom: 1px solid var(--vl-border);
  position: sticky;
  top: 0;
  z-index: 100;
}

.nav-container {
  max-width: 1440px;
  margin: 0 auto;
  padding: 0 1.5rem;
  height: 100%;
  display: flex;
  align-items: center;
  gap: 1rem;
}

.brand {
  display: flex;
  align-items: center;
  gap: 0.75rem;
  text-decoration: none;
  color: inherit;
  flex-shrink: 0;
}

.mirror-mark {
  width: 2rem;
  height: 2rem;
  border-radius: 0.55rem;
  display: grid;
  place-items: center;
  background: linear-gradient(145deg, var(--vl-primary-dim), var(--vl-info-dim));
  border: 1px solid var(--vl-border-focus);
  box-shadow: 0 0 20px var(--vl-primary-glow);
}

.mirror-core {
  width: 0.7rem;
  height: 0.7rem;
  border-radius: 50%;
  background: radial-gradient(circle at 30% 30%, var(--vl-primary), var(--vl-primary-deep) 70%);
  box-shadow: 0 0 10px var(--vl-primary-glow);
}

.brand-text {
  font-family: var(--vl-font-display);
  font-weight: 700;
  font-size: 1.2rem;
  letter-spacing: 0.04em;
  color: var(--vl-text);
  display: flex;
  align-items: baseline;
  gap: 0.45rem;
}

.brand-text .en {
  font-family: var(--vl-font-mono);
  font-size: 0.68rem;
  font-weight: 500;
  letter-spacing: 0.12em;
  text-transform: uppercase;
  color: var(--vl-primary);
  opacity: 0.9;
}

.nav-links {
  display: flex;
  align-items: center;
  gap: 0.25rem;
  margin-left: 0.5rem;
  padding: 0.2rem;
  background: var(--vl-white-a03);
  border: 1px solid var(--vl-border);
  border-radius: 999px;
}

.nav-link {
  padding: 0.4rem 0.95rem;
  border-radius: 999px;
  font-size: 0.86rem;
  font-weight: 500;
  color: var(--vl-text-secondary);
  text-decoration: none;
  transition: color 0.2s, background 0.2s;
}

.nav-link:hover {
  color: var(--vl-text);
}

.nav-link.router-link-active,
.nav-link.is-chat-active {
  color: var(--vl-text-inverse);
  background: var(--vl-primary);
  font-weight: 600;
}

.mobile-menu-btn {
  display: none;
  margin-left: auto;
  width: 2.4rem;
  height: 2.4rem;
  border-radius: var(--vl-radius-sm);
  border: 1px solid var(--vl-border);
  background: var(--vl-surface);
  cursor: pointer;
  place-items: center;
}

.menu-bars {
  width: 1rem;
  height: 0.7rem;
  display: block;
  background:
    linear-gradient(var(--vl-primary), var(--vl-primary)) 0 0 / 100% 2px no-repeat,
    linear-gradient(var(--vl-primary), var(--vl-primary)) 0 50% / 100% 2px no-repeat,
    linear-gradient(var(--vl-primary), var(--vl-primary)) 0 100% / 70% 2px no-repeat;
}

.nav-right {
  margin-left: auto;
  display: flex;
  align-items: center;
  gap: 0.65rem;
}

.theme-wrap {
  position: relative;
}

.theme-btn .theme-icon {
  font-size: 1rem;
  line-height: 1;
  opacity: 0.9;
}

.theme-menu {
  position: absolute;
  top: calc(100% + 0.45rem);
  right: 0;
  min-width: 12.5rem;
  padding: 0.35rem;
  border-radius: var(--vl-radius);
  border: 1px solid var(--vl-border-strong);
  background: var(--vl-panel);
  box-shadow: var(--vl-shadow);
  z-index: 120;
  display: flex;
  flex-direction: column;
  gap: 0.15rem;
}

.theme-option {
  display: flex;
  align-items: center;
  gap: 0.65rem;
  width: 100%;
  text-align: left;
  padding: 0.5rem 0.55rem;
  border: none;
  border-radius: var(--vl-radius-sm);
  background: transparent;
  color: var(--vl-text);
  cursor: pointer;
  font-family: inherit;
  transition: background 0.15s;
}

.theme-option:hover {
  background: var(--vl-surface-hover);
}

.theme-option.active {
  background: var(--vl-primary-dim);
  box-shadow: inset 0 0 0 1px var(--vl-border-focus);
}

.theme-swatch {
  width: 1.15rem;
  height: 1.15rem;
  border-radius: 0.35rem;
  border: 1px solid var(--vl-border-strong);
  flex-shrink: 0;
}

.theme-option-text {
  display: flex;
  flex-direction: column;
  gap: 0.05rem;
  min-width: 0;
}

.theme-option-label {
  font-size: 0.86rem;
  font-weight: 600;
}

.theme-option-hint {
  font-size: 0.72rem;
  color: var(--vl-text-muted);
}

.btn-ghost {
  display: inline-flex;
  align-items: center;
  gap: 0.35rem;
  padding: 0.45rem 0.75rem;
  border-radius: var(--vl-radius-sm);
  border: 1px solid var(--vl-border);
  background: transparent;
  color: var(--vl-text-secondary);
  cursor: pointer;
  font-size: 0.85rem;
  font-weight: 500;
  transition: border-color 0.2s, color 0.2s, background 0.2s;
}

.btn-ghost:hover {
  border-color: var(--vl-border-focus);
  color: var(--vl-primary);
  background: var(--vl-primary-dim);
}

.gear {
  font-size: 1rem;
  line-height: 1;
}

.user-badge {
  display: flex;
  align-items: center;
  gap: 0.55rem;
  padding: 0.25rem 0.7rem 0.25rem 0.25rem;
  border-radius: 999px;
  border: 1px solid var(--vl-border);
  background: var(--vl-white-a03);
}

.user-avatar {
  width: 1.85rem;
  height: 1.85rem;
  border-radius: 50%;
  display: grid;
  place-items: center;
  font-weight: 700;
  font-size: 0.8rem;
  color: var(--vl-text-inverse);
  background: linear-gradient(135deg, var(--vl-primary), var(--vl-info));
}

.user-name {
  font-size: 0.86rem;
  font-weight: 500;
  color: var(--vl-text);
  max-width: 8rem;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.btn-text {
  background: none;
  border: none;
  color: var(--vl-text-muted);
  cursor: pointer;
  font-size: 0.85rem;
  font-weight: 500;
  padding: 0.4rem 0.55rem;
  border-radius: var(--vl-radius-sm);
  transition: color 0.2s, background 0.2s;
}

.btn-text:hover {
  color: var(--vl-danger);
  background: var(--vl-danger-dim);
}

@media (max-width: 900px) {
  .mobile-menu-btn {
    display: grid;
    margin-left: 0.35rem;
  }
  /* keep nav reachable on small screens — previously hidden, chat became unreachable */
  .nav-links {
    display: flex;
    margin-left: 0.25rem;
    padding: 0.15rem;
    gap: 0.15rem;
  }
  .nav-link {
    padding: 0.35rem 0.65rem;
    font-size: 0.8rem;
  }
  .user-name,
  .btn-label {
    display: none;
  }
  .nav-container {
    padding: 0 1rem;
    gap: 0.5rem;
  }
  .nav-right {
    gap: 0.4rem;
  }
}

@media (max-width: 600px) {
  .brand-text {
    font-size: 1.05rem;
  }
  .brand-text .en {
    display: none;
  }
}
</style>
