import { createApp } from 'vue'
import './utils/shared.css'
import './utils/themes.css'
import { initTheme } from './theme.js'
import App from './App.vue'
import router from './router.js'

initTheme()
createApp(App).use(router).mount('#app')
