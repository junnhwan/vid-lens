// 原型：Agent 问答 UI 探索 — mock 类型与演示数据

export type QaScope = 'video' | 'kb'

export type StepStatus = 'pending' | 'running' | 'done' | 'error'

export type AgentStep =
  | { id: string; kind: 'think'; label: string; detail: string; status: StepStatus }
  | { id: string; kind: 'retrieve'; label: string; query: string; hits: number; sources: string[]; status: StepStatus }
  | { id: string; kind: 'tool'; label: string; tool: string; input: string; output?: string; status: StepStatus }
  | { id: string; kind: 'answer'; label: string; content: string; cites: { id: string; text: string; video?: string }[]; status: StepStatus }

export const DEMO_VIDEOS = [
  { id: 101, title: '2024 产品发布会全程' },
  { id: 102, title: '创始人深度访谈' },
  { id: 103, title: '技术架构解析' },
]

export const DEMO_KBS = [
  { id: 1, name: '分布式系统 · 核心知识库', videos: 4 },
  { id: 2, name: 'AI 产品研究', videos: 7 },
]

export const DEMO_USER_QUESTION = '这场发布会提到了哪些新产品？各自的核心卖点是什么？'

export const DEMO_STEPS_TEMPLATE: AgentStep[] = [
  {
    id: 's1', kind: 'think', label: '理解问题', status: 'pending',
    detail: '用户在问发布会新产品与卖点，需要从转写中定位「产品发布」相关段落，并区分不同 SKU。',
  },
  {
    id: 's2', kind: 'retrieve', label: '检索转写片段', status: 'pending',
    query: '发布会 新产品 发布 功能',
    hits: 6,
    sources: ['2024 产品发布会全程', '创始人深度访谈'],
  },
  {
    id: 's3', kind: 'tool', label: '调用工具', status: 'pending',
    tool: 'summarize_segments',
    input: 'chunk_ids=[12,18,24]',
    output: '提取到 3 款产品：AI 助手、知识库平台、开发者工具链',
  },
  {
    id: 's4', kind: 'answer', label: '生成回答', status: 'pending',
    content: '根据转写，发布会重点介绍了三款产品：AI 助手（多模态理解）[C1]、知识库平台（跨视频 RAG）[C2]、开发者工具链（API + SDK）[C3]。',
    cites: [
      { id: 'C1', text: '…AI 助手支持文本、图像和视频的多模态理解…', video: '2024 产品发布会全程' },
      { id: 'C2', text: '…知识库平台可跨多个视频做严格 RAG 检索…', video: '2024 产品发布会全程' },
      { id: 'C3', text: '…开发者工具链包含 API、SDK 与调试面板…', video: '创始人深度访谈' },
    ],
  },
]

export const VARIANTS = [
  { key: 'D', name: '融合（推荐）' },
  { key: 'E', name: '阶段地铁条' },
  { key: 'A', name: '时间线流水线' },
  { key: 'B', name: '消息内嵌步骤' },
  { key: 'C', name: '分屏工作区' },
] as const

export type AgentVariantKey = (typeof VARIANTS)[number]['key']
