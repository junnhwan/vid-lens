// 原型：Agent 问答 UI 探索 — mock 类型与演示数据
// 问题：Research Agent 多轮规划/观察/证据累积在前端应如何呈现？

export type QaScope = 'video' | 'kb'

export type AgentDemoScenario = 'basic' | 'research'

export type StepStatus = 'pending' | 'running' | 'done' | 'error'

export interface EvidenceChunk {
  id: string
  text: string
  video?: string
  score?: number
}

export type AgentStep =
  | { id: string; kind: 'think'; label: string; detail: string; status: StepStatus }
  | { id: string; kind: 'plan'; label: string; detail: string; round: number; replan?: boolean; status: StepStatus }
  | { id: string; kind: 'observe'; label: string; detail: string; round: number; newEvidence?: EvidenceChunk[]; status: StepStatus }
  | { id: string; kind: 'retrieve'; label: string; query: string; hits: number; sources: string[]; round?: number; status: StepStatus }
  | { id: string; kind: 'tool'; label: string; tool: string; input: string; output?: string; round?: number; status: StepStatus }
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

export const DEMO_RESEARCH_QUESTION =
  '对比发布会与访谈中，三款产品各自被强调的核心卖点有何不同？需要跨片段归纳。'

export const DEMO_RESEARCH_STEPS: AgentStep[] = [
  {
    id: 'r1-plan', kind: 'plan', label: '制定检索策略', round: 1, status: 'pending',
    detail: '目标需要跨片段对比，先广搜「产品 + 卖点」相关转写，再按 SKU 分组。',
  },
  {
    id: 'r1-retrieve', kind: 'retrieve', label: '首轮检索', round: 1, status: 'pending',
    query: '产品 卖点 功能 发布',
    hits: 4,
    sources: ['2024 产品发布会全程'],
  },
  {
    id: 'r1-observe', kind: 'observe', label: '观察证据缺口', round: 1, status: 'pending',
    detail: '仅覆盖发布会段落，访谈中对「开发者工具链」的强调尚未命中，证据不足。',
    newEvidence: [
      { id: 'E1', text: '…AI 助手支持多模态理解，主打个人效率…', video: '2024 产品发布会全程', score: 0.91 },
      { id: 'E2', text: '…知识库平台强调跨视频严格 RAG…', video: '2024 产品发布会全程', score: 0.88 },
    ],
  },
  {
    id: 'r2-plan', kind: 'plan', label: '重规划检索', round: 2, replan: true, status: 'pending',
    detail: 'replan：缩小到「开发者工具链」「访谈」关键词，并展开时间窗口补检。',
  },
  {
    id: 'r2-retrieve', kind: 'retrieve', label: '补检访谈片段', round: 2, status: 'pending',
    query: '开发者 工具链 API SDK 访谈',
    hits: 3,
    sources: ['创始人深度访谈'],
  },
  {
    id: 'r2-tool', kind: 'tool', label: '归纳对比', round: 2, status: 'pending',
    tool: 'summarize_segments',
    input: 'group_by=product, compare=卖点',
    output: '三款产品在发布会偏功能发布，访谈中更强调生态与开发者体验',
  },
  {
    id: 'r2-observe', kind: 'observe', label: '证据已充分', round: 2, status: 'pending',
    detail: '已覆盖三款产品 × 两个来源，可生成带引用对比回答。',
    newEvidence: [
      { id: 'E3', text: '…工具链包含 API、SDK 与调试面板，面向开发者生态…', video: '创始人深度访谈', score: 0.93 },
    ],
  },
  {
    id: 'answer', kind: 'answer', label: '生成对比回答', status: 'pending',
    content: '发布会侧重功能亮相：AI 助手强调多模态 [C1]、知识库强调跨视频 RAG [C2]；访谈则突出开发者工具链的 API/SDK 生态 [C3]。同一 SKU 在不同语境下的卖点重心不同。',
    cites: [
      { id: 'C1', text: '…AI 助手支持文本、图像和视频的多模态理解…', video: '2024 产品发布会全程' },
      { id: 'C2', text: '…知识库平台可跨多个视频做严格 RAG 检索…', video: '2024 产品发布会全程' },
      { id: 'C3', text: '…开发者工具链包含 API、SDK 与调试面板…', video: '创始人深度访谈' },
    ],
  },
]

export const AGENT_STYLES = [
  { key: 'full', name: '融合版' },
  { key: 'simple', name: '极简版' },
] as const

export type AgentStyleKey = (typeof AGENT_STYLES)[number]['key']
