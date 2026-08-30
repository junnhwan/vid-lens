import type { Config } from 'tailwindcss'

// 颜色 token 引用 CSS 变量；2 套主题在 globals.css 用 [data-theme] 切换变量值。
// 这样切主题只改 html 上的 data-theme 属性，Tailwind class 不变。
const config: Config = {
  content: [
    './app/**/*.{ts,tsx}',
    './components/**/*.{ts,tsx}',
    '!./components/prototype/**/*.{ts,tsx}',
  ],
  theme: {
    extend: {
      colors: {
        // paper: 0 白底 / 1 页面底 / 2 次级面 / 3 更深面（Tailwind 索引式：paper-0..paper-3）
        paper: {
          0: 'var(--paper-0)', 1: 'var(--paper-1)', 2: 'var(--paper-2)', 3: 'var(--paper-3)',
        },
        // ink: 0 全墨正文 / 1 主文 / 2 次文 / 3 弱 / 4 更弱 / 5 最弱
        ink: {
          0: 'var(--ink-0)', 1: 'var(--ink-1)', 2: 'var(--ink-2)', 3: 'var(--ink-3)', 4: 'var(--ink-4)', 5: 'var(--ink-5)',
        },
        // accent 在 mockup 里叫 sienna，但每套主题映射不同色相
        sienna: { 400: 'var(--accent-400)', 500: 'var(--accent-500)', 600: 'var(--accent-600)', 700: 'var(--accent-700)' },
        moss: 'var(--done)',
        rust: 'var(--fail)',
      },
      fontFamily: {
        // Geist 经 Google Fonts <link> 加载；Noto Sans SC 作中文 fallback
        sans: ['Geist', '"Noto Sans SC"', 'system-ui', 'sans-serif'],
        mono: ['"Geist Mono"', '"JetBrains Mono"', 'ui-monospace', 'monospace'],
      },
    },
  },
  plugins: [],
}
export default config
