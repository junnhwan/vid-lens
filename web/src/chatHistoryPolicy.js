export function parseRetrievalSnapshot(snapshot) {
  if (!snapshot) return []
  if (Array.isArray(snapshot)) return snapshot

  try {
    const parsed = typeof snapshot === 'string' ? JSON.parse(snapshot) : snapshot
    return Array.isArray(parsed) ? parsed : []
  } catch {
    return []
  }
}

export function normalizeChatMessages(messages) {
  if (!Array.isArray(messages)) return []
  return messages.map((message) => ({
    id: message.id,
    role: message.role,
    content: message.content || '',
    citations: parseRetrievalSnapshot(message.retrieval_snapshot),
    timestamp: message.created_at || message.timestamp || null,
  }))
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
