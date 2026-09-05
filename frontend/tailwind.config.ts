import type { Config } from 'tailwindcss'

// 设计系统的视觉 token 全部在 app/globals.css(以 docs/prototype/styles.css 为唯一源),
// 原型的组件类(.btn/.chip/.card/...)是全局 CSS 类,不依赖 tailwind。
// tailwind 只负责 TSX 里的布局类(flex/grid/gap 等)与字体。
const config: Config = {
  content: [
    './app/**/*.{ts,tsx}',
    './components/**/*.{ts,tsx}',
    '!./components/prototype/**/*.{ts,tsx}',
  ],
  theme: {
    extend: {
      fontFamily: {
        sans: ['"Space Grotesk"', '-apple-system', '"Segoe UI"', '"PingFang SC"', '"Hiragino Sans GB"', '"Microsoft YaHei"', 'sans-serif'],
        mono: ['"JetBrains Mono"', 'ui-monospace', '"Cascadia Code"', 'Consolas', 'monospace'],
      },
    },
  },
  plugins: [],
}
export default config
