<template>
  <div class="app-layout" @dragover.prevent @drop.prevent="onGlobalDrop">
    <Sidebar
      :user="app.user"
      :uploading="app.uploading"
      :uploadMsg="app.uploadMsg"
      :uploadProgress="app.uploadProgress"
      :stats="app.taskStats"
      :mobileOpen="app.sidebarOpen"
      @uploadFile="app.handleFileUpload"
      @uploadUrl="app.handleUrlUpload"
      @openAuth="app.openAuth"
      @closeSidebar="app.closeSidebar"
      @toast="(msg) => app.showToast(msg, true)"
    />

    <div class="library-main" :class="{ 'has-selection': !!app.selectedTask }">
      <!-- 左：任务列表 -->
      <main
        class="list-pane"
        :class="{ 'mobile-hidden': isMobile && app.selectedTask }"
      >
        <TaskList
          :tasks="app.tasks"
          :loading="app.loading"
          :initialLoading="app.tasksLoading"
          :hasMore="app.hasMore"
          :loadingMore="app.loadingMore"
          :loadError="app.tasksLoadError"
          :selectedId="app.selectedTask?.id"
          :compactList="!!app.selectedTask && !isMobile"
          @taskClick="app.openTaskDrawer"
          @deleteTask="app.deleteTask"
          @transcribe="app.doTranscribe"
          @analyze="app.doAnalyze"
          @loadMore="app.loadMoreTasks"
          @chat="goChat"
          @retry="app.retryLoadTasks"
          @search="app.onSearchTasks"
        />
      </main>

      <!-- 右：桌面详情栏（仅选中时出现，避免空状态挤占列表） -->
      <div v-if="!isMobile && app.selectedTask" class="detail-pane">
        <TaskDetailPanel
          :task="app.selectedTask"
          :loading="app.loading[app.selectedTask?.id]"
          @close="app.closeDrawer"
          @transcribe="app.doTranscribe(app.selectedTask)"
          @analyze="app.doAnalyze(app.selectedTask)"
          @toast="(msg) => app.showToast(msg)"
        />
      </div>
    </div>

    <!-- 移动端：全屏详情（非遮罩抽屉，整页切换） -->
    <div
      v-if="isMobile && app.selectedTask"
      class="mobile-detail-sheet"
    >
      <TaskDetailPanel
        :task="app.selectedTask"
        :loading="app.loading[app.selectedTask?.id]"
        mobile-sheet
        @close="app.closeDrawer"
        @transcribe="app.doTranscribe(app.selectedTask)"
        @analyze="app.doAnalyze(app.selectedTask)"
        @toast="(msg) => app.showToast(msg)"
      />
    </div>
  </div>
</template>

<script setup>
import { inject, ref, onMounted, onUnmounted, computed } from 'vue'
import { useRouter } from 'vue-router'
import Sidebar from '../components/Sidebar.vue'
import TaskList from '../components/TaskList.vue'
import TaskDetailPanel from '../components/TaskDetailPanel.vue'

const app = inject('appCtx')
const router = useRouter()

const viewportW = ref(typeof window !== 'undefined' ? window.innerWidth : 1200)
const isMobile = computed(() => viewportW.value <= 900)

const goChat = (task) => {
  router.push({ name: 'chat-task', params: { taskId: task.id } })
}

const onResize = () => {
  viewportW.value = window.innerWidth
}

// 支持把视频拖到页面任意位置上传（Sidebar 上传卡用 .stop 自行处理，不会重复触发）
const onGlobalDrop = (e) => {
  const file = e.dataTransfer?.files?.[0]
  if (!file) return
  app.handleFileUpload(file)
}

onMounted(() => {
  window.addEventListener('resize', onResize)
  onResize()
})

onUnmounted(() => {
  window.removeEventListener('resize', onResize)
})
</script>

<style scoped>
.app-layout {
  display: flex;
  min-height: calc(100vh - var(--vl-nav-h));
  max-width: 1440px;
  margin: 0 auto;
  position: relative;
}

.library-main {
  flex: 1;
  min-width: 0;
  display: flex;
  min-height: calc(100vh - var(--vl-nav-h));
}

.list-pane {
  flex: 1;
  min-width: 0;
  padding: 1.35rem 1.5rem 2rem 1.75rem;
  overflow-y: auto;
  transition: flex 0.25s var(--vl-ease), max-width 0.25s var(--vl-ease), padding 0.25s var(--vl-ease);
}

/* 有选中时左栏收窄，给详情更多阅读宽度 */
.library-main.has-selection .list-pane {
  flex: 0 0 min(360px, 34%);
  max-width: 400px;
  padding-right: 0.85rem;
}

.detail-pane {
  flex: 1;
  min-width: 0;
  padding: 1.35rem 1.5rem 1.75rem 0.35rem;
  display: flex;
  flex-direction: column;
  min-height: 0;
  height: calc(100vh - var(--vl-nav-h));
  position: sticky;
  top: var(--vl-nav-h);
  align-self: flex-start;
  animation: vl-fade-in-up 0.28s var(--vl-ease);
}

.detail-pane > :deep(.detail-panel) {
  flex: 1;
  min-height: 0;
}

.mobile-detail-sheet {
  position: fixed;
  top: var(--vl-nav-h);
  left: 0;
  right: 0;
  bottom: 0;
  z-index: 90;
  background: var(--vl-bg);
  display: flex;
  flex-direction: column;
}

.mobile-detail-sheet > :deep(.detail-panel) {
  flex: 1;
  min-height: 0;
  border-radius: 0;
  border: none;
}

@media (max-width: 900px) {
  .list-pane {
    flex: 1 1 auto;
    max-width: none;
    padding: 1.1rem 1rem 1.5rem;
  }

  .library-main.has-selection .list-pane {
    flex: 1;
    max-width: none;
  }

  .list-pane.mobile-hidden {
    display: none;
  }
}
</style>
