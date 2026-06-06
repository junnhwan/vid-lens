<template>
  <div class="rag-chat">
    <div v-if="!indexStatus.indexed" class="index-prompt">
      <div class="prompt-icon">🔍</div>
      <p>需要先构建视频索引才能提问</p>
      <button class="btn-amber" @click="buildIndex" :disabled="building">
        {{ building ? '构建中...' : '构建视频索引' }}
      </button>
      <div v-if="indexStatus.indexed" class="index-info">
        ✅ 已构建 {{ indexStatus.chunks }} 个片段
      </div>
    </div>

    <div v-else class="chat-container">
      <div class="chat-messages" ref="messagesContainer">
        <div v-for="msg in messages" :key="msg.id" class="message" :class="msg.role">
          <div class="message-content">
            <div class="message-text">{{ msg.content }}</div>
            <div v-if="msg.citations && msg.citations.length" class="citations">
              <div class="citations-header">📚 参考片段</div>
              <div v-for="(cite, idx) in msg.citations" :key="idx" class="citation-item">
                <div class="citation-score">相关度: {{ (cite.score * 100).toFixed(0) }}%</div>
                <div class="citation-content">{{ cite.content }}</div>
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
        <input
          v-model="question"
          @keyup.enter="sendQuestion"
          placeholder="问问这个视频..."
          :disabled="loading"
          class="input-field"
        />
        <button @click="sendQuestion" :disabled="loading || !question" class="btn-send">
          发送
        </button>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, onMounted, nextTick } from 'vue'
import api from '../api'

const props = defineProps({
  task: Object
})

const emit = defineEmits(['error'])

const indexStatus = ref({ indexed: false, chunks: 0 })
const building = ref(false)
const messages = ref([])
const question = ref('')
const loading = ref(false)
const sessionId = ref(null)
const messagesContainer = ref(null)

const buildIndex = async () => {
  building.value = true
  try {
    const res = await api.buildRAGIndex(props.task.id)
    indexStatus.value = { indexed: true, chunks: res.chunks || 0 }
    await createSession()
  } catch (err) {
    emit('error', err.message || '构建索引失败')
  } finally {
    building.value = false
  }
}

const createSession = async () => {
  try {
    const res = await api.createChatSession(props.task.id, '会话')
    sessionId.value = res.id
  } catch (err) {
    console.error('创建会话失败:', err)
  }
}

const sendQuestion = async () => {
  if (!question.value || loading.value) return

  const userMessage = { id: Date.now(), role: 'user', content: question.value }
  messages.value.push(userMessage)
  const q = question.value
  question.value = ''

  loading.value = true
  try {
    const res = await api.sendChatMessage(sessionId.value, q, 5)
    messages.value.push({
      id: res.message_id,
      role: 'assistant',
      content: res.answer,
      citations: res.citations || []
    })
    await nextTick()
    messagesContainer.value?.scrollTo({ top: messagesContainer.value.scrollHeight, behavior: 'smooth' })
  } catch (err) {
    emit('error', err.message || '发送失败')
  } finally {
    loading.value = false
  }
}

const checkIndexStatus = async () => {
  try {
    const res = await api.getRAGIndexStatus(props.task.id)
    indexStatus.value = { indexed: res.indexed, chunks: res.chunks || 0 }
    if (res.indexed && !sessionId.value) {
      await createSession()
    }
  } catch (err) {
    console.error('检查索引状态失败:', err)
  }
}

onMounted(() => {
  checkIndexStatus()
})
</script>

<style scoped>
.rag-chat {
  height: 100%;
  display: flex;
  flex-direction: column;
}

.index-prompt {
  text-align: center;
  padding: 3rem;
}

.prompt-icon {
  font-size: 4rem;
  margin-bottom: 1rem;
}

.index-prompt p {
  color: #8b95a8;
  margin-bottom: 2rem;
  font-size: 1rem;
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
  animation: messageIn 0.3s ease-out;
}

@keyframes messageIn {
  from { opacity: 0; transform: translateY(10px); }
  to { opacity: 1; transform: translateY(0); }
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

.citations {
  margin-top: 1rem;
  padding-top: 1rem;
  border-top: 1px solid rgba(139, 149, 168, 0.15);
}

.citations-header {
  font-size: 0.85rem;
  color: #d4af37;
  margin-bottom: 0.75rem;
  font-weight: 600;
}

.citation-item {
  background: rgba(10, 14, 26, 0.5);
  padding: 0.75rem;
  border-radius: 0.65rem;
  margin-bottom: 0.5rem;
  border: 1px solid rgba(139, 149, 168, 0.1);
}

.citation-score {
  font-size: 0.8rem;
  color: #5b8fff;
  margin-bottom: 0.5rem;
  font-family: 'JetBrains Mono', monospace;
}

.citation-content {
  font-size: 0.85rem;
  color: #8b95a8;
  line-height: 1.6;
}

.chat-input {
  padding: 1.5rem;
  border-top: 1px solid rgba(212, 175, 55, 0.15);
  display: flex;
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
  animation: spin 0.8s linear infinite;
}

@keyframes spin {
  to { transform: rotate(360deg); }
}
</style>
