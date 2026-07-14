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
      <div v-if="sessions.length" class="chat-sessions-bar">
        <select class="session-select" :value="sessionId" @change="onSessionSelectChange" aria-label="切换对话">
          <option v-for="s in sessions" :key="s.id" :value="s.id">{{ sessionLabel(s) }}</option>
        </select>
        <button type="button" class="session-btn" :disabled="sessionLoading" @click="newSession" title="新建对话">＋ 新对话</button>
        <button type="button" class="session-btn danger" :disabled="!sessionId" @click="deleteCurrentSession" title="删除当前对话">🗑</button>
      </div>
      <div class="chat-messages" ref="messagesContainer" @scroll="onMessagesScroll">
        <div v-for="(msg, msgIdx) in messages" :key="msg.id || msgIdx" class="message" :class="msg.role">
          <div class="message-content">
            <button
              v-if="msg.role === 'assistant' && msg.content"
              type="button"
              class="message-copy"
              :class="{ copied: copiedMessageId === msg.id }"
              :title="copiedMessageId === msg.id ? '已复制' : '复制回答'"
              @click="copyMessage(msg)"
            >
              {{ copiedMessageId === msg.id ? '✓' : '⧉' }}
            </button>
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
          <textarea
            ref="questionInput"
            v-model="question"
            @keydown.enter.exact.prevent="sendQuestion"
            @input="autoResizeTextarea"
            :placeholder="chatInputPlaceholder"
            :disabled="loading || sessionLoading || strictModeBlocked"
            class="input-field"
            rows="1"
            aria-label="输入问题"
          ></textarea>
          <button v-if="streaming" @click="stopStreaming" class="btn-send stop">
            停止
          </button>
          <button v-else @click="sendQuestion" :disabled="sendDisabled" class="btn-send">
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
import { normalizeChatMessages } from '../chatHistoryPolicy.js'
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
const sessions = ref([])
const question = ref('')
const loading = ref(false)
const sessionLoading = ref(false)
const chatMode = ref('video_assistant')
const sessionId = ref(null)
const messagesContainer = ref(null)
const questionInput = ref(null)
const expandedCitationKeys = ref(new Set())
const streaming = ref(false)
const userAtBottom = ref(true)
const copiedMessageId = ref(null)
let abortController = null
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
    const res = await api.createChatSession(props.task.id, '')
    sessions.value = [res, ...sessions.value]
    sessionId.value = res.id
    messages.value = []
    return res
  } catch (err) {
    console.error('创建会话失败:', err)
    return null
  }
}

const loadMessages = async (id) => {
  try {
    messages.value = normalizeChatMessages(await api.getChatMessages(id))
  } catch (err) {
    console.error('加载会话消息失败:', err)
    messages.value = []
  }
  scrollMessagesToBottom(true)
}

const refreshSessions = async () => {
  try {
    sessions.value = (await api.getChatSessions(props.task.id)) || []
  } catch (err) {
    sessions.value = []
  }
}

const sessionLabel = (s) => s.title || `会话 #${s.id}`

const selectSession = async (id) => {
  if (!id) return
  if (sessionId.value === id) return
  sessionId.value = id
  await loadMessages(id)
}

const onSessionSelectChange = (e) => selectSession(Number(e.target.value))

const newSession = async () => {
  if (sessionLoading.value) return
  sessionLoading.value = true
  try {
    await createSession()
  } finally {
    sessionLoading.value = false
  }
}

const deleteCurrentSession = async () => {
  if (!sessionId.value || sessionLoading.value) return
  if (!window.confirm('确定删除当前对话？此操作不可恢复。')) return
  sessionLoading.value = true
  try {
    await api.deleteChatSession(sessionId.value)
    sessions.value = sessions.value.filter((s) => s.id !== sessionId.value)
    sessionId.value = null
    if (sessions.value.length > 0) {
      await selectSession(sessions.value[0].id)
    } else {
      await createSession()
    }
  } catch (err) {
    emit('error', err.message || '删除会话失败')
  } finally {
    sessionLoading.value = false
  }
}

const scrollMessagesToBottom = (force = false) => {
  // 用户滚到上方查看历史时，不要把视口拽回底部打断阅读
  if (!force && !userAtBottom.value) return
  nextTick(() => {
    messagesContainer.value?.scrollTo({ top: messagesContainer.value.scrollHeight, behavior: 'smooth' })
  })
}

const onMessagesScroll = () => {
  const el = messagesContainer.value
  if (!el) return
  const threshold = 80
  userAtBottom.value = el.scrollTop + el.clientHeight >= el.scrollHeight - threshold
}

const stopStreaming = () => {
  abortController?.abort()
}

const copyMessage = async (message) => {
  try {
    await navigator.clipboard.writeText(message.content || '')
    copiedMessageId.value = message.id
    setTimeout(() => {
      if (copiedMessageId.value === message.id) copiedMessageId.value = null
    }, 1500)
  } catch (err) {
    emit('error', '复制失败')
  }
}

const autoResizeTextarea = () => {
  const el = questionInput.value
  if (!el) return
  el.style.height = 'auto'
  el.style.height = Math.min(el.scrollHeight, 160) + 'px'
}

const ensureChatSession = async () => {
  if (sessionId.value || sessionLoading.value) return
  sessionLoading.value = true
  try {
    await refreshSessions()
    if (sessions.value.length > 0) {
      await selectSession(sessions.value[0].id)
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
  nextTick(() => {
    if (questionInput.value) {
      questionInput.value.style.height = 'auto'
    }
  })

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
  userAtBottom.value = true
  streaming.value = true
  abortController = new AbortController()

  const finish = () => {
    streaming.value = false
    loading.value = false
    abortController = null
  }
  const failStream = (reason) => {
    // 流式失败时若气泡还空着，回填错误说明，避免留一条空气泡
    if (!assistantMsg.content) {
      assistantMsg.content = `⚠️ ${reason}`
    }
    emit('error', reason)
    finish()
  }

  try {
    await api.sendChatMessageStream(sessionId.value, q, 5, mode, (event) => {
      if (event.type === 'answer') {
        assistantMsg.content += event.delta || ''
        scrollMessagesToBottom()
      } else if (event.type === 'citations') {
        assistantMsg.citations = event.citations || []
      } else if (event.type === 'done') {
        assistantMsg.id = event.message_id || assistantMsg.id
        finish()
      } else if (event.type === 'error') {
        failStream(event.message || '回答失败')
      }
    }, abortController.signal)
  } catch (err) {
    if (err?.name === 'AbortError') {
      // 用户主动停止：保留已生成内容，不报错
      finish()
    } else {
      failStream(err.message || '发送失败')
    }
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
  // 离开对话页时中止进行中的流，避免回调写已卸载组件
  abortController?.abort()
  abortController = null
  streaming.value = false
  loading.value = false
})
</script>

<style scoped>
.rag-chat {
  height: 100%;
  min-height: 0;
  display: flex;
  flex-direction: column;
}

.index-prompt {
  display: flex;
  align-items: center;
  gap: 0.85rem;
  flex-wrap: wrap;
  text-align: left;
  padding: 0.85rem 1.25rem;
  border-bottom: 1px solid var(--vl-border);
  background:
    linear-gradient(90deg, rgba(45, 212, 191, 0.06), transparent 55%),
    rgba(7, 9, 15, 0.5);
}

.prompt-icon {
  font-size: 1.2rem;
  line-height: 1;
  width: 2rem;
  height: 2rem;
  border-radius: 0.55rem;
  display: grid;
  place-items: center;
  background: rgba(255, 255, 255, 0.04);
  border: 1px solid var(--vl-border);
  flex-shrink: 0;
}

.index-prompt p {
  color: var(--vl-text-secondary);
  margin: 0;
  font-size: 0.88rem;
  line-height: 1.45;
  flex: 1;
  min-width: 200px;
}

.indexing-spinner {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 0.75rem;
  color: var(--vl-primary);
  font-size: 0.9rem;
  margin: 0;
}

.index-error {
  margin: 0;
  flex: 1 1 100%;
  padding: 0.65rem 0.85rem;
  background: linear-gradient(135deg, rgba(239, 68, 68, 0.12), rgba(220, 38, 38, 0.08));
  border: 1px solid rgba(239, 68, 68, 0.3);
  border-radius: 0.65rem;
  display: flex;
  flex-direction: column;
  gap: 0.35rem;
}

.error-label {
  color: var(--vl-danger);
  font-weight: 600;
  font-size: 0.85rem;
}

.error-text {
  color: #fecaca;
  font-size: 0.9rem;
  font-family: var(--vl-font-mono);
  word-break: break-word;
}

.index-info {
  margin-top: 1rem;
  color: var(--vl-success);
  font-size: 0.9rem;
}

.chat-container {
  display: flex;
  flex-direction: column;
  height: 100%;
  min-height: 0;
}

.chat-sessions-bar {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  padding: 0.65rem 1.25rem;
  border-bottom: 1px solid var(--vl-border);
  background: rgba(7, 9, 15, 0.48);
}

.session-select {
  flex: 1;
  min-width: 0;
  background: rgba(7, 9, 15, 0.65);
  border: 1px solid var(--vl-border);
  border-radius: var(--vl-radius-sm);
  padding: 0.45rem 0.7rem;
  color: var(--vl-text);
  font-size: 0.82rem;
  outline: none;
  cursor: pointer;
  transition: border-color 0.2s, box-shadow 0.2s;
}

.session-select:focus {
  border-color: var(--vl-border-focus);
  box-shadow: 0 0 0 3px var(--vl-primary-dim);
}

.session-btn {
  background: transparent;
  border: 1px solid var(--vl-border);
  color: var(--vl-text-secondary);
  padding: 0.4rem 0.7rem;
  border-radius: var(--vl-radius-sm);
  cursor: pointer;
  font-size: 0.75rem;
  font-family: var(--vl-font-mono);
  transition: border-color 0.2s, color 0.2s, background 0.2s;
  white-space: nowrap;
}

.session-btn:hover:not(:disabled) {
  border-color: rgba(45, 212, 191, 0.4);
  color: var(--vl-primary);
  background: var(--vl-primary-dim);
}

.session-btn.danger:hover:not(:disabled) {
  border-color: rgba(239, 68, 68, 0.5);
  color: var(--vl-danger);
  background: var(--vl-danger-dim);
}

.session-btn:disabled {
  opacity: 0.4;
  cursor: not-allowed;
}

.chat-messages {
  flex: 1;
  overflow-y: auto;
  padding: 1.35rem 1.5rem 1.5rem;
  display: flex;
  flex-direction: column;
  gap: 1.25rem;
  scrollbar-width: thin;
  scrollbar-color: rgba(45, 212, 191, 0.3) transparent;
  background:
    radial-gradient(ellipse 50% 30% at 50% 0%, rgba(45, 212, 191, 0.04), transparent 70%);
}

.chat-messages::-webkit-scrollbar { width: 8px; }
.chat-messages::-webkit-scrollbar-thumb { background: rgba(45, 212, 191, 0.3); border-radius: 4px; }

.message {
  display: flex;
  animation: vl-message-in 0.3s ease-out;
}

.message.user {
  justify-content: flex-end;
}

.message.user .message-content {
  background: linear-gradient(145deg, rgba(45, 212, 191, 0.18), rgba(96, 165, 250, 0.1));
  border: 1px solid rgba(45, 212, 191, 0.32);
  color: var(--vl-text);
  max-width: min(72%, 40rem);
  padding: 0.95rem 1.15rem;
  border-radius: 1.05rem 1.05rem 0.3rem 1.05rem;
  box-shadow: 0 4px 18px rgba(0, 0, 0, 0.12);
}

.message.assistant {
  justify-content: flex-start;
}

.message.assistant .message-content {
  position: relative;
  background: linear-gradient(160deg, rgba(16, 22, 34, 0.72), rgba(12, 16, 24, 0.55));
  border: 1px solid var(--vl-border-strong);
  color: #c5d0e0;
  max-width: min(84%, 48rem);
  padding: 0.95rem 1.15rem;
  border-radius: 1.05rem 1.05rem 1.05rem 0.3rem;
  box-shadow: 0 4px 18px rgba(0, 0, 0, 0.14);
}

.message-copy {
  position: absolute;
  top: 0.4rem;
  right: 0.4rem;
  opacity: 0;
  background: rgba(7, 9, 15, 0.75);
  border: 1px solid rgba(139, 149, 168, 0.25);
  color: var(--vl-text-secondary);
  width: 1.7rem;
  height: 1.7rem;
  border-radius: 0.4rem;
  cursor: pointer;
  font-size: 0.85rem;
  display: flex;
  align-items: center;
  justify-content: center;
  transition: all 0.2s;
}

.message-content:hover .message-copy {
  opacity: 1;
}

.message-copy:hover {
  color: var(--vl-primary);
  border-color: rgba(45, 212, 191, 0.4);
}

.message-copy.copied {
  color: var(--vl-success);
  opacity: 1;
  border-color: rgba(74, 222, 128, 0.4);
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
  color: var(--vl-text-secondary);
  margin-top: 0.5rem;
  font-family: var(--vl-font-mono);
  opacity: 0.7;
}

/* RAG Chat 内的 Markdown 渲染 */
.message-text.markdown-body :deep(p) {
  margin-bottom: 0.5rem;
}

.message-text.markdown-body :deep(strong) {
  color: var(--vl-primary);
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
  color: var(--vl-primary);
}

.message-text.markdown-body :deep(code) {
  background: rgba(45, 212, 191, 0.1);
  padding: 0.1rem 0.4rem;
  border-radius: 0.25rem;
  font-family: var(--vl-font-mono);
  font-size: 0.85rem;
  color: var(--vl-primary);
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
  color: var(--vl-primary);
  cursor: pointer;
  font-size: 0.75rem;
  line-height: 1;
  padding: 0.35rem 0.55rem;
  border-radius: 0.35rem;
  transition: all 0.2s;
}

.citations-toggle:hover,
.citation-toggle:hover {
  background: rgba(45, 212, 191, 0.08);
  border-color: rgba(45, 212, 191, 0.3);
}

.citation-item {
  background: rgba(7, 9, 15, 0.35);
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
  color: var(--vl-info);
  font-family: var(--vl-font-mono);
  background: rgba(96, 165, 250, 0.1);
  padding: 0.2rem 0.5rem;
  border-radius: 0.35rem;
  border: 1px solid rgba(96, 165, 250, 0.2);
}

.citation-chunk {
  font-size: 0.75rem;
  color: var(--vl-text-secondary);
  font-family: var(--vl-font-mono);
}

.citation-scores {
  display: flex;
  gap: 0.75rem;
  margin-bottom: 0.5rem;
  flex-wrap: wrap;
  font-size: 0.75rem;
  font-family: var(--vl-font-mono);
  opacity: 0.75;
}

.citation-rrf {
  color: var(--vl-primary);
  background: rgba(45, 212, 191, 0.1);
  padding: 0.2rem 0.5rem;
  border-radius: 0.35rem;
  border: 1px solid rgba(45, 212, 191, 0.2);
}

.citation-rank {
  color: var(--vl-text-secondary);
  background: rgba(139, 149, 168, 0.1);
  padding: 0.2rem 0.5rem;
  border-radius: 0.35rem;
  border: 1px solid rgba(139, 149, 168, 0.15);
}

.citation-content {
  font-size: 0.85rem;
  color: var(--vl-text-secondary);
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
  padding: 0.95rem 1.25rem 1.15rem;
  border-top: 1px solid var(--vl-border);
  display: flex;
  flex-direction: column;
  gap: 0.7rem;
  background:
    linear-gradient(180deg, rgba(12, 16, 24, 0.55), rgba(7, 9, 15, 0.72));
  backdrop-filter: blur(12px) saturate(140%);
}

.input-field {
  flex: 1;
  background: rgba(7, 9, 15, 0.7);
  border: 1px solid var(--vl-border-strong);
  padding: 0.8rem 1.05rem;
  border-radius: var(--vl-radius);
  color: var(--vl-text);
  outline: none;
  transition: border-color 0.2s, box-shadow 0.2s;
  font-size: 0.92rem;
  resize: none;
  overflow-y: auto;
  min-height: 2.85rem;
  max-height: 160px;
  line-height: 1.5;
  font-family: inherit;
}

.input-field:focus {
  border-color: var(--vl-border-focus);
  box-shadow: 0 0 0 3px var(--vl-primary-dim);
}

.input-field:disabled {
  opacity: 0.55;
  cursor: not-allowed;
}

.btn-send {
  background: linear-gradient(135deg, var(--vl-primary), #14b8a6);
  color: var(--vl-text-inverse);
  border: none;
  padding: 0.8rem 1.45rem;
  border-radius: var(--vl-radius);
  font-weight: 600;
  cursor: pointer;
  transition: transform 0.2s var(--vl-ease), box-shadow 0.2s, filter 0.2s;
  font-size: 0.9rem;
  flex-shrink: 0;
  box-shadow: 0 4px 14px var(--vl-primary-glow);
  align-self: flex-end;
}

.btn-send:hover:not(:disabled) {
  transform: translateY(-1px);
  box-shadow: 0 8px 22px var(--vl-primary-glow);
  filter: brightness(1.05);
}

.btn-send:disabled {
  opacity: 0.4;
  cursor: not-allowed;
  transform: none;
  box-shadow: none;
}

.btn-send.stop {
  background: linear-gradient(135deg, #f87171, #dc2626);
  color: #fff;
  box-shadow: 0 4px 14px rgba(248, 113, 113, 0.28);
}

.btn-send.stop:hover {
  box-shadow: 0 8px 22px rgba(248, 113, 113, 0.35);
}

.btn-amber {
  background: var(--vl-primary);
  color: var(--vl-bg);
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
  box-shadow: 0 6px 24px rgba(45, 212, 191, 0.4);
}

.btn-amber:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

.spinner {
  width: 1.25rem;
  height: 1.25rem;
  border: 2.5px solid rgba(45, 212, 191, 0.15);
  border-top-color: var(--vl-primary);
  border-radius: 50%;
  animation: vl-spin 0.8s linear infinite;
}

.chat-input-toolbar {
  display: flex;
  justify-content: flex-start;
  gap: 0.4rem;
  flex-wrap: wrap;
  padding: 0.2rem;
  width: fit-content;
  max-width: 100%;
  background: rgba(255, 255, 255, 0.025);
  border: 1px solid var(--vl-border);
  border-radius: 999px;
}

.chat-mode-warning {
  color: var(--vl-warning);
  font-size: 0.78rem;
  line-height: 1.4;
  padding: 0.35rem 0.55rem;
  border-radius: var(--vl-radius-sm);
  background: var(--vl-warning-dim);
  border: 1px solid rgba(251, 191, 36, 0.22);
}

.chat-input-row {
  display: flex;
  gap: 0.75rem;
  align-items: flex-end;
}

.mode-toggle {
  background: transparent;
  border: 1px solid transparent;
  color: var(--vl-text-muted);
  cursor: pointer;
  font-size: 0.74rem;
  padding: 0.38rem 0.75rem;
  border-radius: 999px;
  transition: color 0.2s, background 0.2s, border-color 0.2s;
  font-family: var(--vl-font-mono);
  letter-spacing: 0.01em;
}

.mode-toggle:hover:not(:disabled) {
  color: var(--vl-text);
  background: rgba(255, 255, 255, 0.04);
}

.mode-toggle.active {
  background: var(--vl-primary-dim);
  border-color: rgba(45, 212, 191, 0.35);
  color: var(--vl-primary);
  font-weight: 600;
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
  font-family: var(--vl-font-mono);
  padding: 0.2rem 0.5rem;
  border-radius: 0.35rem;
  border: 1px solid rgba(139, 149, 168, 0.2);
  background: rgba(139, 149, 168, 0.08);
  color: #a8b3c7;
}

.agent-template {
  color: var(--vl-primary);
  border-color: rgba(45, 212, 191, 0.3);
  background: rgba(45, 212, 191, 0.1);
}

.agent-model {
  color: var(--vl-text-secondary);
}

.agent-trace {
  margin-top: 1rem;
  padding-top: 1rem;
  border-top: 1px solid rgba(139, 149, 168, 0.15);
}

.trace-item {
  background: rgba(7, 9, 15, 0.35);
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
  font-family: var(--vl-font-mono);
}

.trace-tool {
  color: var(--vl-info);
  background: rgba(96, 165, 250, 0.1);
  padding: 0.15rem 0.45rem;
  border-radius: 0.35rem;
  border: 1px solid rgba(96, 165, 250, 0.2);
}

.trace-name {
  color: #a8b3c7;
}

.trace-output {
  color: var(--vl-primary);
  opacity: 0.85;
}

.trace-input {
  margin-top: 0.4rem;
  font-size: 0.72rem;
  color: var(--vl-text-secondary);
  font-family: var(--vl-font-mono);
  white-space: pre-wrap;
  word-break: break-word;
  opacity: 0.8;
}

.trace-error {
  margin-top: 0.4rem;
  font-size: 0.72rem;
  color: var(--vl-danger);
  font-family: var(--vl-font-mono);
}
</style>
