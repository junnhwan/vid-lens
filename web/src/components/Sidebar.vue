<template>
  <aside class="sidebar" :class="{ open: mobileOpen }">
    <section class="sidebar-section">
      <div class="section-head">
        <h3 class="section-title">上传</h3>
        <span class="section-hint">本地 / 链接</span>
      </div>

      <div
        class="upload-card"
        :class="{ disabled: !user, uploading, dragging }"
        @click="handleLocalUploadClick"
        @dragover.prevent="dragging = true"
        @dragleave.prevent="dragging = false"
        @drop.prevent.stop="handleDrop"
      >
        <div class="upload-glyph" aria-hidden="true">
          <span class="upload-arrow">↑</span>
        </div>
        <div class="upload-copy">
          <p class="upload-label">本地上传</p>
          <p class="upload-sub">拖拽视频到此处，或点击选择</p>
        </div>
        <input type="file" accept="video/*" :disabled="!user" @change="handleFileSelect" hidden ref="fileInput" />
        <button type="button" class="upload-btn" @click.stop="triggerFileInput" :disabled="!user">
          {{ dragging ? '松手上传' : '选择文件' }}
        </button>
      </div>

      <div class="upload-card url-card" :class="{ disabled: !user, uploading }" @click="handleUrlCardClick">
        <div class="upload-glyph soft" aria-hidden="true">
          <span class="upload-link">◎</span>
        </div>
        <div class="upload-copy">
          <p class="upload-label">链接下载</p>
          <p class="upload-sub">B 站 / YouTube 等公开链接</p>
        </div>
        <div class="url-input-group" @click.stop>
          <input
            v-model="videoUrl"
            placeholder="https://..."
            @keyup.enter="handleUrlUpload"
            :disabled="!user || uploading"
          />
          <button type="button" class="upload-btn solid" @click="handleUrlUpload" :disabled="!user || uploading || !videoUrl">
            开始
          </button>
        </div>
      </div>

      <div v-if="uploading && uploadProgress >= 0" class="upload-progress">
        <div class="progress-bar">
          <div class="progress-fill" :style="{ width: uploadProgress + '%' }"></div>
        </div>
        <div class="progress-info">
          <div class="spinner small"></div>
          <span>{{ uploadMsg }} · {{ uploadProgress }}%</span>
        </div>
      </div>

      <div v-else-if="uploading" class="upload-status">
        <div class="spinner small"></div>
        <span>{{ uploadMsg }}</span>
      </div>
    </section>

    <section v-if="user" class="sidebar-section">
      <div class="section-head">
        <h3 class="section-title">概览</h3>
      </div>
      <div v-if="stats.loaded != null && stats.loaded < stats.total" class="stats-partial-hint">
        仅统计已加载 {{ stats.loaded }} / {{ stats.total }} 条
      </div>
      <div class="stats-grid">
        <div class="stat-card">
          <div class="stat-value">{{ stats.total }}</div>
          <div class="stat-label">全部</div>
        </div>
        <div class="stat-card ok">
          <div class="stat-value">{{ stats.completed }}</div>
          <div class="stat-label">完成</div>
        </div>
        <div class="stat-card run">
          <div class="stat-value">{{ stats.processing }}</div>
          <div class="stat-label">处理中</div>
        </div>
        <div class="stat-card fail">
          <div class="stat-value">{{ stats.failed }}</div>
          <div class="stat-label">失败</div>
        </div>
      </div>
    </section>
  </aside>

  <transition name="fade">
    <div v-if="mobileOpen" class="sidebar-overlay" @click="$emit('closeSidebar')"></div>
  </transition>
</template>

<script setup>
import { ref } from 'vue'

const props = defineProps({
  user: Object,
  uploading: Boolean,
  uploadMsg: String,
  uploadProgress: { type: Number, default: -1 },
  stats: Object,
  mobileOpen: Boolean
})

const emit = defineEmits(['uploadFile', 'uploadUrl', 'openAuth', 'closeSidebar', 'toast'])

const videoUrl = ref('')
const dragging = ref(false)
const fileInput = ref(null)

const handleLocalUploadClick = (e) => {
  if (!props.user) {
    e.preventDefault()
    e.stopPropagation()
    emit('openAuth')
  }
}

const handleUrlCardClick = () => {
  if (!props.user) emit('openAuth')
}

const triggerFileInput = () => {
  if (!props.user) {
    emit('openAuth')
    return
  }
  fileInput.value?.click()
}

const handleFileSelect = async (e) => {
  const file = e.target.files?.[0]
  if (!file) return
  emit('uploadFile', file)
  e.target.value = ''
}

const handleDrop = async (e) => {
  dragging.value = false
  if (!props.user) {
    emit('openAuth')
    return
  }
  const file = e.dataTransfer.files?.[0]
  if (!file) return
  if (!file.type.startsWith('video/')) {
    emit('toast', '仅支持视频文件')
    return
  }
  emit('uploadFile', file)
}

const handleUrlUpload = () => {
  if (!props.user) {
    emit('openAuth')
    return
  }
  if (!videoUrl.value) return
  if (!/^https?:\/\//i.test(videoUrl.value)) {
    emit('toast', '请输入 http(s) 开头的链接')
    return
  }
  emit('uploadUrl', videoUrl.value)
  videoUrl.value = ''
}
</script>

<style scoped>
.sidebar {
  width: 300px;
  flex-shrink: 0;
  padding: 1.35rem 1.15rem;
  background: rgba(8, 11, 18, 0.55);
  border-right: 1px solid var(--vl-border);
  overflow-y: auto;
}

.sidebar-section {
  margin-bottom: 1.75rem;
}

.sidebar-section:last-child {
  margin-bottom: 0;
}

.section-head {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  margin-bottom: 0.9rem;
}

.section-title {
  margin: 0;
  font-family: var(--vl-font-display);
  font-size: 0.95rem;
  font-weight: 700;
  letter-spacing: 0.04em;
  color: var(--vl-text);
}

.section-hint {
  font-size: 0.72rem;
  color: var(--vl-text-muted);
  font-family: var(--vl-font-mono);
  letter-spacing: 0.04em;
}

.stats-partial-hint {
  font-size: 0.72rem;
  color: var(--vl-text-muted);
  margin: -0.35rem 0 0.75rem;
}

.upload-card {
  display: flex;
  flex-direction: column;
  gap: 0.75rem;
  padding: 1rem;
  border-radius: var(--vl-radius-lg);
  border: 1px dashed var(--vl-border-strong);
  background: rgba(16, 22, 34, 0.55);
  cursor: pointer;
  transition: border-color 0.2s, background 0.2s, box-shadow 0.2s, transform 0.2s;
  margin-bottom: 0.75rem;
}

.upload-card:last-of-type {
  margin-bottom: 0;
}

.upload-card:hover:not(.disabled),
.upload-card.dragging:not(.disabled) {
  border-color: rgba(45, 212, 191, 0.55);
  border-style: solid;
  background: rgba(45, 212, 191, 0.06);
  box-shadow: 0 0 0 3px var(--vl-primary-dim);
}

.upload-card.disabled {
  opacity: 0.45;
  cursor: not-allowed;
}

.upload-card.uploading {
  pointer-events: none;
  opacity: 0.65;
}

.upload-glyph {
  width: 2.4rem;
  height: 2.4rem;
  border-radius: 0.65rem;
  display: grid;
  place-items: center;
  background: linear-gradient(145deg, rgba(45, 212, 191, 0.2), rgba(96, 165, 250, 0.1));
  border: 1px solid rgba(45, 212, 191, 0.3);
  color: var(--vl-primary);
  font-size: 1.1rem;
  font-weight: 700;
}

.upload-glyph.soft {
  background: rgba(255, 255, 255, 0.04);
  border-color: var(--vl-border);
  color: var(--vl-info);
}

.upload-copy {
  min-width: 0;
}

.upload-label {
  margin: 0 0 0.2rem;
  font-size: 0.92rem;
  font-weight: 600;
  color: var(--vl-text);
}

.upload-sub {
  margin: 0;
  font-size: 0.78rem;
  color: var(--vl-text-muted);
  line-height: 1.4;
}

.upload-btn {
  width: 100%;
  padding: 0.55rem 0.9rem;
  border-radius: var(--vl-radius-sm);
  border: 1px solid rgba(45, 212, 191, 0.35);
  background: var(--vl-primary-dim);
  color: var(--vl-primary);
  font-weight: 600;
  font-size: 0.84rem;
  cursor: pointer;
  transition: background 0.2s, border-color 0.2s, color 0.2s;
}

.upload-btn:hover:not(:disabled) {
  background: rgba(45, 212, 191, 0.22);
  border-color: var(--vl-primary);
}

.upload-btn.solid {
  background: linear-gradient(135deg, var(--vl-primary), #14b8a6);
  border-color: transparent;
  color: var(--vl-text-inverse);
}

.upload-btn:disabled {
  opacity: 0.4;
  cursor: not-allowed;
}

.url-input-group {
  display: flex;
  flex-direction: column;
  gap: 0.55rem;
}

.url-input-group input {
  width: 100%;
  box-sizing: border-box;
  background: rgba(7, 9, 15, 0.65);
  border: 1px solid var(--vl-border);
  border-radius: var(--vl-radius-sm);
  padding: 0.6rem 0.8rem;
  color: var(--vl-text);
  outline: none;
  font-size: 0.86rem;
  font-family: var(--vl-font-mono);
  transition: border-color 0.2s, box-shadow 0.2s;
}

.url-input-group input:focus {
  border-color: var(--vl-border-focus);
  box-shadow: 0 0 0 3px var(--vl-primary-dim);
}

.url-input-group input::placeholder {
  color: var(--vl-text-muted);
}

.upload-progress,
.upload-status {
  margin-top: 0.85rem;
  padding: 0.85rem;
  border-radius: var(--vl-radius);
  border: 1px solid rgba(45, 212, 191, 0.25);
  background: rgba(45, 212, 191, 0.06);
  color: var(--vl-primary);
  font-size: 0.82rem;
  font-weight: 500;
}

.progress-bar {
  height: 5px;
  background: rgba(148, 163, 184, 0.15);
  border-radius: 999px;
  overflow: hidden;
  margin-bottom: 0.65rem;
}

.progress-fill {
  height: 100%;
  background: linear-gradient(90deg, var(--vl-primary), #5eead4);
  border-radius: 999px;
  transition: width 0.3s ease;
  box-shadow: 0 0 8px var(--vl-primary-glow);
}

.progress-info,
.upload-status {
  display: flex;
  align-items: center;
  gap: 0.5rem;
}

.spinner {
  width: 1rem;
  height: 1rem;
  border: 2px solid rgba(45, 212, 191, 0.2);
  border-top-color: var(--vl-primary);
  border-radius: 50%;
  animation: vl-spin 0.75s linear infinite;
  flex-shrink: 0;
}

.stats-grid {
  display: grid;
  grid-template-columns: repeat(2, 1fr);
  gap: 0.55rem;
}

.stat-card {
  padding: 0.85rem 0.7rem;
  border-radius: var(--vl-radius);
  border: 1px solid var(--vl-border);
  background: rgba(16, 22, 34, 0.55);
  text-align: center;
  transition: border-color 0.2s, transform 0.2s;
}

.stat-card:hover {
  border-color: var(--vl-border-strong);
  transform: translateY(-1px);
}

.stat-value {
  font-family: var(--vl-font-mono);
  font-size: 1.45rem;
  font-weight: 600;
  color: var(--vl-text);
  line-height: 1.1;
  margin-bottom: 0.2rem;
}

.stat-card.ok .stat-value { color: var(--vl-success); }
.stat-card.run .stat-value { color: var(--vl-accent); }
.stat-card.fail .stat-value { color: var(--vl-danger); }

.stat-label {
  font-size: 0.72rem;
  color: var(--vl-text-muted);
  font-weight: 500;
  letter-spacing: 0.04em;
}

.sidebar-overlay {
  display: none;
}

@media (max-width: 900px) {
  .sidebar {
    position: fixed;
    top: 0;
    left: 0;
    height: 100vh;
    z-index: 150;
    transform: translateX(-100%);
    transition: transform 0.3s var(--vl-ease);
    box-shadow: var(--vl-shadow);
    border-right: 1px solid var(--vl-border-strong);
    background: rgba(7, 9, 15, 0.98);
  }

  .sidebar.open {
    transform: translateX(0);
  }

  .sidebar-overlay {
    display: block;
    position: fixed;
    inset: 0;
    background: rgba(0, 0, 0, 0.55);
    z-index: 149;
  }
}

.fade-enter-active, .fade-leave-active { transition: opacity 0.25s; }
.fade-enter-from, .fade-leave-to { opacity: 0; }
</style>
