<template>
  <div class="settings-page">
    <header class="settings-header">
      <div class="settings-header-text">
        <p class="settings-kicker">CONTROL SURFACE</p>
        <h1>设置</h1>
        <p class="settings-sub">外观、账号与 AI 模型服务</p>
      </div>
      <router-link :to="{ name: 'library' }" class="back-link">
        <VlIcon :name="ICON.arrowLeft" size="sm" />
        返回视频库
      </router-link>
    </header>

    <section class="settings-section" aria-labelledby="appearance-heading">
      <div class="section-head">
        <h2 id="appearance-heading">外观</h2>
        <p class="section-desc">界面主题仅保存在本机浏览器，切换后立即生效。</p>
      </div>
      <div class="theme-grid" role="listbox" aria-label="主题">
        <button
          v-for="opt in themeOptions"
          :key="opt.id"
          type="button"
          role="option"
          class="theme-card"
          :class="{ active: currentTheme === opt.id }"
          :aria-selected="currentTheme === opt.id"
          @click="pickTheme(opt.id)"
        >
          <span class="theme-card-swatch" :style="{ background: opt.swatch }" aria-hidden="true"></span>
          <span class="theme-card-meta">
            <span class="theme-card-label">{{ opt.label }}</span>
            <span class="theme-card-hint">{{ opt.hint }}</span>
          </span>
          <span v-if="currentTheme === opt.id" class="theme-card-check" aria-hidden="true">
            <VlIcon :name="ICON.check" size="sm" />
          </span>
        </button>
      </div>
    </section>

    <div v-if="!app?.user" class="settings-guest">
      <p>登录后可管理账号与模型配置。</p>
      <button type="button" class="btn-amber" @click="app?.openAuth?.()">登录 / 注册</button>
    </div>

    <template v-else>
      <section class="settings-section" aria-labelledby="account-heading">
        <div class="section-head">
          <h2 id="account-heading">账号</h2>
        </div>
        <div class="account-card">
          <div class="account-avatar" aria-hidden="true">
            {{ (app.user.nickname || app.user.username || 'U').slice(0, 1) }}
          </div>
          <div class="account-meta">
            <div class="account-name">{{ app.user.nickname || app.user.username }}</div>
            <div class="account-user">用户名 · {{ app.user.username }}</div>
            <div v-if="app.user.id" class="account-id">ID · {{ app.user.id }}</div>
          </div>
          <button type="button" class="btn-text-danger" @click="app.logout?.()">退出登录</button>
        </div>
      </section>

      <section class="settings-section" aria-labelledby="ai-heading">
        <div class="section-head">
          <h2 id="ai-heading">模型服务</h2>
          <p class="section-desc">
            使用 OpenAI 兼容接口配置对话、语音识别与向量模型。各段可使用不同中转与 Key；密钥仅保存在服务端。
          </p>
        </div>
        <div class="settings-panel">
          <AIProfileEditor @updated="onAIUpdated" />
        </div>
      </section>
    </template>
  </div>
</template>

<script setup>
import { computed, inject } from 'vue'
import AIProfileEditor from '../components/AIProfileEditor.vue'
import VlIcon from '../components/VlIcon.vue'
import { ICON } from '../icons.js'
import { THEME_OPTIONS, getStoredTheme } from '../theme.js'

const app = inject('appCtx', null)
const themeOptions = THEME_OPTIONS

const currentTheme = computed(() => app?.theme || getStoredTheme())

const pickTheme = (id) => {
  app?.setTheme?.(id)
}

const onAIUpdated = () => {
  // toast 已由 AIProfileEditor 处理；保留钩子便于后续扩展
}
</script>

<style scoped>
.settings-page {
  width: min(960px, 100%);
  margin: 0 auto;
  padding: calc(var(--vl-nav-h, 3.5rem) + 1.25rem) 1.5rem 2.5rem;
  box-sizing: border-box;
}

.settings-header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 1rem;
  margin-bottom: 1.75rem;
  flex-wrap: wrap;
}

.settings-kicker {
  margin: 0 0 0.4rem;
  font-family: var(--vl-font-mono);
  font-size: 0.68rem;
  letter-spacing: 0.2em;
  text-transform: uppercase;
  color: var(--vl-primary);
  opacity: 0.85;
}

.settings-header h1 {
  margin: 0 0 0.35rem;
  font-family: var(--vl-font-display, inherit);
  font-size: clamp(1.75rem, 3vw, 2.35rem);
  font-weight: 800;
  letter-spacing: -0.03em;
  line-height: 1.05;
  color: var(--vl-text);
}

.settings-sub {
  margin: 0;
  font-size: 0.9rem;
  color: var(--vl-text-muted);
}

.back-link {
  display: inline-flex;
  align-items: center;
  gap: 0.35rem;
  color: var(--vl-text-secondary);
  text-decoration: none;
  font-size: 0.88rem;
  padding: 0.4rem 0.65rem;
  border-radius: 0.5rem;
  border: 1px solid transparent;
  transition: color 0.15s, border-color 0.15s, background 0.15s;
}

.back-link:hover {
  color: var(--vl-primary);
  border-color: var(--vl-border-focus);
  background: var(--vl-primary-dim);
}

.settings-guest {
  text-align: center;
  padding: 3rem 1rem;
  border: 1px dashed var(--vl-border-strong);
  border-radius: var(--vl-radius-xl, 1rem);
  background: var(--vl-surface);
}

.settings-guest p {
  color: var(--vl-text-secondary);
  margin: 0 0 1.25rem;
}

.settings-section {
  margin-bottom: 2rem;
}

.section-head {
  margin-bottom: 0.9rem;
}

.section-head h2 {
  margin: 0 0 0.35rem;
  font-size: 1.05rem;
  font-weight: 650;
  color: var(--vl-text);
}

.section-desc {
  margin: 0;
  font-size: 0.82rem;
  line-height: 1.5;
  color: var(--vl-text-muted);
  max-width: 42rem;
}

.theme-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(10.5rem, 1fr));
  gap: 0.75rem;
}

.theme-card {
  display: flex;
  align-items: center;
  gap: 0.7rem;
  padding: 0.85rem 0.9rem;
  border-radius: var(--vl-radius-lg);
  border: 1px solid var(--vl-border-strong);
  background: var(--vl-panel);
  cursor: pointer;
  text-align: left;
  color: var(--vl-text);
  font-family: inherit;
  position: relative;
  transition: border-color 0.15s, box-shadow 0.15s, background 0.15s;
}

.theme-card:hover {
  border-color: var(--vl-border-focus);
  background: var(--vl-surface-hover);
}

.theme-card.active {
  border-color: var(--vl-border-focus);
  box-shadow: 0 0 0 1px var(--vl-primary-glow);
  background: var(--vl-primary-dim);
}

.theme-card-swatch {
  width: 2rem;
  height: 2rem;
  border-radius: 0.5rem;
  border: 1px solid var(--vl-border-strong);
  flex-shrink: 0;
  box-shadow: var(--vl-shadow-sm);
}

.theme-card-meta {
  display: flex;
  flex-direction: column;
  gap: 0.15rem;
  min-width: 0;
}

.theme-card-label {
  font-weight: 650;
  font-size: 0.92rem;
}

.theme-card-hint {
  font-size: 0.75rem;
  color: var(--vl-text-muted);
  line-height: 1.35;
}

.theme-card-check {
  margin-left: auto;
  color: var(--vl-primary);
  font-weight: 700;
  font-size: 0.95rem;
}

.account-card {
  display: flex;
  align-items: center;
  gap: 1rem;
  padding: 1.15rem 1.25rem;
  border-radius: var(--vl-radius-xl, 1rem);
  border: 1px solid var(--vl-border-strong);
  background: var(--vl-panel);
  flex-wrap: wrap;
}

.account-avatar {
  width: 2.75rem;
  height: 2.75rem;
  border-radius: 50%;
  display: grid;
  place-items: center;
  font-weight: 700;
  font-size: 1.05rem;
  color: var(--vl-text-inverse);
  background: linear-gradient(135deg, var(--vl-primary), var(--vl-info));
  flex-shrink: 0;
}

.account-meta {
  flex: 1;
  min-width: 10rem;
}

.account-name {
  font-weight: 600;
  color: var(--vl-text);
  font-size: 1rem;
}

.account-user,
.account-id {
  margin-top: 0.2rem;
  font-size: 0.8rem;
  color: var(--vl-text-muted);
  font-family: var(--vl-font-mono);
}

.btn-text-danger {
  appearance: none;
  border: 1px solid color-mix(in srgb, var(--vl-danger) 35%, transparent);
  background: var(--vl-danger-dim);
  color: var(--vl-danger);
  padding: 0.45rem 0.85rem;
  border-radius: 0.55rem;
  cursor: pointer;
  font-size: 0.86rem;
  font-weight: 600;
}

.btn-text-danger:hover {
  filter: brightness(1.05);
}

.settings-panel {
  padding: 1.25rem 1.25rem 1.35rem;
  border-radius: var(--vl-radius-xl, 1rem);
  border: 1px solid var(--vl-border-strong);
  background: var(--vl-panel);
}

.btn-amber {
  background: var(--vl-primary);
  color: var(--vl-text-inverse);
  border: none;
  padding: 0.7rem 1.5rem;
  border-radius: 0.75rem;
  font-weight: 600;
  cursor: pointer;
  font-size: 0.92rem;
}

.btn-amber:hover {
  box-shadow: 0 6px 20px var(--vl-primary-glow);
}

@media (max-width: 600px) {
  .settings-page {
    padding-left: 1rem;
    padding-right: 1rem;
  }

  .settings-panel {
    padding: 1rem;
  }
}
</style>
