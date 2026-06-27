import { defineConfig } from 'vitepress'

export default defineConfig({
  title: 'VidLens 面试宝典',
  description: '基于 VidLens 项目的 Go 后端面试准备书 — 源码走读 + 高频面试题 + 八股速查',
  base: '/vid-lens/',
  srcDir: './docs',
  ignoreDeadLinks: true,
  themeConfig: {
    nav: [
      { text: '首页', link: '/' },
      { text: '面试题', link: '/interview/' },
      { text: '源码走读', link: '/source/' },
      { text: '动画实验', link: '/animations/' },
      { text: '八股速查', link: '/reference/' },
    ],
    sidebar: {
      '/interview/': [
        {
          text: '🎯 面试题',
          items: [
            { text: '总览', link: '/interview/' },
            { text: '面试作战手册', link: '/interview/playbook/' },
            { text: '1. 架构与启动流程', link: '/interview/architecture/' },
            { text: '2. AI 策略层', link: '/interview/ai-strategy/' },
            { text: '3. Kafka 异步处理', link: '/interview/kafka-async/' },
            { text: '4. 分布式锁', link: '/interview/distributed-lock/' },
            { text: '5. 令牌桶限流', link: '/interview/rate-limiting/' },
            { text: '6. RAG 检索管道', link: '/interview/rag-pipeline/' },
            { text: '7. 媒体上传与存储', link: '/interview/media-upload/' },
            { text: '8. 数据模型设计', link: '/interview/data-model/' },
            { text: '9. Repository 层', link: '/interview/repository/' },
            { text: '10. 安全体系', link: '/interview/security/' },
          ]
        }
      ],
      '/source/': [
        {
          text: '📖 源码走读',
          items: [
            { text: '总览', link: '/source/' },
            { text: '1. 架构与启动流程', link: '/source/architecture/' },
            { text: '2. AI 策略层', link: '/source/ai-strategy/' },
            { text: '3. Kafka 异步处理', link: '/source/kafka-async/' },
            { text: '4. 分布式锁', link: '/source/distributed-lock/' },
            { text: '5. 令牌桶限流', link: '/source/rate-limiting/' },
            { text: '6. RAG 检索管道', link: '/source/rag-pipeline/' },
            { text: '7. 媒体上传与存储', link: '/source/media-upload/' },
            { text: '8. 数据模型设计', link: '/source/data-model/' },
            { text: '9. Repository 层', link: '/source/repository/' },
            { text: '10. 安全体系', link: '/source/security/' },
          ]
        }
      ],
      '/animations/': [
        {
          text: '🎬 动画实验',
          items: [
            { text: '总览', link: '/animations/' },
          ]
        }
      ],
      '/reference/': [
        {
          text: '📋 八股速查',
          items: [
            { text: '速查表', link: '/reference/' },
            { text: '简历话术 & 项目介绍', link: '/reference/resume' },
          ]
        }
      ]
    },
    socialLinks: [
      { icon: 'github', link: 'https://github.com/junnhwan/vid-lens' }
    ],
    search: {
      provider: 'local'
    },
    outline: {
      level: [2, 3],
      label: '页面导航'
    }
  }
})
