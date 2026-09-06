// 回答文本里的引用标记解析:LLM 按提示词在事实后写 [C1][C2] 独立标记,
// 后端持久化前会剥掉它们(chat_stream done.answer 为干净文本),但流式增量
// 里带着原始标记。展示层把标记渲染成行内 C# chip,done 后内容已被后端
// 清洗,行内 chip 自然消失,引用统一由答案下方的引用卡承接。

export type AnswerToken = string | { cite: number }

const CITE_TOKEN = /\[C(\d+)\]/g

export function parseAnswerTokens(content: string): AnswerToken[] {
  const tokens: AnswerToken[] = []
  let cursor = 0
  for (const match of content.matchAll(CITE_TOKEN)) {
    const start = match.index ?? 0
    if (start > cursor) tokens.push(content.slice(cursor, start))
    tokens.push({ cite: Number(match[1]) })
    cursor = start + match[0].length
  }
  if (cursor < content.length) tokens.push(content.slice(cursor))
  return tokens
}
