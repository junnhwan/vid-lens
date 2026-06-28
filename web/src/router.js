import { createRouter, createWebHashHistory } from 'vue-router'
import LibraryView from './views/LibraryView.vue'
import ChatView from './views/ChatView.vue'

// 用 hash 路由：后端（Go）serve 前端 dist 时无需配置 SPA fallback。
const router = createRouter({
  history: createWebHashHistory(),
  routes: [
    { path: '/', redirect: '/library' },
    { path: '/library', name: 'library', component: LibraryView },
    { path: '/chat', name: 'chat', component: ChatView },
    { path: '/chat/:taskId', name: 'chat-task', component: ChatView },
  ],
})

export default router
