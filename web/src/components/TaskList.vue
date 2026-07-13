<template>
  <!-- 骨架屏加载状态 -->
  <section v-if="showInitialSkeleton" class="tasks-section">
    <div class="section-header">
      <h2>我的任务</h2>
    </div>
    <div class="tasks-list">
      <div v-for="i in 3" :key="i" class="skeleton-card">
        <div class="skeleton-header">
          <div class="skeleton-icon"></div>
          <div class="skeleton-info">
            <div class="skeleton-title"></div>
            <div class="skeleton-meta"></div>
          </div>
        </div>
        <div class="skeleton-actions">
          <div class="skeleton-btn"></div>
          <div class="skeleton-btn"></div>
        </div>
      </div>
    </div>
  </section>

  <section v-else-if="loadError" class="empty-state">
    <div class="empty-icon" aria-hidden="true">!</div>
    <h3>任务列表加载失败</h3>
    <p>{{ loadError }}</p>
    <button class="load-more-btn" @click="$emit('retry')">重新加载</button>
  </section>

  <section v-else-if="tasks.length" class="tasks-section">
    <div class="section-header">
      <h2>我的任务</h2>
      <div class="filter-tabs">
        <button v-for="tab in tabs" :key="tab.key"
                :class="['tab', { active: activeTab === tab.key }]"
                @click="activeTab = tab.key">
          {{ tab.label }} <span class="tab-count">{{ tab.count }}</span>
        </button>
      </div>
      <input v-model="searchQuery" class="search-box" placeholder="搜索文件名…  Ctrl+K" aria-label="搜索任务" />
      <button class="view-toggle" @click="toggleView" :title="viewMode === 'grid' ? '切换到列表视图' : '切换到网格视图'">
        {{ viewMode === 'grid' ? '≡' : '▦' }}
      </button>
    </div>

    <TransitionGroup name="task-list" tag="div" class="tasks-list" :class="viewMode">
      <TaskCard
        v-for="t in filteredTasks"
        :key="t.id"
        :task="t"
        :loading="loading[t.id]"
        :compact="viewMode === 'list'"
        @click="$emit('taskClick', t)"
        @delete="$emit('deleteTask', t)"
        @transcribe="$emit('transcribe', t)"
        @analyze="$emit('analyze', t)"
        @chat="$emit('chat', t)"
      />
    </TransitionGroup>

    <!-- 搜索无结果 -->
    <div v-if="tasks.length && !filteredTasks.length" class="empty-search">
      <div class="empty-search-icon">⌀</div>
      <p>没有找到匹配「{{ searchQuery }}」的任务</p>
    </div>

    <!-- 加载更多 -->
    <div v-if="hasMore" class="load-more">
      <button class="load-more-btn" @click="$emit('loadMore')" :disabled="loadingMore">
        {{ loadingMore ? '加载中...' : '加载更多' }}
      </button>
    </div>
  </section>

  <div v-else class="empty-state">
    <div class="empty-icon" aria-hidden="true">◇</div>
    <h3>还没有任务</h3>
    <p>从左侧上传视频，或粘贴 B 站 / YouTube 链接开始分析</p>
  </div>

  <!-- 回到顶部按钮 -->
  <transition name="scroll-top">
    <button v-if="showScrollTop" class="scroll-top-btn" @click="scrollToTop" aria-label="回到顶部">
      ↑
    </button>
  </transition>
</template>

<script setup>
import { ref, computed, onMounted, onUnmounted, watch } from 'vue'
import TaskCard from './TaskCard.vue'
import { shouldShowInitialTaskSkeleton } from '../taskListLoadingPolicy.js'

const props = defineProps({
  tasks: Array,
  loading: Object,
  initialLoading: { type: Boolean, default: false },
  hasMore: { type: Boolean, default: false },
  loadingMore: { type: Boolean, default: false },
  loadError: { type: String, default: '' }
})

const emit = defineEmits(['taskClick', 'deleteTask', 'transcribe', 'analyze', 'loadMore', 'chat', 'retry', 'search'])

const activeTab = ref('all')
const searchQuery = ref('')
const showScrollTop = ref(false)
const viewMode = ref(localStorage.getItem('taskViewMode') || 'grid')

const showInitialSkeleton = computed(() =>
  shouldShowInitialTaskSkeleton(props.tasks, props.initialLoading),
)

const toggleView = () => {
  viewMode.value = viewMode.value === 'grid' ? 'list' : 'grid'
  localStorage.setItem('taskViewMode', viewMode.value)
}

const tabs = computed(() => [
  { key: 'all', label: '全部', count: props.tasks.length },
  { key: 'processing', label: '处理中', count: props.tasks.filter(t => t.status < 3).length },
  { key: 'completed', label: '已完成', count: props.tasks.filter(t => t.status === 3).length },
  { key: 'failed', label: '失败', count: props.tasks.filter(t => t.status === 4 || t.status === 5).length }
])

const filteredTasks = computed(() => {
  // 关键字搜索交给后端（emit search），这里只做状态 tab 的客户端过滤
  let result = props.tasks
  if (activeTab.value === 'processing') result = result.filter(t => t.status < 3)
  if (activeTab.value === 'completed') result = result.filter(t => t.status === 3)
  if (activeTab.value === 'failed') result = result.filter(t => t.status === 4 || t.status === 5)
  return result
})

let searchTimer = null
watch(searchQuery, (val) => {
  clearTimeout(searchTimer)
  searchTimer = setTimeout(() => emit('search', val), 300)
})

const handleScroll = () => {
  const contentArea = document.querySelector('.content-area')
  if (contentArea) {
    showScrollTop.value = contentArea.scrollTop > 400
  }
}

const scrollToTop = () => {
  const contentArea = document.querySelector('.content-area')
  if (contentArea) {
    contentArea.scrollTo({ top: 0, behavior: 'smooth' })
  }
}

onMounted(() => {
  const contentArea = document.querySelector('.content-area')
  if (contentArea) {
    contentArea.addEventListener('scroll', handleScroll)
  }
})

onUnmounted(() => {
  const contentArea = document.querySelector('.content-area')
  if (contentArea) {
    contentArea.removeEventListener('scroll', handleScroll)
  }
})
</script>

<style scoped>
.tasks-section {
  animation: vl-fade-in-up 0.45s var(--vl-ease);
}

.section-header {
  display: flex;
  align-items: center;
  gap: 0.85rem;
  margin-bottom: 1.35rem;
  flex-wrap: wrap;
  padding-bottom: 1rem;
  border-bottom: 1px solid var(--vl-border);
}

.section-header h2 {
  margin: 0;
  font-family: var(--vl-font-display);
  font-size: 1.35rem;
  font-weight: 700;
  letter-spacing: 0.02em;
  color: var(--vl-text);
}

.filter-tabs {
  display: flex;
  gap: 0.35rem;
  flex: 1;
  flex-wrap: wrap;
}

.tab {
  background: transparent;
  border: 1px solid transparent;
  padding: 0.4rem 0.7rem;
  border-radius: 999px;
  color: var(--vl-text-muted);
  cursor: pointer;
  transition: all 0.2s var(--vl-ease);
  display: inline-flex;
  align-items: center;
  gap: 0.4rem;
  font-weight: 500;
  font-size: 0.82rem;
}

.tab:hover {
  color: var(--vl-text);
  background: rgba(255, 255, 255, 0.04);
}

.tab.active {
  color: var(--vl-primary);
  background: var(--vl-primary-dim);
  border-color: rgba(45, 212, 191, 0.3);
}

.tab-count {
  background: rgba(148, 163, 184, 0.14);
  padding: 0.1rem 0.4rem;
  border-radius: 999px;
  font-size: 0.7rem;
  font-weight: 600;
  font-family: var(--vl-font-mono);
  color: var(--vl-text-secondary);
}

.tab.active .tab-count {
  background: rgba(45, 212, 191, 0.2);
  color: var(--vl-primary);
}

.search-box {
  padding: 0.55rem 0.85rem;
  background: rgba(7, 9, 15, 0.45);
  border: 1px solid var(--vl-border);
  border-radius: var(--vl-radius-sm);
  color: var(--vl-text);
  outline: none;
  min-width: 200px;
  transition: border-color 0.2s, box-shadow 0.2s;
  font-size: 0.86rem;
}

.search-box:focus {
  border-color: var(--vl-border-focus);
  box-shadow: 0 0 0 3px var(--vl-primary-dim);
}

.search-box::placeholder {
  color: var(--vl-text-muted);
}

.view-toggle {
  background: transparent;
  border: 1px solid var(--vl-border);
  width: 2.25rem;
  height: 2.25rem;
  border-radius: var(--vl-radius-sm);
  color: var(--vl-text-muted);
  cursor: pointer;
  transition: all 0.2s;
  font-size: 1rem;
  display: grid;
  place-items: center;
}

.view-toggle:hover {
  border-color: rgba(45, 212, 191, 0.4);
  color: var(--vl-primary);
  background: var(--vl-primary-dim);
}

.tasks-list {
  display: flex;
  flex-direction: column;
  gap: 0.85rem;
}

.tasks-list.list {
  gap: 0.65rem;
}

.task-list-enter-active {
  transition: all 0.3s var(--vl-ease);
}
.task-list-leave-active {
  transition: all 0.25s var(--vl-ease);
}
.task-list-enter-from {
  opacity: 0;
  transform: translateY(10px);
}
.task-list-leave-to {
  opacity: 0;
  transform: translateX(-12px);
}
.task-list-move {
  transition: transform 0.3s var(--vl-ease);
}

.empty-search {
  text-align: center;
  padding: 2.5rem 1rem;
}

.empty-search-icon {
  font-size: 1.75rem;
  margin-bottom: 0.65rem;
  opacity: 0.7;
}

.empty-search p {
  color: var(--vl-text-muted);
  font-size: 0.9rem;
  margin: 0;
}

.load-more {
  text-align: center;
  margin-top: 1.5rem;
  padding-top: 1.15rem;
  border-top: 1px solid var(--vl-border);
}

.load-more-btn {
  background: transparent;
  border: 1px solid var(--vl-border);
  padding: 0.6rem 1.4rem;
  border-radius: var(--vl-radius-sm);
  color: var(--vl-text-secondary);
  cursor: pointer;
  transition: all 0.2s;
  font-weight: 600;
  font-size: 0.86rem;
}

.load-more-btn:hover:not(:disabled) {
  border-color: rgba(45, 212, 191, 0.4);
  color: var(--vl-primary);
  background: var(--vl-primary-dim);
}

.load-more-btn:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

.empty-state {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  min-height: 360px;
  text-align: center;
  padding: 2rem 1rem;
}

.empty-icon {
  width: 3.5rem;
  height: 3.5rem;
  margin-bottom: 1rem;
  border-radius: 1rem;
  display: grid;
  place-items: center;
  font-size: 1.5rem;
  background: var(--vl-primary-dim);
  border: 1px solid rgba(45, 212, 191, 0.25);
}

.empty-state h3 {
  margin: 0 0 0.4rem;
  font-family: var(--vl-font-display);
  font-size: 1.25rem;
  color: var(--vl-text);
}

.empty-state p {
  margin: 0;
  color: var(--vl-text-muted);
  font-size: 0.9rem;
  max-width: 22rem;
  line-height: 1.5;
}

.skeleton-card {
  background: var(--vl-surface);
  border: 1px solid var(--vl-border);
  border-radius: var(--vl-radius-lg);
  padding: 1.15rem 1.25rem;
  animation: vl-skeleton-pulse 1.4s ease-in-out infinite;
}

.skeleton-header {
  display: flex;
  gap: 0.9rem;
  margin-bottom: 1rem;
}

.skeleton-icon {
  width: 2.75rem;
  height: 2.75rem;
  background: rgba(148, 163, 184, 0.14);
  border-radius: 0.7rem;
  flex-shrink: 0;
}

.skeleton-info {
  flex: 1;
  display: flex;
  flex-direction: column;
  gap: 0.55rem;
  justify-content: center;
}

.skeleton-title {
  height: 0.95rem;
  background: rgba(148, 163, 184, 0.16);
  border-radius: 0.35rem;
  width: 55%;
}

.skeleton-meta {
  height: 0.7rem;
  background: rgba(148, 163, 184, 0.1);
  border-radius: 0.35rem;
  width: 35%;
}

.skeleton-actions {
  display: flex;
  gap: 0.55rem;
}

.skeleton-btn {
  flex: 1;
  height: 2.15rem;
  background: rgba(148, 163, 184, 0.1);
  border-radius: var(--vl-radius-sm);
}

.scroll-top-btn {
  position: fixed;
  bottom: 1.5rem;
  right: 1.5rem;
  width: 2.6rem;
  height: 2.6rem;
  background: linear-gradient(135deg, var(--vl-primary), #14b8a6);
  border: none;
  border-radius: 50%;
  font-size: 1rem;
  cursor: pointer;
  transition: transform 0.2s, box-shadow 0.2s;
  box-shadow: 0 6px 18px var(--vl-primary-glow);
  z-index: 100;
  display: grid;
  place-items: center;
  color: var(--vl-text-inverse);
}

.scroll-top-btn:hover {
  transform: translateY(-2px);
  box-shadow: 0 10px 24px var(--vl-primary-glow);
}

.scroll-top-enter-active,
.scroll-top-leave-active {
  transition: all 0.25s var(--vl-ease);
}

.scroll-top-enter-from,
.scroll-top-leave-to {
  opacity: 0;
  transform: translateY(12px) scale(0.9);
}

@media (max-width: 600px) {
  .section-header {
    gap: 0.65rem;
  }
  .search-box {
    min-width: unset;
    width: 100%;
    order: 10;
  }
  .filter-tabs {
    order: 5;
    overflow-x: auto;
    flex-wrap: nowrap;
    -webkit-overflow-scrolling: touch;
  }
}
</style>
