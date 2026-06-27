import DefaultTheme from 'vitepress/theme'
import './custom.css'
import TaskStateMachine from './components/TaskStateMachine.vue'

export default {
  extends: DefaultTheme,
  enhanceApp({ app }) {
    app.component('TaskStateMachine', TaskStateMachine)
  }
}
