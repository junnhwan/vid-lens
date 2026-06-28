<template>
  <div class="rag-chat">
    <!-- 索引状态提示 -->
    <div v-if="indexStatus.status !== 'indexed'" class="index-prompt">
      <div class="prompt-icon">{{ indexIcon }}</div>
      <p>{{ indexPromptText }}</p>

      <!-- 索引构建按钮 -->
      <button
        v-if="indexStatus.status !== 'indexing'"
        class="btn-amber"
        @click="buildIndex"
        :disabled="building"
      >
        {{ building ? '构建中...' : indexButtonText }}
      </button>

      <!-- 构建中提示 -->
      <div v-if="indexStatus.status === 'indexing'" class="indexing-spinner">
        <div class="spinner"></div>
        <span>索引构建中，请稍候...</span>
      </div>

      <!-- 失败时显示错误 -->
      <div v-if="indexStatus.status === 'failed' && indexStatus.error" class="index-error">
        <span class="error-label">失败原因:</span>
        <span class="error-text">{{ indexStatus.error }}</span>
      </div>
    </div>

    <div class="chat-container">
      <div class="chat-messages" ref="messagesContainer">
        <div v-for="(msg, msgIdx) in messages" :key="msg.id || msgIdx" class="message" :class="msg.role">
          <div class="message-content">
            <div v-if="msg.role === 'assistant'" class="message-text markdown-body" v-html="renderMarkdown(msg.content)"></div>
            <div v-else class="message-text">{{ msg.content }}</div>
            <div v-if="msg.timestamp" class="message-time">{{ formatMessageTime(msg.timestamp) }}</div>
            <div
              v-if="msg.role === 'assistant' && (msg.template || msg.model)"
              class="agent-meta"
            >
              <span v-if="msg.template" class="agent-tag agent-template">模板: {{ msg.template }}</span>
              <span v-if="msg.model" class="agent-tag agent-model">模型: {{ msg.model }}</span>
            </div>
            <div v-if="msg.citations && msg.citations.length" class="citations">
              <div class="citations-header">
                <span class="citations-title">参考片段</span>
                <button
                  v-if="messageHasExpandableCitations(msg.citations)"
                  type="button"
                  class="citations-toggle"
                  @click="toggleMessageCitations(msg, msgIdx)"
                >
                  {{ messageCitationsAllExpanded(msg, msgIdx) ? '收起全部' : '展开全部' }}
                </button>
              </div>
              <div v-for="(cite, idx) in msg.citations" :key="idx" class="citation-item">
                <div class="citation-meta">
                  <span v-if="cite.source" class="citation-source">来源: {{ cite.source }}</span>
                  <span v-if="cite.chunk_id" class="citation-chunk">Chunk: #{{ cite.chunk_id }}</span>
                </div>
                <div class="citation-scores">
                  <span v-if="cite.rrf_score !== undefined" class="citation-rrf">RRF: {{ cite.rrf_score.toFixed(4) }}</span>
                  <span v-if="cite.vector_rank" class="citation-rank">向量: #{{ cite.vector_rank }}</span>
                  <span v-if="cite.keyword_rank" class="citation-rank">关键词: #{{ cite.keyword_rank }}</span>
                </div>
                <div
                  class="citation-content"
                  :class="{ collapsed: citationHasExpansion(cite) && !citationIsExpanded(msg, msgIdx, idx) }"
                >
                  {{ citationDisplayContent(cite, msg, msgIdx, idx) }}
                </div>
                <div v-if="citationHasExpansion(cite)" class="citation-actions">
                  <button
                    type="button"
                    class="citation-toggle"
                    :aria-expanded="citationIsExpanded(msg, msgIdx, idx)"
                    @click="toggleCitationExpansion(msg, msgIdx, idx)"
                  >
                    {{ citationIsExpanded(msg, msgIdx, idx) ? '收起' : '展开' }}
                  </button>
                </div>
              </div>
            </div>
            <div v-if="msg.trace && msg.trace.length" class="agent-trace">
              <div class="citations-header">
                <span class="citations-title">工具调用</span>
              </div>
              <div v-for="(step, idx) in msg.trace" :key="idx" class="trace-item">
                <div class="trace-meta">
                  <span class="trace-tool">{{ step.tool }}</span>
                  <span v-if="step.name" class="trace-name">{{ step.name }}</span>
                  <span v-if="step.output_ref" class="trace-output">→ {{ step.output_ref }}</span>
                </div>
                <div v-if="step.input" class="trace-input">{{ formatTraceValue(step.input) }}</div>
                <div v-if="step.error" class="trace-error">{{ step.error }}</div>
              </div>
            </div>
          </div>
        </div>
        <div v-if="loading" class="message assistant loading">
          <div class="spinner"></div>
          <span>思考中...</span>
        </div>
      </div>

      <div class="chat-input">
        <div class="chat-input-toolbar">
          <button
            type="button"
            class="mode-toggle"
            :class="{ active: chatMode === 'video_assistant' }"
            :disabled="loading || sessionLoading"
            title="结合摘要、转写和必要检索回答"
            @click="chatMode = 'video_assistant'"
          >
            视频助手
          </button>
          <button
            type="button"
            class="mode-toggle"
            :class="{ active: chatMode === 'strict_rag' }"
            :disabled="loading || sessionLoading"
            title="只基于检索到的视频片段回答"
            @click="chatMode = 'strict_rag'"
          >
            严格引用
          </button>
          <button
            type="button"
            class="mode-toggle"
            :class="{ active: chatMode === 'agent' }"
            :disabled="loading || sessionLoading"
            title="Agentic QA：工具调用链 + 引用 + 模板（非流式）"
            @click="chatMode = 'agent'"
          >
            Agentic QA
          </button>
        </div>
        <div v-if="strictModeBlocked" class="chat-mode-warning">
          {{ strictModeBlockedText }}
        </div>
        <div class="chat-input-row">
          <input
            v-model="question"
            @keyup.enter="sendQuestion"
            :placeholder="chatInputPlaceholder"
            :disabled="loading || sessionLoading || strictModeBlocked"
            class="input-field"
            aria-label="输入问题"
          />
          <button @click="sendQuestion" :disabled="sendDisabled" class="btn-send">
            发送
          </button>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, onUnmounted, nextTick } from 'vue'
import { marked } from 'marked'
import DOMPurify from 'dompurify'
import api from '../api'
import { normalizeChatMessages, resolveReusableChatSession } from '../chatHistoryPolicy.js'
import {
  DEFAULT_CITATION_PREVIEW_OPTIONS,
  areAllExpandableCitationsExpanded,
  citationExpansionKey,
  citationNeedsExpansion,
  citationTextForDisplay,
  setMessageCitationsExpanded,
} from '../citationDisplayPolicy.js'

const props = defineProps({
  task: Object
})

const emit = defineEmits(['error'])

const indexStatus = ref({ status: 'not_indexed', chunks: 0, error: '' })
const building = ref(false)
const messages = ref([])
const question = ref('')
const loading = ref(false)
const sessionLoading = ref(false)
const chatMode = ref('video_assistant')
const sessionId = ref(null)
const messagesContainer = ref(null)
const expandedCitationKeys = ref(new Set())
let indexStatusTimer = null

const citationPreviewOptions = DEFAULT_CITATION_PREVIEW_OPTIONS

const indexIcon = computed(() => {
  switch (indexStatus.value.status) {
    case 'indexing': return '⏳'
    case 'failed': return '❌'
    case 'indexed': return '✅'
    default: return '🔍'
  }
})

const indexPromptText = computed(() => {
  switch (indexStatus.value.status) {
    case 'indexing': return '正在构建视频索引；视频助手仍可先基于摘要和转写回答'
    case 'failed': return '索引构建失败；严格引用不可用，视频助手仍可基于摘要和转写回答'
    case 'not_indexed': return '严格引用需要先构建视频索引；视频助手可先基于摘要和转写回答'
    default: return '严格引用需要先构建视频索引；视频助手可先基于摘要和转写回答'
  }
})

const indexButtonText = computed(() => {
  return indexStatus.value.status === 'failed' ? '重新构建索引' : '构建视频索引'
})

const strictModeBlocked = computed(() => {
  return (chatMode.value === 'strict_rag' || chatMode.value === 'agent') && indexStatus.value.status !== 'indexed'
})

const strictModeBlockedText = computed(() => {
  return chatMode.value === 'agent'
    ? 'Agentic QA 依赖视频索引，请先构建索引。'
    : '严格引用模式依赖视频索引，请先构建索引。'
})

const chatInputPlaceholder = computed(() => {
  if (chatMode.value === 'strict_rag') return '基于引用片段提问...'
  if (chatMode.value === 'agent') return '让 Agentic QA 分析这个视频...'
  return '问问这个视频...'
})

const sendDisabled = computed(() => loading.value || sessionLoading.value || !question.value || strictModeBlocked.value)

const formatMessageTime = (timestamp) => {
  if (!timestamp) return ''
  const now = new Date()
  const diff = now - new Date(timestamp)
  const seconds = Math.floor(diff / 1000)
  if (seconds < 60) return '刚刚'
  const minutes = Math.floor(seconds / 60)
  if (minutes < 60) return `${minutes}分钟前`
  const hours = Math.floor(minutes / 60)
  if (hours < 24) return `${hours}小时前`
  const days = Math.floor(hours / 24)
  return `${days}天前`
}

const renderMarkdown = (content) => DOMPurify.sanitize(marked.parse(content || ''))

const messageExpansionId = (message, messageIndex) => message?.id ?? `message-${messageIndex}`

const citationHasExpansion = (citation) => citationNeedsExpansion(citation?.content, citationPreviewOptions)

const citationIsExpanded = (message, messageIndex, citationIndex) => {
  const key = citationExpansionKey(messageExpansionId(message, messageIndex), citationIndex)
  return expandedCitationKeys.value.has(key)
}

const citationDisplayContent = (citation, message, messageIndex, citationIndex) => {
  return citationTextForDisplay(
    citation?.content,
    citationIsExpanded(message, messageIndex, citationIndex),
    citationPreviewOptions,
  )
}

const messageHasExpandableCitations = (citations) => {
  return Array.isArray(citations) && citations.some((citation) => citationHasExpansion(citation))
}

const messageCitationsAllExpanded = (message, messageIndex) => {
  return areAllExpandableCitationsExpanded(
    expandedCitationKeys.value,
    messageExpansionId(message, messageIndex),
    message?.citations,
    citationPreviewOptions,
  )
}

const toggleCitationExpansion = (message, messageIndex, citationIndex) => {
  const key = citationExpansionKey(messageExpansionId(message, messageIndex), citationIndex)
  const nextKeys = new Set(expandedCitationKeys.value)
  if (nextKeys.has(key)) {
    nextKeys.delete(key)
  } else {
    nextKeys.add(key)
  }
  expandedCitationKeys.value = nextKeys
}

const toggleMessageCitations = (message, messageIndex) => {
  const messageId = messageExpansionId(message, messageIndex)
  const expand = !areAllExpandableCitationsExpanded(
    expandedCitationKeys.value,
    messageId,
    message?.citations,
    citationPreviewOptions,
  )
  expandedCitationKeys.value = setMessageCitationsExpanded(
    expandedCitationKeys.value,
    messageId,
    message?.citations,
    expand,
    citationPreviewOptions,
  )
}

const buildIndex = async () => {
  building.value = true
  indexStatus.value.status = 'indexing'
  try {
    const res = await api.buildRAGIndex(props.task.id)
    indexStatus.value = { status: 'indexed', chunks: res.chunks || 0, error: '' }
    await ensureChatSession()
  } catch (err) {
    indexStatus.value = { status: 'failed', chunks: 0, error: err.message || '构建索引失败' }
    emit('error', err.message || '构建索引失败')
  } finally {
    building.value = false
  }
}

const createSession = async () => {
  try {
    const res = await api.createChatSession(props.task.id, '会话')
    sessionId.value = res.id
    return res
  } catch (err) {
    console.error('创建会话失败:', err)
    return null
  }
}

const scrollMessagesToBottom = () => {
  nextTick(() => {
    messagesContainer.value?.scrollTo({ top: messagesContainer.value.scrollHeight, behavior: 'smooth' })
  })
}

const ensureChatSession = async () => {
  if (sessionId.value || sessionLoading.value) return
  sessionLoading.value = true
  try {
    const sessions = await api.getChatSessions(props.task.id)
    const reusable = await resolveReusableChatSession(sessions, (id) => api.getChatMessages(id))
    if (reusable.session) {
      sessionId.value = reusable.session.id
      messages.value = normalizeChatMessages(reusable.messages)
      scrollMessagesToBottom()
      return
    }
    await createSession()
  } catch (err) {
    console.error('加载会话失败:', err)
    if (!sessionId.value) {
      await createSession()
    }
  } finally {
    sessionLoading.value = false
  }
}

const formatTraceValue = (value) => {
  if (value == null) return ''
  if (typeof value === 'string') return value
  try {
    return JSON.stringify(value)
  } catch {
    return String(value)
  }
}

const sendQuestion = async () => {
  if (!question.value || loading.value || sessionLoading.value) return
  if (!sessionId.value) {
    await ensureChatSession()
  }
  if (!sessionId.value) {
    emit('error', '会话初始化失败')
    return
  }

  const userMessage = { id: Date.now(), role: 'user', content: question.value, timestamp: new Date() }
  messages.value.push(userMessage)
  const q = question.value
  question.value = ''

  loading.value = true

  if (chatMode.value === 'agent') {
    await sendAgentQuestion(q)
  } else {
    await sendStreamQuestion(q, chatMode.value)
  }
}

// 视频助手 / 严格引用：SSE 流式，边收边渲染
const sendStreamQuestion = async (q, mode) => {
  // 预插入一个空的 assistant 消息
  const assistantMsg = {
    id: `temp-${Date.now()}`,
    role: 'assistant',
    content: '',
    citations: [],
    timestamp: new Date(),
    template: null,
    trace: [],
    model: null,
    mode: 'rag',
  }
  messages.value.push(assistantMsg)

  try {
    await api.sendChatMessageStream(sessionId.value, q, 5, mode, (event) => {
      if (event.type === 'answer') {
        assistantMsg.content += event.delta || ''
        scrollMessagesToBottom()
      } else if (event.type === 'citations') {
        assistantMsg.citations = event.citations || []
      } else if (event.type === 'done') {
        assistantMsg.id = event.message_id || assistantMsg.id
        loading.value = false
      } else if (event.type === 'error') {
        emit('error', event.message || '回答失败')
        loading.value = false
      }
    })
  } catch (err) {
    emit('error', err.message || '发送失败')
    loading.value = false
  }
}

// Agentic QA：非流式，等待接口返回后一次性插入 assistant 消息
const sendAgentQuestion = async (q) => {
  try {
    const res = await api.sendAgentMessage(sessionId.value, q, 5)
    messages.value.push({
      id: res.message_id || `temp-${Date.now()}`,
      role: 'assistant',
      content: res.answer || '',
      citations: Array.isArray(res.citations) ? res.citations : [],
      timestamp: new Date(),
      template: res.template || null,
      trace: Array.isArray(res.trace) ? res.trace : [],
      model: res.model || null,
      mode: 'agent',
    })
    scrollMessagesToBottom()
  } catch (err) {
    emit('error', err.message || 'Agentic 问答失败')
  } finally {
    loading.value = false
  }
}

const checkIndexStatus = async () => {
  try {
    const res = await api.getRAGIndexStatus(props.task.id)
    // 后端返回 { indexed: boolean, status: string, chunks: number, last_error: string }
    if (res.indexed) {
      indexStatus.value = { status: 'indexed', chunks: res.chunks || 0, error: '' }
      stopIndexPolling()
      if (!sessionId.value) {
        await ensureChatSession()
      }
    } else if (res.status === 'indexing') {
      indexStatus.value = { status: 'indexing', chunks: 0, error: '' }
      startIndexPolling()
    } else if (res.status === 'failed') {
      indexStatus.value = { status: 'failed', chunks: 0, error: res.last_error || '构建失败' }
      stopIndexPolling()
    } else {
      indexStatus.value = { status: 'not_indexed', chunks: 0, error: '' }
      stopIndexPolling()
    }
  } catch (err) {
    console.error('检查索引状态失败:', err)
    indexStatus.value = { status: 'not_indexed', chunks: 0, error: '' }
    stopIndexPolling()
  }
}

const startIndexPolling = () => {
  if (indexStatusTimer) return
  indexStatusTimer = setInterval(() => {
    checkIndexStatus()
  }, 2500)
}

const stopIndexPolling = () => {
  if (indexStatusTimer) {
    clearInterval(indexStatusTimer)
    indexStatusTimer = null
  }
}

onMounted(() => {
  checkIndexStatus()
  ensureChatSession()
})

onUnmounted(() => {
  stopIndexPolling()
})
</script>

<style scoped>
.rag-chat {
  height: 100%;
  display: flex;
  flex-direction: column;
}

.index-prompt {
  display: flex;
  align-items: center;
  gap: 0.85rem;
  flex-wrap: wrap;
  text-align: left;
  padding: 0.9rem 1.25rem;
  border-bottom: 1px solid rgba(139, 149, 168, 0.14);
  background: rgba(10, 14, 26, 0.42);
}

.prompt-icon {
  font-size: 1.35rem;
  line-height: 1;
}

.index-prompt p {
  color: #8b95a8;
  margin: 0;
  font-size: 0.9rem;
  flex: 1;
  min-width: 220px;
}

.indexing-spinner {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 0.75rem;
  color: #d4af37;
  font-size: 0.95rem;
  margin: 0;
}

.index-error {
  margin-top: 1.5rem;
  padding: 1rem;
  background: linear-gradient(135deg, rgba(239, 68, 68, 0.12), rgba(220, 38, 38, 0.08));
  border: 1px solid rgba(239, 68, 68, 0.3);
  border-radius: 0.75rem;
  display: flex;
  flex-direction: column;
  gap: 0.5rem;
}

.error-label {
  color: #f87171;
  font-weight: 600;
  font-size: 0.85rem;
}

.error-text {
  color: #fecaca;
  font-size: 0.9rem;
  font-family: 'JetBrains Mono', monospace;
  word-break: break-word;
}

.index-info {
  margin-top: 1rem;
  color: #4ade80;
  font-size: 0.9rem;
}

.chat-container {
  display: flex;
  flex-direction: column;
  height: 100%;
}

.chat-messages {
  flex: 1;
  overflow-y: auto;
  padding: 1.5rem;
  display: flex;
  flex-direction: column;
  gap: 1.5rem;
  scrollbar-width: thin;
  scrollbar-color: rgba(212, 175, 55, 0.3) transparent;
}

.chat-messages::-webkit-scrollbar { width: 8px; }
.chat-messages::-webkit-scrollbar-thumb { background: rgba(212, 175, 55, 0.3); border-radius: 4px; }

.message {
  display: flex;
  animation: vl-message-in 0.3s ease-out;
}

.message.user {
  justify-content: flex-end;
}

.message.user .message-content {
  background: linear-gradient(135deg, rgba(212, 175, 55, 0.15), rgba(41, 98, 255, 0.12));
  border: 1px solid rgba(212, 175, 55, 0.3);
  color: #e8eef7;
  max-width: 70%;
  padding: 1rem 1.25rem;
  border-radius: 1rem 1rem 0.25rem 1rem;
}

.message.assistant {
  justify-content: flex-start;
}

.message.assistant .message-content {
  background: linear-gradient(135deg, rgba(15, 25, 45, 0.6), rgba(20, 30, 50, 0.4));
  border: 1px solid rgba(139, 149, 168, 0.2);
  color: #b8c5db;
  max-width: 80%;
  padding: 1rem 1.25rem;
  border-radius: 1rem 1rem 1rem 0.25rem;
}

.message.loading {
  gap: 0.75rem;
  align-items: center;
  padding: 1rem 1.25rem;
}

.message-text {
  line-height: 1.7;
  white-space: pre-wrap;
  word-wrap: break-word;
}

.message-time {
  font-size: 0.75rem;
  color: #8b95a8;
  margin-top: 0.5rem;
  font-family: 'JetBrains Mono', monospace;
  opacity: 0.7;
}

/* RAG Chat 内的 Markdown 渲染 */
.message-text.markdown-body :deep(p) {
  margin-bottom: 0.5rem;
}

.message-text.markdown-body :deep(strong) {
  color: #f4e4a6;
}

.message-text.markdown-body :deep(ul),
.message-text.markdown-body :deep(ol) {
  padding-left: 1.5rem;
  margin-bottom: 0.5rem;
}

.message-text.markdown-body :deep(li) {
  margin-bottom: 0.3rem;
}

.message-text.markdown-body :deep(li::marker) {
  color: #d4af37;
}

.message-text.markdown-body :deep(code) {
  background: rgba(212, 175, 55, 0.1);
  padding: 0.1rem 0.4rem;
  border-radius: 0.25rem;
  font-family: 'JetBrains Mono', monospace;
  font-size: 0.85rem;
  color: #f4e4a6;
}

.citations {
  margin-top: 1rem;
  padding-top: 1rem;
  border-top: 1px solid rgba(139, 149, 168, 0.15);
}

.citations-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 0.75rem;
  font-size: 0.85rem;
  margin-bottom: 0.75rem;
  font-weight: 600;
}

.citations-title {
  color: #a8b3c7;
}

.citations-toggle,
.citation-toggle {
  background: transparent;
  border: 1px solid rgba(139, 149, 168, 0.2);
  color: #d4af37;
  cursor: pointer;
  font-size: 0.75rem;
  line-height: 1;
  padding: 0.35rem 0.55rem;
  border-radius: 0.35rem;
  transition: all 0.2s;
}

.citations-toggle:hover,
.citation-toggle:hover {
  background: rgba(212, 175, 55, 0.08);
  border-color: rgba(212, 175, 55, 0.3);
}

.citation-item {
  background: rgba(10, 14, 26, 0.35);
  padding: 0.7rem 0.75rem;
  border-radius: 0.5rem;
  margin-bottom: 0.5rem;
  border: 1px solid rgba(139, 149, 168, 0.1);
}

.citation-meta {
  display: flex;
  gap: 0.75rem;
  margin-bottom: 0.5rem;
  flex-wrap: wrap;
}

.citation-source {
  font-size: 0.75rem;
  color: #5b8fff;
  font-family: 'JetBrains Mono', monospace;
  background: rgba(41, 98, 255, 0.1);
  padding: 0.2rem 0.5rem;
  border-radius: 0.35rem;
  border: 1px solid rgba(41, 98, 255, 0.2);
}

.citation-chunk {
  font-size: 0.75rem;
  color: #8b95a8;
  font-family: 'JetBrains Mono', monospace;
}

.citation-scores {
  display: flex;
  gap: 0.75rem;
  margin-bottom: 0.5rem;
  flex-wrap: wrap;
  font-size: 0.75rem;
  font-family: 'JetBrains Mono', monospace;
  opacity: 0.75;
}

.citation-rrf {
  color: #d4af37;
  background: rgba(212, 175, 55, 0.1);
  padding: 0.2rem 0.5rem;
  border-radius: 0.35rem;
  border: 1px solid rgba(212, 175, 55, 0.2);
}

.citation-rank {
  color: #8b95a8;
  background: rgba(139, 149, 168, 0.1);
  padding: 0.2rem 0.5rem;
  border-radius: 0.35rem;
  border: 1px solid rgba(139, 149, 168, 0.15);
}

.citation-content {
  font-size: 0.85rem;
  color: #8b95a8;
  line-height: 1.6;
  white-space: pre-wrap;
  word-break: break-word;
}

.citation-content.collapsed {
  color: #9aa6ba;
}

.citation-actions {
  display: flex;
  justify-content: flex-end;
  margin-top: 0.5rem;
}

.chat-input {
  padding: 1.5rem;
  border-top: 1px solid rgba(212, 175, 55, 0.15);
  display: flex;
  flex-direction: column;
  gap: 1rem;
  background: linear-gradient(135deg, rgba(10, 14, 26, 0.5), rgba(15, 25, 45, 0.4));
}

.input-field {
  flex: 1;
  background: rgba(10, 14, 26, 0.6);
  border: 1px solid rgba(139, 149, 168, 0.2);
  padding: 0.85rem 1.25rem;
  border-radius: 0.875rem;
  color: #e8eef7;
  outline: none;
  transition: all 0.3s;
  font-size: 0.95rem;
}

.input-field:focus {
  border-color: #d4af37;
  box-shadow: 0 0 0 3px rgba(212, 175, 55, 0.15);
}

.btn-send {
  background: linear-gradient(135deg, #d4af37, #f4e4a6);
  color: #0a0e1a;
  border: none;
  padding: 0.85rem 2rem;
  border-radius: 0.875rem;
  font-weight: 600;
  cursor: pointer;
  transition: all 0.3s;
  font-size: 0.95rem;
}

.btn-send:hover:not(:disabled) {
  transform: translateY(-2px);
  box-shadow: 0 6px 24px rgba(212, 175, 55, 0.4);
}

.btn-send:disabled {
  opacity: 0.4;
  cursor: not-allowed;
}

.btn-amber {
  background: linear-gradient(135deg, #d4af37, #f4e4a6);
  color: #0a0e1a;
  border: none;
  padding: 0.75rem 1.75rem;
  border-radius: 0.75rem;
  font-weight: 600;
  cursor: pointer;
  transition: all 0.3s;
  font-size: 0.95rem;
}

.btn-amber:hover:not(:disabled) {
  transform: translateY(-2px);
  box-shadow: 0 6px 24px rgba(212, 175, 55, 0.4);
}

.btn-amber:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

.spinner {
  width: 1.25rem;
  height: 1.25rem;
  border: 2.5px solid rgba(212, 175, 55, 0.15);
  border-top-color: #d4af37;
  border-radius: 50%;
  animation: vl-spin 0.8s linear infinite;
}

.chat-input-toolbar {
  display: flex;
  justify-content: flex-end;
  gap: 0.5rem;
  flex-wrap: wrap;
}

.chat-mode-warning {
  color: #f4c46b;
  font-size: 0.8rem;
  line-height: 1.4;
}

.chat-input-row {
  display: flex;
  gap: 1rem;
}

.mode-toggle {
  background: transparent;
  border: 1px solid rgba(139, 149, 168, 0.2);
  color: #8b95a8;
  cursor: pointer;
  font-size: 0.75rem;
  padding: 0.35rem 0.7rem;
  border-radius: 0.5rem;
  transition: all 0.2s;
  font-family: 'JetBrains Mono', monospace;
}

.mode-toggle:hover:not(:disabled) {
  border-color: rgba(212, 175, 55, 0.3);
  color: #d4af37;
}

.mode-toggle.active {
  background: rgba(212, 175, 55, 0.12);
  border-color: rgba(212, 175, 55, 0.4);
  color: #d4af37;
}

.mode-toggle:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

.agent-meta {
  display: flex;
  flex-wrap: wrap;
  gap: 0.5rem;
  margin-top: 0.5rem;
}

.agent-tag {
  font-size: 0.7rem;
  font-family: 'JetBrains Mono', monospace;
  padding: 0.2rem 0.5rem;
  border-radius: 0.35rem;
  border: 1px solid rgba(139, 149, 168, 0.2);
  background: rgba(139, 149, 168, 0.08);
  color: #a8b3c7;
}

.agent-template {
  color: #d4af37;
  border-color: rgba(212, 175, 55, 0.3);
  background: rgba(212, 175, 55, 0.1);
}

.agent-model {
  color: #8b95a8;
}

.agent-trace {
  margin-top: 1rem;
  padding-top: 1rem;
  border-top: 1px solid rgba(139, 149, 168, 0.15);
}

.trace-item {
  background: rgba(10, 14, 26, 0.35);
  padding: 0.55rem 0.75rem;
  border-radius: 0.5rem;
  margin-bottom: 0.5rem;
  border: 1px solid rgba(139, 149, 168, 0.1);
}

.trace-meta {
  display: flex;
  flex-wrap: wrap;
  gap: 0.5rem;
  align-items: center;
  font-size: 0.75rem;
  font-family: 'JetBrains Mono', monospace;
}

.trace-tool {
  color: #5b8fff;
  background: rgba(41, 98, 255, 0.1);
  padding: 0.15rem 0.45rem;
  border-radius: 0.35rem;
  border: 1px solid rgba(41, 98, 255, 0.2);
}

.trace-name {
  color: #a8b3c7;
}

.trace-output {
  color: #d4af37;
  opacity: 0.85;
}

.trace-input {
  margin-top: 0.4rem;
  font-size: 0.72rem;
  color: #8b95a8;
  font-family: 'JetBrains Mono', monospace;
  white-space: pre-wrap;
  word-break: break-word;
  opacity: 0.8;
}

.trace-error {
  margin-top: 0.4rem;
  font-size: 0.72rem;
  color: #f87171;
  font-family: 'JetBrains Mono', monospace;
}
</style>
