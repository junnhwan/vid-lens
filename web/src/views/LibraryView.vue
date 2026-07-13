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

    <main class="content-area">
      <TaskList
        :tasks="app.tasks"
        :loading="app.loading"
        :initialLoading="app.tasksLoading"
        :hasMore="app.hasMore"
        :loadingMore="app.loadingMore"
        :loadError="app.tasksLoadError"
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
  </div>

  <TaskDrawer
    :task="app.selectedTask"
    :loading="app.loading[app.selectedTask?.id]"
    @close="app.closeDrawer"
    @transcribe="app.doTranscribe(app.selectedTask)"
    @analyze="app.doAnalyze(app.selectedTask)"
    @toast="(msg) => app.showToast(msg)"
  />
</template>

<script setup>
import { inject } from 'vue'
import { useRouter } from 'vue-router'
import Sidebar from '../components/Sidebar.vue'
import TaskList from '../components/TaskList.vue'
import TaskDrawer from '../components/TaskDrawer.vue'

const app = inject('appCtx')
const router = useRouter()

const goChat = (task) => {
  router.push({ name: 'chat-task', params: { taskId: task.id } })
}

// 支持把视频拖到页面任意位置上传（Sidebar 上传卡用 .stop 自行处理，不会重复触发）
const onGlobalDrop = (e) => {
  const file = e.dataTransfer?.files?.[0]
  if (!file) return
  app.handleFileUpload(file)
}
</script>

<style scoped>
.app-layout {
  display: flex;
  min-height: calc(100vh - var(--vl-nav-h));
  max-width: 1440px;
  margin: 0 auto;
}

.content-area {
  flex: 1;
  min-width: 0;
  padding: 1.35rem 1.75rem 2rem;
  overflow-y: auto;
}

@media (max-width: 900px) {
  .content-area {
    padding: 1.1rem 1rem 1.5rem;
  }
}
</style>
