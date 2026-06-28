<template>
  <div class="app-layout">
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
    />

    <main class="content-area">
      <TaskList
        :tasks="app.tasks"
        :loading="app.loading"
        :initialLoading="app.tasksLoading"
        :hasMore="app.hasMore"
        :loadingMore="app.loadingMore"
        @taskClick="app.openTaskDrawer"
        @deleteTask="app.deleteTask"
        @transcribe="app.doTranscribe"
        @analyze="app.doAnalyze"
        @loadMore="app.loadMoreTasks"
        @chat="goChat"
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
</script>

<style scoped>
.app-layout {
  display: flex;
  min-height: calc(100vh - 80px);
  max-width: 1600px;
  margin: 0 auto;
  padding: 0;
  position: relative;
  z-index: 2;
}

.content-area {
  flex: 1;
  padding: 2rem 3rem;
  overflow-y: auto;
  scrollbar-width: thin;
  scrollbar-color: rgba(212, 175, 55, 0.3) transparent;
}

.content-area::-webkit-scrollbar {
  width: 8px;
}

.content-area::-webkit-scrollbar-track {
  background: transparent;
}

.content-area::-webkit-scrollbar-thumb {
  background: rgba(212, 175, 55, 0.3);
  border-radius: 4px;
}

.content-area::-webkit-scrollbar-thumb:hover {
  background: rgba(212, 175, 55, 0.5);
}

@media (max-width: 900px) {
  .content-area {
    padding: 1.5rem 1rem;
  }
}
</style>
