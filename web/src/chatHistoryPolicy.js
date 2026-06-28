function parseSnapshotValue(snapshot) {
  if (snapshot == null) return null
  if (typeof snapshot === 'string') {
    try {
      return JSON.parse(snapshot)
    } catch {
      return null
    }
  }
  return snapshot
}

// 普通 RAG 的 retrieval_snapshot 是 citations 数组；
// Agentic QA 的 retrieval_snapshot 是对象 { template, citations, trace }。
// 这里统一抽取 citations，供只关心引用片段的调用方使用。
export function parseRetrievalSnapshot(snapshot) {
  const parsed = parseSnapshotValue(snapshot)
  if (Array.isArray(parsed)) return parsed
  if (parsed && Array.isArray(parsed.citations)) return parsed.citations
  return []
}

// 返回完整的快照元数据，供需要展示 template / trace 的 Agentic 消息使用。
export function parseRetrievalSnapshotDetail(snapshot) {
  const parsed = parseSnapshotValue(snapshot)
  if (Array.isArray(parsed)) {
    return { template: null, citations: parsed, trace: [], mode: 'rag' }
  }
  if (parsed && typeof parsed === 'object') {
    const trace = Array.isArray(parsed.trace) ? parsed.trace : []
    const template = parsed.template || null
    return {
      template,
      citations: Array.isArray(parsed.citations) ? parsed.citations : [],
      trace,
      mode: template || trace.length ? 'agent' : 'rag',
    }
  }
  return { template: null, citations: [], trace: [], mode: 'rag' }
}

export function normalizeChatMessages(messages) {
  if (!Array.isArray(messages)) return []
  return messages.map((message) => {
    const detail = parseRetrievalSnapshotDetail(message.retrieval_snapshot)
    return {
      id: message.id,
      role: message.role,
      content: message.content || '',
      citations: detail.citations,
      timestamp: message.created_at || message.timestamp || null,
      template: detail.template,
      trace: detail.trace,
      model: message.model || null,
      mode: message.mode || detail.mode,
    }
  })
}

export async function resolveReusableChatSession(sessions, loadMessages) {
  const list = Array.isArray(sessions) ? sessions : []
  if (!list.length) return { session: null, messages: [] }

  const fallback = list[0]
  for (const session of list) {
    const messages = await loadMessages(session.id)
    if (Array.isArray(messages) && messages.length > 0) {
      return { session, messages }
    }
  }
  return { session: fallback, messages: [] }
}
