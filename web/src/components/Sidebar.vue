<template>
  <aside class="sidebar" :class="{ open: mobileOpen }">
    <!-- 上传区 -->
    <section class="sidebar-section">
      <h3 class="section-title">📤 上传视频</h3>

      <div class="upload-card" :class="{ disabled: !user, uploading }"
           @click="handleLocalUploadClick"
           @dragover.prevent="dragging = true"
           @dragleave.prevent="dragging = false"
           @drop.prevent.stop="handleDrop">
        <div class="upload-icon">📁</div>
        <p class="upload-label">本地上传</p>
        <input type="file" accept="video/*" :disabled="!user" @change="handleFileSelect" hidden ref="fileInput" />
        <button class="upload-btn" @click="triggerFileInput" :disabled="!user">
          {{ dragging ? '松手上传' : '选择文件' }}
        </button>
      </div>

      <div class="upload-card" :class="{ disabled: !user, uploading }" @click="handleUrlCardClick">
        <div class="upload-icon">🌐</div>
        <p class="upload-label">链接下载</p>
        <div class="url-input-group">
          <input v-model="videoUrl" placeholder="粘贴链接..." @keyup.enter="handleUrlUpload" :disabled="!user || uploading" />
          <button class="upload-btn" @click="handleUrlUpload" :disabled="!user || uploading || !videoUrl">开始</button>
        </div>
      </div>

      <!-- 上传进度 -->
      <div v-if="uploading && uploadProgress >= 0" class="upload-progress">
        <div class="progress-bar">
          <div class="progress-fill" :style="{ width: uploadProgress + '%' }"></div>
        </div>
        <div class="progress-info">
          <div class="spinner small"></div>
          <span>{{ uploadMsg }} · {{ uploadProgress }}%</span>
        </div>
      </div>

      <!-- 上传状态（无进度时） -->
      <div v-else-if="uploading" class="upload-status">
        <div class="spinner small"></div>
        <span>{{ uploadMsg }}</span>
      </div>
    </section>

    <!-- 统计卡片 -->
    <section v-if="user" class="sidebar-section">
      <h3 class="section-title">📊 数据概览</h3>
      <div v-if="stats.loaded != null && stats.loaded < stats.total" class="stats-partial-hint">
        仅统计已加载 {{ stats.loaded }} / {{ stats.total }} 条
      </div>
      <div class="stats-grid">
        <div class="stat-card">
          <div class="stat-value">{{ stats.total }}</div>
          <div class="stat-label">总任务数</div>
        </div>
        <div class="stat-card">
          <div class="stat-value">{{ stats.completed }}</div>
          <div class="stat-label">已完成</div>
        </div>
        <div class="stat-card">
          <div class="stat-value">{{ stats.processing }}</div>
          <div class="stat-label">处理中</div>
        </div>
        <div class="stat-card">
          <div class="stat-value">{{ stats.failed }}</div>
          <div class="stat-label">失败</div>
        </div>
      </div>
    </section>
  </aside>

  <!-- 移动端遮罩 -->
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
  if (!file) {
    return
  }
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
  if (!videoUrl.value) {
    return
  }
  if (!/^https?:\/\//i.test(videoUrl.value)) {
    emit('toast', '请输入 http(s) 开头的链接')
    return
  }
  emit('uploadUrl', videoUrl.value)
  videoUrl.value = ''
}
</script>

<style scoped>
/* 侧边栏 */
.sidebar {
  width: 320px;
  flex-shrink: 0;
  padding: 2rem 1.5rem;
  backdrop-filter: blur(20px) saturate(180%);
  background: linear-gradient(135deg, rgba(10, 14, 26, 0.4), rgba(15, 25, 45, 0.3));
  border-right: 1px solid rgba(212, 175, 55, 0.15);
  overflow-y: auto;
  scrollbar-width: thin;
  scrollbar-color: rgba(212, 175, 55, 0.3) transparent;
}

.sidebar::-webkit-scrollbar {
  width: 6px;
}

.sidebar::-webkit-scrollbar-track {
  background: transparent;
}

.sidebar::-webkit-scrollbar-thumb {
  background: rgba(212, 175, 55, 0.3);
  border-radius: 3px;
}

.sidebar::-webkit-scrollbar-thumb:hover {
  background: rgba(212, 175, 55, 0.5);
}

.sidebar-section {
  margin-bottom: 2.5rem;
}

.sidebar-section:last-child {
  margin-bottom: 0;
}

.section-title {
  font-size: 1.1rem;
  font-weight: 700;
  background: linear-gradient(135deg, #d4af37, #f4e4a6);
  -webkit-background-clip: text;
  -webkit-text-fill-color: transparent;
  background-clip: text;
  margin-bottom: 1.25rem;
  letter-spacing: 0.5px;
}

.stats-partial-hint {
  font-size: 0.72rem;
  color: #8b95a8;
  margin-top: -0.5rem;
  margin-bottom: 1rem;
  opacity: 0.8;
}

/* 上传卡片 */
.upload-card {
  backdrop-filter: blur(20px) saturate(180%);
  background: linear-gradient(135deg, rgba(15, 25, 45, 0.5), rgba(20, 30, 50, 0.4));
  border: 1px solid rgba(212, 175, 55, 0.2);
  border-radius: 1rem;
  padding: 1.5rem;
  text-align: center;
  transition: all 0.3s cubic-bezier(0.4, 0, 0.2, 1);
  position: relative;
  overflow: hidden;
  cursor: pointer;
  box-shadow: 0 4px 16px rgba(0, 0, 0, 0.2), inset 0 1px 0 rgba(255, 255, 255, 0.03);
  margin-bottom: 1rem;
}

.upload-card:last-of-type {
  margin-bottom: 0;
}

.upload-card::before {
  content: '';
  position: absolute;
  top: -50%;
  left: -50%;
  width: 200%;
  height: 200%;
  background: radial-gradient(circle, rgba(212, 175, 55, 0.08) 0%, transparent 70%);
  opacity: 0;
  transition: opacity 0.4s, transform 0.6s;
  transform: scale(0.8);
}

.upload-card:hover:not(.disabled)::before {
  opacity: 1;
  transform: scale(1);
}

.upload-card:hover:not(.disabled) {
  transform: translateY(-4px);
  box-shadow: 0 8px 24px rgba(212, 175, 55, 0.2), 0 0 0 1px rgba(212, 175, 55, 0.4), inset 0 1px 0 rgba(255, 255, 255, 0.08);
  border-color: rgba(212, 175, 55, 0.4);
}

.upload-card.disabled {
  opacity: 0.4;
  cursor: not-allowed;
  filter: grayscale(0.5);
}

.upload-card.uploading {
  pointer-events: none;
  opacity: 0.6;
}

.upload-icon {
  font-size: 2.5rem;
  margin-bottom: 0.75rem;
  filter: drop-shadow(0 2px 8px rgba(212, 175, 55, 0.3));
  position: relative;
  z-index: 1;
}

.upload-label {
  font-size: 0.95rem;
  font-weight: 600;
  color: #d4af37;
  margin-bottom: 1rem;
  position: relative;
  z-index: 1;
}

.upload-btn {
  background: linear-gradient(135deg, rgba(212, 175, 55, 0.15), rgba(41, 98, 255, 0.1));
  border: 1px solid rgba(212, 175, 55, 0.3);
  color: #d4af37;
  padding: 0.6rem 1.5rem;
  border-radius: 0.65rem;
  cursor: pointer;
  transition: all 0.3s cubic-bezier(0.4, 0, 0.2, 1);
  font-weight: 600;
  font-size: 0.9rem;
  backdrop-filter: blur(8px);
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.2), inset 0 1px 0 rgba(255, 255, 255, 0.1);
  position: relative;
  z-index: 1;
  letter-spacing: 0.5px;
  width: 100%;
}

.upload-btn:hover:not(:disabled) {
  background: linear-gradient(135deg, rgba(212, 175, 55, 0.25), rgba(41, 98, 255, 0.15));
  border-color: #d4af37;
  box-shadow: 0 4px 12px rgba(212, 175, 55, 0.3), inset 0 1px 0 rgba(255, 255, 255, 0.15);
  transform: translateY(-2px);
}

.upload-btn:disabled {
  opacity: 0.4;
  cursor: not-allowed;
}

.url-input-group {
  display: flex;
  flex-direction: column;
  gap: 0.75rem;
  margin-top: 0.75rem;
  position: relative;
  z-index: 1;
}

.url-input-group input {
  width: 100%;
  box-sizing: border-box;
  background: rgba(10, 14, 26, 0.6);
  border: 1px solid rgba(139, 149, 168, 0.2);
  border-radius: 0.65rem;
  padding: 0.65rem 1rem;
  color: #e8eef7;
  outline: none;
  transition: all 0.3s;
  backdrop-filter: blur(8px);
  font-size: 0.9rem;
}

.url-input-group input:focus {
  border-color: #d4af37;
  box-shadow: 0 0 0 3px rgba(212, 175, 55, 0.15), 0 2px 8px rgba(212, 175, 55, 0.2);
}

.url-input-group input::placeholder {
  color: #5a6477;
}

/* 上传进度条 */
.upload-progress {
  margin-top: 1rem;
  backdrop-filter: blur(20px) saturate(180%);
  background: linear-gradient(135deg, rgba(15, 25, 45, 0.7), rgba(20, 30, 50, 0.5));
  border: 1px solid rgba(212, 175, 55, 0.3);
  border-radius: 0.75rem;
  padding: 1rem;
  box-shadow: 0 4px 16px rgba(0, 0, 0, 0.2), inset 0 1px 0 rgba(255, 255, 255, 0.05);
}

.progress-bar {
  height: 6px;
  background: rgba(139, 149, 168, 0.15);
  border-radius: 3px;
  overflow: hidden;
  margin-bottom: 0.75rem;
}

.progress-fill {
  height: 100%;
  background: linear-gradient(90deg, #d4af37, #f4e4a6);
  border-radius: 3px;
  transition: width 0.3s ease;
  box-shadow: 0 0 8px rgba(212, 175, 55, 0.4);
}

.progress-info {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  font-size: 0.85rem;
  font-weight: 500;
  color: #d4af37;
}

/* 上传状态（无进度） */
.upload-status {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 0.75rem;
  padding: 1rem;
  backdrop-filter: blur(20px) saturate(180%);
  background: linear-gradient(135deg, rgba(15, 25, 45, 0.7), rgba(20, 30, 50, 0.5));
  border: 1px solid rgba(212, 175, 55, 0.3);
  border-radius: 0.75rem;
  margin-top: 1rem;
  box-shadow: 0 4px 16px rgba(0, 0, 0, 0.2), inset 0 1px 0 rgba(255, 255, 255, 0.05);
  font-size: 0.9rem;
  font-weight: 500;
  color: #d4af37;
}

.spinner {
  width: 1.25rem;
  height: 1.25rem;
  border: 2.5px solid rgba(212, 175, 55, 0.15);
  border-top-color: #d4af37;
  border-radius: 50%;
  animation: vl-spin 0.8s linear infinite;
  box-shadow: 0 0 12px rgba(212, 175, 55, 0.3);
}

.spinner.small {
  width: 1.25rem;
  height: 1.25rem;
  border-width: 2.5px;
}

/* 统计网格 */
.stats-grid {
  display: grid;
  grid-template-columns: repeat(2, 1fr);
  gap: 0.75rem;
}

.stat-card {
  backdrop-filter: blur(20px) saturate(180%);
  background: linear-gradient(135deg, rgba(15, 25, 45, 0.5), rgba(20, 30, 50, 0.4));
  border: 1px solid rgba(139, 149, 168, 0.2);
  border-radius: 0.875rem;
  padding: 1rem;
  text-align: center;
  transition: all 0.3s cubic-bezier(0.4, 0, 0.2, 1);
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.1), inset 0 1px 0 rgba(255, 255, 255, 0.03);
}

.stat-card:hover {
  transform: translateY(-2px);
  border-color: rgba(212, 175, 55, 0.3);
  box-shadow: 0 4px 16px rgba(212, 175, 55, 0.15), inset 0 1px 0 rgba(255, 255, 255, 0.05);
}

.stat-value {
  font-size: 1.75rem;
  font-weight: 700;
  background: linear-gradient(135deg, #d4af37, #f4e4a6);
  -webkit-background-clip: text;
  -webkit-text-fill-color: transparent;
  background-clip: text;
  font-family: 'JetBrains Mono', monospace;
  margin-bottom: 0.25rem;
}

.stat-label {
  font-size: 0.8rem;
  color: #8b95a8;
  font-weight: 500;
}

/* 移动端遮罩 */
.sidebar-overlay {
  display: none;
}

/* 响应式 */
@media (max-width: 900px) {
  .sidebar {
    position: fixed;
    top: 0;
    left: 0;
    height: 100vh;
    z-index: 150;
    transform: translateX(-100%);
    transition: transform 0.35s cubic-bezier(0.4, 0, 0.2, 1);
    box-shadow: 4px 0 24px rgba(0, 0, 0, 0.4);
    border-right: 1px solid rgba(212, 175, 55, 0.3);
    background: linear-gradient(135deg, rgba(10, 14, 26, 0.97), rgba(15, 25, 45, 0.97));
  }

  .sidebar.open {
    transform: translateX(0);
  }

  .sidebar-overlay {
    display: block;
    position: fixed;
    inset: 0;
    background: rgba(0, 0, 0, 0.5);
    z-index: 149;
  }
}

.fade-enter-active, .fade-leave-active { transition: opacity 0.3s; }
.fade-enter-from, .fade-leave-to { opacity: 0; }
</style>
