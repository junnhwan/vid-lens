import { createRouter, createWebHashHistory } from 'vue-router'
import LibraryView from './views/LibraryView.vue'
import ChatView from './views/ChatView.vue'
import SettingsView from './views/SettingsView.vue'

// 用 hash 路由：后端（Go）serve 前端 dist 时无需配置 SPA fallback。
const router = createRouter({
  history: createWebHashHistory(),
  routes: [
    { path: '/', redirect: '/library' },
    {
      path: '/library',
      name: 'library',
      component: LibraryView,
      meta: { pageKey: 'library' },
    },
    {
      path: '/chat',
      name: 'chat',
      component: ChatView,
      meta: { pageKey: 'chat' },
    },
    {
      path: '/chat/:taskId',
      name: 'chat-task',
      component: ChatView,
      // 同一 pageKey：切换视频不触发整页 transition，避免闪一下
      meta: { pageKey: 'chat' },
    },
    {
      path: '/settings',
      name: 'settings',
      component: SettingsView,
      meta: { pageKey: 'settings' },
    },
  ],
})

export default router
