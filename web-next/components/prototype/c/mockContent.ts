// PROTOTYPE — 演示用长文本（列表 API 通常不带全文，弹窗打开时合并展示）

export const MOCK_TRANSCRIPTION_101 = `[00:00:12] 主持人：各位来宾、各位线上观众，大家好。欢迎来到映知 2026 春季发布会。我是今天的主持人小林。

[00:01:05] 今天我们将会发布三款重磅产品。第一款是「映知 AI 助手」——它不仅能理解视频画面，更能基于完整转写内容做引用式问答，每一个回答都可以追溯到原始片段。

[00:03:22] 第二款是「跨视频知识库」。你可以把产品发布会、用户访谈、技术分享归入同一个知识库，然后提问：「这几场活动对定价策略的说法有什么差异？」系统会跨视频检索，并在回答里标注 [C1][C2] 引用来源。

[00:06:48] 第三款面向开发者：开放 API 与 Webhook，支持分片上传、断点续传，以及 RabbitMQ 异步任务的可观测与重试。我们实测 2 小时发布会视频，端到端转写加索引约 18 分钟完成。

[00:09:15] CEO 张伟：关于定价，我们决定从一次性买断转向年度订阅。个人版 99 元/月，团队版按席位计费。早期用户可享受首年 7 折。

[00:12:40] 现场 Q&A：有观众问「严格 RAG 和普通问答有什么区别？」——严格模式下，没有建立向量索引就不会生成回答，避免模型幻觉；普通模式则允许模型结合转写做更自由的解读。

[00:15:03] 主持人：感谢各位。发布会议程到此结束，产品将于下月正式开放公测。`

export const MOCK_SUMMARY_101 = `## 发布会核心摘要

### 三款新产品
1. **映知 AI 助手** — 基于转写的引用式问答，回答带可追溯片段
2. **跨视频知识库** — 多视频联合检索，引用标注来源视频
3. **开发者工具链** — 开放 API、分片上传、异步任务可观测

### 定价策略（重要）
- 从「一次性买断」转向 **年度订阅制**
- 个人版：99 元/月
- 团队版：按席位计费
- 早期用户：首年 **7 折**

### 技术亮点
- 2 小时视频 → 转写 + 索引约 **18 分钟**
- 严格 RAG 模式：无索引则 fail closed，避免幻觉

### 现场问答要点
观众关心严格 RAG 与普通模式的差异：严格模式强制走检索；普通模式允许更自由的解读。`

export const MOCK_TRANSCRIPTION_102 = `[00:00:08] 记者：张总，能否回顾一下映知从 0 到 1 的关键节点？

[00:00:45] 张伟：最早我们只是想解决「长视频看不完」的问题。团队里有人提出：能不能把视频当文献来读？转写、摘要、批注、检索——这就是映知的起点。

[00:02:30] 记者：订阅制转型是怎么决定的？

[00:02:58] 张伟：订阅制是今年最重要的战略转向。SaaS 模式下我们可以持续迭代 ASR 和 RAG 质量，而不是卖断后无力维护。`

export const MOCK_SUMMARY_102 = `## 创始人访谈摘要

- **创业起点**：把长视频当「文献」来读——转写、摘要、检索
- **战略转向**：订阅制而非买断，以便持续投入 ASR / RAG 质量
- **产品哲学**：内容可追溯比「说得漂亮」更重要`

/** 原型弹窗：若 API 列表项缺转写/摘要，用 mock 补全以便预览 UI */
export function enrichTaskForPrototype<T extends {
  id: number
  title?: string
  filename: string
  status?: number
  has_transcription: boolean
  has_summary: boolean
  transcription?: { content: string; words: number; id?: number; task_id?: number; file_md5?: string; created_at?: string }
  summary?: { content: string; model_name?: string; id?: number; task_id?: number; file_md5?: string; created_at?: string }
}>(task: T, opts?: { demoFill?: boolean }): T {
  const patches: Record<number, { transcription?: string; summary?: string; words?: number }> = {
    101: { transcription: MOCK_TRANSCRIPTION_101, summary: MOCK_SUMMARY_101, words: 1240 },
    102: { transcription: MOCK_TRANSCRIPTION_102, summary: MOCK_SUMMARY_102, words: 380 },
  }

  const patch = patches[task.id]

  let next = { ...task }

  const isCompleted = task.status === 3
  const demoFill = opts?.demoFill && isCompleted

  const transcriptionText = patch?.transcription
    ?? (next.has_transcription && !next.transcription?.content
      ? MOCK_TRANSCRIPTION_101.replace('映知 2026 春季发布会', task.title || task.filename)
      : demoFill ? MOCK_TRANSCRIPTION_101.replace('映知 2026 春季发布会', task.title || task.filename) : undefined)

  const summaryText = patch?.summary
    ?? (next.has_summary && !next.summary?.content
      ? MOCK_SUMMARY_101
      : (!next.summary?.content && transcriptionText ? MOCK_SUMMARY_101 : demoFill ? MOCK_SUMMARY_101 : undefined))

  if (transcriptionText && !next.transcription?.content) {
    next = {
      ...next,
      has_transcription: true,
      transcription: {
        id: next.transcription?.id ?? 0,
        task_id: task.id,
        file_md5: '',
        content: transcriptionText,
        words: patch?.words ?? transcriptionText.length,
        created_at: next.transcription?.created_at ?? '',
      },
    }
  }

  if (summaryText && !next.summary?.content) {
    next = {
      ...next,
      has_summary: true,
      summary: {
        id: next.summary?.id ?? 0,
        task_id: task.id,
        file_md5: '',
        content: summaryText,
        model_name: next.summary?.model_name ?? 'gpt-4o',
        created_at: next.summary?.created_at ?? '',
      },
    }
  }

  return next
}
