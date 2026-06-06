<template>
  <section v-if="tasks.length" class="tasks-section">
    <div class="section-header">
      <h2>我的任务</h2>
      <div class="filter-tabs">
        <button v-for="tab in tabs" :key="tab.key"
                :class="['tab', { active: activeTab === tab.key }]"
                @click="activeTab = tab.key">
          {{ tab.label }} <span class="tab-count">{{ tab.count }}</span>
        </button>
      </div>
      <input v-model="searchQuery" class="search-box" placeholder="🔍 搜索文件名..." aria-label="搜索任务" />
    </div>

    <TransitionGroup name="task-list" tag="div" class="tasks-list">
      <TaskCard
        v-for="t in filteredTasks"
        :key="t.id"
        :task="t"
        :loading="loading[t.id]"
        @click="$emit('taskClick', t)"
        @delete="$emit('deleteTask', t)"
        @transcribe="$emit('transcribe', t)"
        @analyze="$emit('analyze', t)"
      />
    </TransitionGroup>

    <!-- 搜索无结果 -->
    <div v-if="tasks.length && !filteredTasks.length" class="empty-search">
      <div class="empty-search-icon">🔍</div>
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
    <div class="empty-icon">📦</div>
    <h3>还没有任务</h3>
    <p>从左侧上传你的第一个视频开始分析吧</p>
  </div>
</template>

<script setup>
import { ref, computed } from 'vue'
import TaskCard from './TaskCard.vue'

const props = defineProps({
  tasks: Array,
  loading: Object,
  hasMore: { type: Boolean, default: false },
  loadingMore: { type: Boolean, default: false }
})

defineEmits(['taskClick', 'deleteTask', 'transcribe', 'analyze', 'loadMore'])

const activeTab = ref('all')
const searchQuery = ref('')

const tabs = computed(() => [
  { key: 'all', label: '全部', count: props.tasks.length },
  { key: 'processing', label: '处理中', count: props.tasks.filter(t => t.status < 3).length },
  { key: 'completed', label: '已完成', count: props.tasks.filter(t => t.status === 3).length }
])

const filteredTasks = computed(() => {
  let result = props.tasks
  if (activeTab.value === 'processing') result = result.filter(t => t.status < 3)
  if (activeTab.value === 'completed') result = result.filter(t => t.status === 3)
  if (searchQuery.value) {
    const q = searchQuery.value.toLowerCase()
    result = result.filter(t => t.filename?.toLowerCase().includes(q))
  }
  return result
})
</script>

<style scoped>
/* 任务区 */
.tasks-section {
  animation: vl-fade-in-up 0.6s ease-out;
}

.section-header {
  display: flex;
  align-items: center;
  gap: 1.5rem;
  margin-bottom: 2rem;
  flex-wrap: wrap;
  padding-bottom: 1.25rem;
  border-bottom: 1px solid rgba(212, 175, 55, 0.1);
  position: relative;
}
.section-header::after {
  content: '';
  position: absolute;
  bottom: -1px;
  left: 0;
  width: 100px;
  height: 2px;
  background: linear-gradient(90deg, #d4af37, transparent);
}
.section-header h2 {
  font-size: 1.75rem;
  font-weight: 700;
  background: linear-gradient(135deg, #d4af37, #f4e4a6);
  -webkit-background-clip: text;
  -webkit-text-fill-color: transparent;
  background-clip: text;
  letter-spacing: 1px;
}
.filter-tabs {
  display: flex;
  gap: 0.75rem;
  flex: 1;
}
.tab {
  background: linear-gradient(135deg, rgba(15, 25, 45, 0.4), rgba(20, 30, 50, 0.3));
  border: 1px solid rgba(139, 149, 168, 0.2);
  padding: 0.65rem 1.25rem;
  border-radius: 0.75rem;
  color: #8b95a8;
  cursor: pointer;
  transition: all 0.3s cubic-bezier(0.4, 0, 0.2, 1);
  display: flex;
  align-items: center;
  gap: 0.65rem;
  backdrop-filter: blur(8px);
  font-weight: 500;
  font-size: 0.9rem;
  letter-spacing: 0.3px;
}
.tab:hover {
  border-color: rgba(212, 175, 55, 0.4);
  color: #d4af37;
  background: linear-gradient(135deg, rgba(212, 175, 55, 0.08), rgba(41, 98, 255, 0.05));
  transform: translateY(-2px);
}
.tab.active {
  background: linear-gradient(135deg, rgba(212, 175, 55, 0.15), rgba(41, 98, 255, 0.1));
  border-color: rgba(212, 175, 55, 0.5);
  color: #d4af37;
  box-shadow: 0 2px 8px rgba(212, 175, 55, 0.2), inset 0 1px 0 rgba(255, 255, 255, 0.1);
}
.tab-count {
  background: rgba(139, 149, 168, 0.2);
  padding: 0.2rem 0.6rem;
  border-radius: 0.4rem;
  font-size: 0.75rem;
  font-weight: 600;
  font-family: 'JetBrains Mono', monospace;
}
.tab.active .tab-count {
  background: rgba(212, 175, 55, 0.25);
  color: #f4e4a6;
}
.search-box {
  padding: 0.75rem 1.25rem;
  background: rgba(10, 14, 26, 0.5);
  border: 1px solid rgba(139, 149, 168, 0.2);
  border-radius: 0.75rem;
  color: #e8eef7;
  outline: none;
  min-width: 240px;
  transition: all 0.3s;
  backdrop-filter: blur(8px);
  font-size: 0.95rem;
}
.search-box:focus {
  border-color: #d4af37;
  box-shadow: 0 0 0 3px rgba(212, 175, 55, 0.15), 0 2px 8px rgba(212, 175, 55, 0.2);
}
.search-box::placeholder {
  color: #5a6477;
}

/* 任务列表 + TransitionGroup */
.tasks-list {
  display: flex;
  flex-direction: column;
  gap: 1.5rem;
}

.task-list-enter-active {
  transition: all 0.4s cubic-bezier(0.4, 0, 0.2, 1);
}
.task-list-leave-active {
  transition: all 0.3s cubic-bezier(0.4, 0, 0.2, 1);
  position: relative;
}
.task-list-enter-from {
  opacity: 0;
  transform: translateY(20px);
}
.task-list-leave-to {
  opacity: 0;
  transform: translateX(-30px);
}
.task-list-move {
  transition: transform 0.4s cubic-bezier(0.4, 0, 0.2, 1);
}

/* 搜索无结果 */
.empty-search {
  text-align: center;
  padding: 3rem 1rem;
  opacity: 0.6;
}

.empty-search-icon {
  font-size: 3rem;
  margin-bottom: 1rem;
}

.empty-search p {
  color: #8b95a8;
  font-size: 0.95rem;
}

/* 加载更多 */
.load-more {
  text-align: center;
  margin-top: 2rem;
  padding-top: 1.5rem;
  border-top: 1px solid rgba(139, 149, 168, 0.1);
}

.load-more-btn {
  background: linear-gradient(135deg, rgba(15, 25, 45, 0.5), rgba(20, 30, 50, 0.4));
  border: 1px solid rgba(139, 149, 168, 0.25);
  padding: 0.75rem 2rem;
  border-radius: 0.75rem;
  color: #8b95a8;
  cursor: pointer;
  transition: all 0.3s;
  font-weight: 600;
  font-size: 0.9rem;
}

.load-more-btn:hover:not(:disabled) {
  border-color: rgba(212, 175, 55, 0.4);
  color: #d4af37;
  transform: translateY(-2px);
}

.load-more-btn:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

/* 空状态 */
.empty-state {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  min-height: 400px;
  text-align: center;
  opacity: 0.6;
}

.empty-icon {
  font-size: 4rem;
  margin-bottom: 1rem;
  filter: drop-shadow(0 4px 12px rgba(212, 175, 55, 0.2));
}

.empty-state h3 {
  font-size: 1.5rem;
  margin-bottom: 0.5rem;
  background: linear-gradient(135deg, #d4af37, #f4e4a6);
  -webkit-background-clip: text;
  -webkit-text-fill-color: transparent;
  background-clip: text;
}

.empty-state p {
  color: #8b95a8;
  font-size: 0.95rem;
}

/* 响应式 */
@media (max-width: 600px) {
  .section-header {
    gap: 1rem;
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
