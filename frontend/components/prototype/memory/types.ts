export type MemoryStatus = 'active' | 'conflicted' | 'withdrawn'
export type MemoryScopeType = 'user' | 'video' | 'knowledge_base' | 'run'

export interface MemoryItem {
  id: string
  scopeType: MemoryScopeType
  scopeId: string
  scopeLabel: string
  kind: string
  content: string
  sourceType: string
  sourceRef: string
  importance: number
  status: MemoryStatus
  version: number
  createdLabel: string
  lastUsedLabel?: string
  expiresLabel?: string
  embeddingReady: boolean
}

export const MEMORY_VARIANTS = [
  { key: 'A', name: '设置里的偏好清单' },
  { key: 'B', name: '按范围的档案柜' },
  { key: 'C', name: '冲突优先的治理台' },
] as const

export type MemoryVariantKey = (typeof MEMORY_VARIANTS)[number]['key']

export const KIND_LABEL: Record<string, string> = {
  response_preference: '回答偏好',
  format: '回答格式',
  language: '语言',
  topic: '关注主题',
  alias: '视频别名',
  speaker: '主讲人',
  term: '领域术语',
  open: '待确认',
}

export const SCOPE_TYPE_LABEL: Record<MemoryScopeType, string> = {
  user: '我的偏好',
  video: '视频',
  knowledge_base: '知识库',
  run: '本次问答',
}

export const SOURCE_LABEL: Record<string, string> = {
  user_message: '你说过',
  user_confirmation: '你确认过',
  verified_claim: '已核验事实',
  manual: '手工写入',
  run_observation: '本次问答观察',
}

export const SEED_MEMORIES: MemoryItem[] = [
  {
    id: 'u-lang-zh',
    scopeType: 'user', scopeId: '1', scopeLabel: '我',
    kind: 'language', content: '用中文回答',
    sourceType: 'user_message', sourceRef: 'message:88',
    importance: 0.9, status: 'active', version: 1,
    createdLabel: '8 天前', lastUsedLabel: '今天 14:02',
    embeddingReady: true,
  },
  {
    id: 'u-format',
    scopeType: 'user', scopeId: '1', scopeLabel: '我',
    kind: 'format', content: '先给结论，再展开依据',
    sourceType: 'user_message', sourceRef: 'message:91',
    importance: 0.8, status: 'active', version: 1,
    createdLabel: '6 天前', lastUsedLabel: '昨天',
    embeddingReady: true,
  },
  {
    id: 'u-brief',
    scopeType: 'user', scopeId: '1', scopeLabel: '我',
    kind: 'response_preference', content: '回答尽量简洁',
    sourceType: 'user_message', sourceRef: 'message:12',
    importance: 0.7, status: 'conflicted', version: 2,
    createdLabel: '12 天前', lastUsedLabel: '3 天前',
    embeddingReady: true,
  },
  {
    id: 'u-detail',
    scopeType: 'user', scopeId: '1', scopeLabel: '我',
    kind: 'response_preference', content: '回答尽量详细，把证据列全',
    sourceType: 'user_message', sourceRef: 'message:77',
    importance: 0.7, status: 'conflicted', version: 1,
    createdLabel: '4 天前', lastUsedLabel: '今天 11:20',
    embeddingReady: true,
  },
  {
    id: 'u-topic-withdrawn',
    scopeType: 'user', scopeId: '1', scopeLabel: '我',
    kind: 'topic', content: '之后都围绕融资节奏提问',
    sourceType: 'user_message', sourceRef: 'message:40',
    importance: 0.5, status: 'withdrawn', version: 3,
    createdLabel: '3 周前', lastUsedLabel: '2 周前',
    embeddingReady: false,
  },
  {
    id: 'v-101-alias',
    scopeType: 'video', scopeId: '101', scopeLabel: '产品发布会全程实录',
    kind: 'alias', content: '这是 2024 春季发布会',
    sourceType: 'user_confirmation', sourceRef: 'message:102',
    importance: 0.85, status: 'active', version: 1,
    createdLabel: '2 天前', lastUsedLabel: '今天 14:02',
    embeddingReady: true,
  },
  {
    id: 'v-101-speaker-a',
    scopeType: 'video', scopeId: '101', scopeLabel: '产品发布会全程实录',
    kind: 'speaker', content: '主讲人是张薇',
    sourceType: 'user_confirmation', sourceRef: 'message:104',
    importance: 0.8, status: 'conflicted', version: 1,
    createdLabel: '2 天前',
    embeddingReady: true,
  },
  {
    id: 'v-101-speaker-b',
    scopeType: 'video', scopeId: '101', scopeLabel: '产品发布会全程实录',
    kind: 'speaker', content: '主讲人是李哲',
    sourceType: 'user_confirmation', sourceRef: 'message:109',
    importance: 0.75, status: 'conflicted', version: 1,
    createdLabel: '昨天',
    embeddingReady: true,
  },
  {
    id: 'v-102-topic',
    scopeType: 'video', scopeId: '102', scopeLabel: '创始人访谈：从 0 到 1',
    kind: 'topic', content: '关注融资与团队故事，少谈技术细节',
    sourceType: 'user_confirmation', sourceRef: 'message:201',
    importance: 0.7, status: 'active', version: 1,
    createdLabel: '5 天前', lastUsedLabel: '昨天',
    embeddingReady: true,
  },
  {
    id: 'kb-term',
    scopeType: 'knowledge_base', scopeId: '1', scopeLabel: '产品研究',
    kind: 'term', content: '「映知」指视频理解产品，不是监控系统',
    sourceType: 'manual', sourceRef: 'manual:1',
    importance: 0.9, status: 'active', version: 1,
    createdLabel: '1 周前', lastUsedLabel: '今天 09:40',
    embeddingReady: true,
  },
  {
    id: 'run-open',
    scopeType: 'run', scopeId: 'run-a', scopeLabel: '刚才那轮问答',
    kind: 'open', content: '用户是否还要对比竞品定价，尚未确认',
    sourceType: 'run_observation', sourceRef: 'run:run-a',
    importance: 0.4, status: 'active', version: 1,
    createdLabel: '今天 14:02', expiresLabel: '本轮结束后失效',
    embeddingReady: false,
  },
]

export function kindLabel(kind: string) {
  return KIND_LABEL[kind] ?? kind
}

export function sourceLabel(sourceType: string) {
  return SOURCE_LABEL[sourceType] ?? sourceType
}

export function scopeKey(item: Pick<MemoryItem, 'scopeType' | 'scopeId'>) {
  return `${item.scopeType}:${item.scopeId}`
}

export function conflictKey(item: Pick<MemoryItem, 'scopeType' | 'scopeId' | 'kind'>) {
  return `${item.scopeType}:${item.scopeId}:${item.kind}`
}

export function groupConflicts(items: MemoryItem[]) {
  const map = new Map<string, MemoryItem[]>()
  for (const item of items) {
    if (item.status !== 'conflicted') continue
    const key = conflictKey(item)
    const group = map.get(key) ?? []
    group.push(item)
    map.set(key, group)
  }
  return [...map.values()].filter(group => group.length > 1)
}

export function countByStatus(items: MemoryItem[]) {
  return {
    active: items.filter(i => i.status === 'active').length,
    conflicted: items.filter(i => i.status === 'conflicted').length,
    withdrawn: items.filter(i => i.status === 'withdrawn').length,
  }
}
