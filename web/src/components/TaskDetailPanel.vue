<template>
  <!-- 空状态：未选中任务 -->
  <aside v-if="!task" class="detail-panel empty" aria-label="任务详情">
    <div class="empty-inner">
      <div class="empty-mark" aria-hidden="true">
        <span class="empty-core"></span>
      </div>
      <h3>选择一个视频</h3>
      <p>在左侧列表点选任务，查看文字提取与 AI 总结</p>
    </div>
  </aside>

  <aside
    v-else
    class="detail-panel"
    :class="{ mobile: mobileSheet }"
    role="region"
    :aria-label="task.title || task.filename"
  >
    <header class="detail-header">
      <button
        v-if="mobileSheet"
        class="back-btn"
        type="button"
        @click="$emit('close')"
        aria-label="返回列表"
      >
        ←
      </button>
      <div class="header-text">
        <h2 class="detail-title">{{ task.title || task.filename }}</h2>
        <div class="header-meta">
          <span class="meta-status" :class="detailedStatus.class">
            <span class="status-dot" aria-hidden="true"></span>
            {{ detailedStatus.text }}
          </span>
          <span class="meta-sep">·</span>
          <span class="meta-muted">{{ formatTime(task.created_at) }}</span>
          <span class="meta-sep">·</span>
          <span class="meta-muted">{{ formatFileSize(task.file_size) }}</span>
        </div>
      </div>
      <button
        class="close-btn"
        type="button"
        @click="$emit('close')"
        aria-label="关闭详情"
        title="关闭"
      >
        ×
      </button>
    </header>

    <div class="detail-actions">
      <div class="action-stack">
        <button
          class="action-btn"
          :class="{ done: hasTx }"
          type="button"
          @click="$emit('transcribe')"
          :disabled="txPrimaryDisabled"
          :title="hasTx ? '文字已提取，请在下方 Tab 查看' : '提取视频文字'"
        >
          {{ txLabel }}
        </button>
        <button
          v-if="hasTx && !isActionDisabled"
          type="button"
          class="rerun-link"
          @click="$emit('retranscribe')"
          title="重新调用 ASR，覆盖已有文字"
        >
          重新提取
        </button>
      </div>
      <div class="action-stack">
        <button
          class="action-btn accent"
          :class="{ done: hasSum }"
          type="button"
          @click="$emit('analyze')"
          :disabled="sumPrimaryDisabled"
          :title="hasSum ? '总结已生成，请在下方 Tab 查看' : '生成 AI 总结'"
        >
          {{ sumLabel }}
        </button>
        <button
          v-if="hasSum && !isActionDisabled"
          type="button"
          class="rerun-link"
          @click="$emit('reanalyze')"
          title="重新调用模型，覆盖已有总结"
        >
          重新总结
        </button>
      </div>
      <button
        v-if="canUseRAG"
        class="action-btn chat"
        type="button"
        @click="goChat"
      >
        去对话
      </button>
    </div>

    <nav class="detail-tabs" role="tablist" aria-label="详情分区">
      <button
        v-for="tab in tabs"
        :key="tab.key"
        type="button"
        role="tab"
        class="tab-btn"
        :class="{ active: activeTab === tab.key }"
        :aria-selected="activeTab === tab.key"
        @click="activeTab = tab.key"
      >
        {{ tab.label }}
        <span v-if="tab.badge" class="tab-badge">{{ tab.badge }}</span>
      </button>
    </nav>

    <div class="detail-body">
      <!-- 处理中横幅：不遮挡已有转写/总结，避免读结果时被整页 spinner 顶掉 -->
      <div v-if="loading" class="panel-loading-banner" role="status">
        <div class="spinner" aria-hidden="true"></div>
        <span>处理中…</span>
      </div>

      <!-- 概览 -->
      <div v-if="activeTab === 'overview'" class="tab-pane" role="tabpanel">
        <div v-if="failureMessage" class="error-block">
          <h4>处理失败</h4>
          <p class="error-text">{{ failureMessage }}</p>
        </div>

        <div v-if="errorMsg || task.next_retry_at" class="retry-info">
          <div v-if="errorMsg" class="retry-row">
            <span class="retry-label">错误</span>
            <span class="retry-text">{{ errorMsg }}</span>
          </div>
          <div v-if="task.retry_count !== undefined" class="retry-row">
            <span class="retry-label">重试</span>
            <span class="retry-text">{{ task.retry_count }} / {{ task.max_retries || 3 }}</span>
          </div>
          <div v-if="task.next_retry_at" class="retry-row">
            <span class="retry-label">下次</span>
            <span class="retry-text">{{ formatRelativeTime(task.next_retry_at) }}</span>
          </div>
        </div>

        <div class="meta-grid">
          <div class="meta-card">
            <span class="meta-label">创建时间</span>
            <span class="meta-value">{{ formatTime(task.created_at) }}</span>
          </div>
          <div class="meta-card">
            <span class="meta-label">文件大小</span>
            <span class="meta-value">{{ formatFileSize(task.file_size) }}</span>
          </div>
          <div class="meta-card">
            <span class="meta-label">状态</span>
            <span class="meta-status" :class="detailedStatus.class">
              <span class="status-dot" aria-hidden="true"></span>
              {{ detailedStatus.text }}
            </span>
          </div>
        </div>

        <div v-if="task.jobs?.length" class="jobs-section">
          <button type="button" class="jobs-toggle" @click="jobsExpanded = !jobsExpanded">
            <span class="section-title">处理明细</span>
            <span class="jobs-toggle-meta">{{ task.jobs.length }} 项 · {{ jobsExpanded ? '收起' : '展开' }}</span>
          </button>
          <div v-if="jobsExpanded" class="jobs-list">
            <div v-for="job in task.jobs" :key="job.id" class="job-item">
              <div class="job-header">
                <span class="job-type">{{ jobTypeLabel(job.job_type) }}</span>
                <span class="job-status" :class="jobStatusClass(job.status)">
                  {{ jobStatusLabel(job.status) }}
                </span>
              </div>
              <div v-if="job.stage" class="job-row">
                <span class="job-label">阶段</span>
                <span class="job-value">{{ job.stage }}</span>
              </div>
              <div v-if="job.retry_count !== undefined" class="job-row">
                <span class="job-label">重试</span>
                <span class="job-value">{{ job.retry_count }} / {{ job.max_retries || 3 }}</span>
              </div>
              <div v-if="job.next_retry_at" class="job-row">
                <span class="job-label">下次</span>
                <span class="job-value">{{ formatRelativeTime(job.next_retry_at) }}</span>
              </div>
              <div v-if="job.last_error_msg" class="job-row">
                <span class="job-label">错误</span>
                <span class="job-error-text">{{ job.last_error_msg }}</span>
              </div>
            </div>
          </div>
        </div>

        <div v-if="!task.jobs?.length && !failureMessage && !errorMsg" class="overview-hints">
          <p v-if="task.transcription?.content" class="hint-ok">已有文字提取，可在「文字提取」Tab 查看全文。</p>
          <p v-else class="hint-muted">尚未提取文字，点上方「提取文字」开始。</p>
          <p v-if="task.summary?.content" class="hint-ok">已有 AI 总结，可在「AI 总结」Tab 查看。</p>
          <p v-else class="hint-muted">尚未生成总结，点上方「AI 总结」开始。</p>
        </div>
      </div>

      <!-- 文字提取 -->
      <div v-else-if="activeTab === 'transcription'" class="tab-pane" role="tabpanel">
        <template v-if="task.transcription?.content">
          <div class="result-toolbar">
            <h4>文字提取</h4>
            <div class="result-actions">
              <button type="button" class="icon-btn" @click="copyText(task.transcription.content)" title="复制">
                复制
              </button>
              <button
                type="button"
                class="icon-btn"
                @click="downloadText(task.transcription.content, task.filename)"
                title="下载 TXT"
              >
                下载
              </button>
              <button
                type="button"
                class="icon-btn subtle"
                :disabled="isActionDisabled"
                @click="$emit('retranscribe')"
                title="重新调用 ASR"
              >
                重新提取
              </button>
            </div>
          </div>
          <pre class="result-text">{{ transcriptionPreview }}</pre>
          <button
            v-if="showTranscriptionExpand"
            type="button"
            class="expand-btn"
            @click="transcriptionExpanded = !transcriptionExpanded"
          >
            {{ transcriptionExpanded ? '收起' : '展开全部' }}
          </button>
        </template>
        <div v-else class="result-empty">
          <p>还没有文字提取结果</p>
          <button
            type="button"
            class="action-btn solid"
            @click="$emit('transcribe')"
            :disabled="isActionDisabled"
          >
            开始提取
          </button>
        </div>
      </div>

      <!-- AI 总结 -->
      <div v-else class="tab-pane" role="tabpanel">
        <template v-if="task.summary?.content">
          <div class="result-toolbar">
            <h4>AI 总结</h4>
            <div class="result-actions">
              <button type="button" class="icon-btn" @click="copyText(task.summary.content)" title="复制">
                复制
              </button>
              <button
                type="button"
                class="icon-btn"
                @click="downloadMarkdown(task.summary.content, task.filename)"
                title="下载 MD"
              >
                下载
              </button>
              <button
                type="button"
                class="icon-btn subtle"
                :disabled="isActionDisabled"
                @click="$emit('reanalyze')"
                title="重新调用模型"
              >
                重新总结
              </button>
            </div>
          </div>
          <div class="result-markdown" v-html="renderMarkdown(summaryPreview)"></div>
          <button
            v-if="showSummaryExpand"
            type="button"
            class="expand-btn"
            @click="summaryExpanded = !summaryExpanded"
          >
            {{ summaryExpanded ? '收起' : '展开全部' }}
          </button>
        </template>
        <div v-else class="result-empty">
          <p>还没有 AI 总结</p>
          <button
            type="button"
            class="action-btn solid"
            @click="$emit('analyze')"
            :disabled="isActionDisabled"
          >
            开始总结
          </button>
        </div>
      </div>
    </div>
  </aside>
</template>

<script setup>
import { ref, computed, watch } from 'vue'
import { marked } from 'marked'
import DOMPurify from 'dompurify'
import { useRouter } from 'vue-router'
import {
  hasSummaryResult,
  hasTranscriptionResult,
  isPrimaryAnalyzeDisabled,
  isPrimaryTranscribeDisabled,
  isTaskActionDisabled,
  primaryAnalyzeLabel,
  primaryTranscribeLabel,
} from '../taskActionPolicy.js'
import { taskFailureMessage } from '../taskDetailPolicy.js'
import {
  DEFAULT_SUMMARY_PREVIEW_OPTIONS,
  DEFAULT_TRANSCRIPTION_PREVIEW_OPTIONS,
  taskResultNeedsExpansion,
  taskResultTextForDisplay,
} from '../taskResultDisplayPolicy.js'
import {
  formatTime,
  formatFileSize,
  getDetailedStatus,
  getErrorMessage,
  formatRelativeTime,
} from '../utils/format.js'

const props = defineProps({
  task: Object,
  loading: Boolean,
  /** 移动端全屏 sheet 模式（显示返回按钮） */
  mobileSheet: { type: Boolean, default: false },
})

const emit = defineEmits(['close', 'transcribe', 'analyze', 'retranscribe', 'reanalyze', 'toast'])

const router = useRouter()
const activeTab = ref('overview')
const transcriptionExpanded = ref(false)
const summaryExpanded = ref(false)
const jobsExpanded = ref(false)

const tabs = computed(() => [
  { key: 'overview', label: '概览' },
  {
    key: 'transcription',
    label: '文字提取',
    badge: props.task?.transcription?.content ? '✓' : '',
  },
  {
    key: 'summary',
    label: 'AI 总结',
    badge: props.task?.summary?.content ? '✓' : '',
  },
])

const transcriptionPreview = computed(() => {
  const content = props.task?.transcription?.content || ''
  return taskResultTextForDisplay(content, transcriptionExpanded.value, DEFAULT_TRANSCRIPTION_PREVIEW_OPTIONS)
})

const summaryPreview = computed(() => {
  const content = props.task?.summary?.content || ''
  return taskResultTextForDisplay(content, summaryExpanded.value, DEFAULT_SUMMARY_PREVIEW_OPTIONS)
})

const showTranscriptionExpand = computed(() => {
  const content = props.task?.transcription?.content || ''
  return taskResultNeedsExpansion(content, DEFAULT_TRANSCRIPTION_PREVIEW_OPTIONS)
})

const showSummaryExpand = computed(() => {
  const content = props.task?.summary?.content || ''
  return taskResultNeedsExpansion(content, DEFAULT_SUMMARY_PREVIEW_OPTIONS)
})

const canUseRAG = computed(() => {
  return hasTranscriptionResult(props.task) || props.task?.status === 3
})

const isActionDisabled = computed(() => isTaskActionDisabled(props.task, props.loading))
const hasTx = computed(() => hasTranscriptionResult(props.task))
const hasSum = computed(() => hasSummaryResult(props.task))
const txPrimaryDisabled = computed(() => isPrimaryTranscribeDisabled(props.task, props.loading))
const sumPrimaryDisabled = computed(() => isPrimaryAnalyzeDisabled(props.task, props.loading))
const txLabel = computed(() => primaryTranscribeLabel(props.task))
const sumLabel = computed(() => primaryAnalyzeLabel(props.task))
const failureMessage = computed(() => taskFailureMessage(props.task))
const detailedStatus = computed(() => getDetailedStatus(props.task))
const errorMsg = computed(() => getErrorMessage(props.task))

const goChat = () => {
  if (props.task?.id == null) return
  emit('close')
  router.push({ name: 'chat-task', params: { taskId: props.task.id } })
}

const renderMarkdown = (content) => DOMPurify.sanitize(marked.parse(content || ''))

const copyText = async (text) => {
  try {
    await navigator.clipboard.writeText(text)
    emit('toast', '已复制到剪贴板')
  } catch {
    emit('toast', '复制失败')
  }
}

const downloadText = (content, filename) => {
  const blob = new Blob([content], { type: 'text/plain;charset=utf-8' })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = `${filename || 'video'}_文字提取.txt`
  a.click()
  URL.revokeObjectURL(url)
}

const downloadMarkdown = (content, filename) => {
  const blob = new Blob([content], { type: 'text/markdown;charset=utf-8' })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = `${filename || 'video'}_AI总结.md`
  a.click()
  URL.revokeObjectURL(url)
}

const jobTypeLabel = (type) =>
  ({ download: '下载', transcribe: '转录', analyze: '分析', rag_index: 'RAG 索引' }[type] || type)

const jobStatusLabel = (status) =>
  ({
    queued: '排队中',
    running: '运行中',
    completed: '已完成',
    failed: '失败',
    retrying: '重试中',
  }[status] || status)

const jobStatusClass = (status) => `job-status-${status || 'queued'}`

// 选中新任务时：重置折叠；有内容时优先落到有结果的 Tab
watch(
  () => props.task?.id,
  (id, prev) => {
    if (id === prev) return
    transcriptionExpanded.value = false
    summaryExpanded.value = false
    jobsExpanded.value = false
    if (!props.task) {
      activeTab.value = 'overview'
      return
    }
    if (props.task.summary?.content) activeTab.value = 'summary'
    else if (props.task.transcription?.content) activeTab.value = 'transcription'
    else activeTab.value = 'overview'
  },
)

// 轮询完成后内容首次出现：若仍在概览，自动切到对应结果 Tab
watch(
  () => [!!props.task?.transcription?.content, !!props.task?.summary?.content],
  ([hasTx, hasSum], [prevTx, prevSum] = [false, false]) => {
    if (activeTab.value !== 'overview') return
    if (hasSum && !prevSum) activeTab.value = 'summary'
    else if (hasTx && !prevTx) activeTab.value = 'transcription'
  },
)
</script>

<style scoped>
.detail-panel {
  display: flex;
  flex-direction: column;
  min-height: 0;
  height: 100%;
  background: var(--vl-panel);
  border: 1px solid var(--vl-border);
  border-radius: var(--vl-radius-lg);
  overflow: hidden;
}

.detail-panel.empty {
  align-items: center;
  justify-content: center;
  border-style: dashed;
  background: color-mix(in srgb, var(--vl-panel) 45%, transparent);
}

.empty-inner {
  text-align: center;
  padding: 2rem 1.5rem;
  max-width: 16rem;
}

.empty-mark {
  width: 3rem;
  height: 3rem;
  margin: 0 auto 1rem;
  border-radius: 0.75rem;
  display: grid;
  place-items: center;
  background: linear-gradient(145deg, var(--vl-primary-dim), var(--vl-info-dim));
  border: 1px solid var(--vl-primary-glow);
}

.empty-core {
  width: 0.7rem;
  height: 0.7rem;
  border-radius: 50%;
  background: radial-gradient(circle at 30% 30%, var(--vl-primary), var(--vl-primary-deep) 70%);
  box-shadow: 0 0 12px var(--vl-border-focus);
}

.empty-inner h3 {
  margin: 0 0 0.4rem;
  font-family: var(--vl-font-display);
  font-size: 1.1rem;
  color: var(--vl-text);
}

.empty-inner p {
  margin: 0;
  font-size: 0.86rem;
  line-height: 1.55;
  color: var(--vl-text-muted);
}

.detail-header {
  display: flex;
  align-items: flex-start;
  gap: 0.65rem;
  padding: 1rem 1.15rem 0.85rem;
  border-bottom: 1px solid var(--vl-border);
  flex-shrink: 0;
}

.back-btn {
  flex-shrink: 0;
  width: 2.25rem;
  height: 2.25rem;
  border-radius: var(--vl-radius-sm);
  border: 1px solid var(--vl-border);
  background: transparent;
  color: var(--vl-text-secondary);
  font-size: 1.1rem;
  cursor: pointer;
  display: grid;
  place-items: center;
}

.back-btn:hover {
  border-color: var(--vl-border-focus);
  color: var(--vl-primary);
  background: var(--vl-primary-dim);
}

.header-text {
  flex: 1;
  min-width: 0;
}

.detail-title {
  margin: 0 0 0.35rem;
  font-family: var(--vl-font-display);
  font-size: 1.15rem;
  font-weight: 700;
  color: var(--vl-text);
  line-height: 1.35;
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
  overflow: hidden;
}

.header-meta {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 0.35rem;
  font-size: 0.78rem;
  color: var(--vl-text-muted);
}

.meta-sep {
  opacity: 0.45;
}

.meta-muted {
  font-family: var(--vl-font-mono);
  font-size: 0.74rem;
}

.close-btn {
  flex-shrink: 0;
  width: 2.25rem;
  height: 2.25rem;
  border-radius: 50%;
  border: 1px solid transparent;
  background: transparent;
  color: var(--vl-text-muted);
  font-size: 1.35rem;
  line-height: 1;
  cursor: pointer;
  display: grid;
  place-items: center;
  transition: color 0.2s, background 0.2s, border-color 0.2s;
}

.close-btn:hover {
  color: var(--vl-danger);
  background: var(--vl-danger-dim);
  border-color: color-mix(in srgb, var(--vl-danger) 30%, transparent);
}

.detail-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 0.5rem;
  padding: 0.85rem 1.15rem;
  border-bottom: 1px solid var(--vl-border);
  flex-shrink: 0;
  align-items: flex-start;
}

.action-stack {
  flex: 1;
  min-width: 5.5rem;
  display: flex;
  flex-direction: column;
  gap: 0.25rem;
}

.action-btn {
  width: 100%;
  min-width: 5.5rem;
  padding: 0.55rem 0.75rem;
  border-radius: var(--vl-radius-sm);
  border: 1px solid var(--vl-border);
  background: var(--vl-surface-hover);
  color: var(--vl-text-secondary);
  font-weight: 600;
  font-size: 0.82rem;
  cursor: pointer;
  transition: border-color 0.2s, color 0.2s, background 0.2s, opacity 0.2s;
}

.detail-actions > .action-btn.chat {
  flex: 1;
  width: auto;
  align-self: stretch;
}

.action-btn:hover:not(:disabled) {
  border-color: var(--vl-border-focus);
  color: var(--vl-primary);
  background: var(--vl-primary-dim);
}

.action-btn.accent {
  border-color: var(--vl-accent-glow);
  color: var(--vl-accent);
  background: var(--vl-accent-dim);
}

.action-btn.chat {
  border-color: color-mix(in srgb, var(--vl-info) 30%, transparent);
  color: var(--vl-info);
  background: var(--vl-info-dim);
}

.action-btn.solid {
  flex: 0 1 auto;
  width: auto;
  border-color: var(--vl-border-focus);
  color: var(--vl-text-inverse);
  background: linear-gradient(135deg, var(--vl-primary), var(--vl-primary-deep));
}

.action-btn:disabled {
  opacity: 0.4;
  cursor: not-allowed;
}

.action-btn.done:disabled {
  opacity: 0.55;
  color: var(--vl-text-muted);
  border-color: var(--vl-border);
  background: color-mix(in srgb, var(--vl-bg) 45%, transparent);
  cursor: default;
}

.rerun-link {
  appearance: none;
  border: none;
  background: transparent;
  color: var(--vl-text-muted);
  font-size: 0.72rem;
  font-weight: 500;
  padding: 0.05rem 0.1rem;
  cursor: pointer;
  text-align: center;
  text-decoration: underline;
  text-underline-offset: 2px;
  opacity: 0.8;
}

.rerun-link:hover {
  color: var(--vl-text-secondary);
  opacity: 1;
}

.icon-btn.subtle {
  opacity: 0.75;
  font-weight: 500;
}

.icon-btn.subtle:disabled {
  opacity: 0.4;
  cursor: not-allowed;
}

.detail-tabs {
  display: flex;
  gap: 0.25rem;
  padding: 0.55rem 1rem 0;
  border-bottom: 1px solid var(--vl-border);
  flex-shrink: 0;
  overflow-x: auto;
}

.tab-btn {
  position: relative;
  padding: 0.55rem 0.9rem 0.65rem;
  border: none;
  background: transparent;
  color: var(--vl-text-muted);
  font-weight: 600;
  font-size: 0.84rem;
  cursor: pointer;
  border-bottom: 2px solid transparent;
  margin-bottom: -1px;
  white-space: nowrap;
  display: inline-flex;
  align-items: center;
  gap: 0.35rem;
  transition: color 0.2s, border-color 0.2s;
}

.tab-btn:hover {
  color: var(--vl-text);
}

.tab-btn.active {
  color: var(--vl-primary);
  border-bottom-color: var(--vl-primary);
}

.tab-badge {
  font-size: 0.7rem;
  color: var(--vl-success);
  font-weight: 700;
}

.detail-body {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
  padding: 1rem 1.15rem 1.35rem;
  scrollbar-width: thin;
  scrollbar-color: var(--vl-primary-glow) transparent;
}

.tab-pane {
  animation: vl-fade-in-up 0.28s var(--vl-ease);
}

.panel-loading-banner {
  display: flex;
  align-items: center;
  gap: 0.65rem;
  margin: -0.25rem 0 0.9rem;
  padding: 0.65rem 0.85rem;
  border-radius: var(--vl-radius-sm);
  border: 1px solid var(--vl-primary-glow);
  background: var(--vl-primary-dim);
  color: var(--vl-primary);
  font-weight: 600;
  font-size: 0.84rem;
}

.spinner {
  width: 1.35rem;
  height: 1.35rem;
  border: 2px solid color-mix(in srgb, var(--vl-primary) 20%, transparent);
  border-top-color: var(--vl-primary);
  border-radius: 50%;
  animation: vl-spin 0.75s linear infinite;
}

.meta-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(120px, 1fr));
  gap: 0.65rem;
  margin-bottom: 1.15rem;
}

.meta-card {
  display: flex;
  flex-direction: column;
  gap: 0.35rem;
  padding: 0.75rem 0.85rem;
  border-radius: var(--vl-radius);
  border: 1px solid var(--vl-border);
  background: var(--vl-surface);
}

.meta-label {
  font-size: 0.72rem;
  font-weight: 600;
  color: var(--vl-text-muted);
  text-transform: uppercase;
  letter-spacing: 0.04em;
}

.meta-value {
  font-size: 0.88rem;
  font-weight: 600;
  font-family: var(--vl-font-mono);
  color: var(--vl-text);
}

.meta-status {
  display: inline-flex;
  align-items: center;
  gap: 0.3rem;
  width: fit-content;
  padding: 0.15rem 0.5rem;
  border-radius: 999px;
  font-weight: 600;
  font-size: 0.72rem;
  border: 1px solid transparent;
}

.status-dot {
  width: 0.35rem;
  height: 0.35rem;
  border-radius: 50%;
  background: currentColor;
}

.meta-status.pending {
  background: var(--vl-border);
  color: var(--vl-text-secondary);
  border-color: var(--vl-border-strong);
}
.meta-status.queued {
  background: var(--vl-info-dim);
  color: var(--vl-info);
  border-color: color-mix(in srgb, var(--vl-info) 30%, transparent);
}
.meta-status.running {
  background: var(--vl-accent-dim);
  color: var(--vl-accent);
  border-color: color-mix(in srgb, var(--vl-accent) 30%, transparent);
}
.meta-status.running .status-dot {
  animation: vl-status-pulse 1.6s ease-in-out infinite;
}
.meta-status.completed {
  background: var(--vl-success-dim);
  color: var(--vl-success);
  border-color: color-mix(in srgb, var(--vl-success) 30%, transparent);
}
.meta-status.failed {
  background: var(--vl-danger-dim);
  color: var(--vl-danger);
  border-color: color-mix(in srgb, var(--vl-danger) 30%, transparent);
}
.meta-status.retrying {
  background: var(--vl-warning-dim);
  color: var(--vl-warning);
  border-color: color-mix(in srgb, var(--vl-warning) 30%, transparent);
}
.meta-status.dead {
  background: var(--vl-border);
  color: var(--vl-text-secondary);
  border-color: color-mix(in srgb, var(--vl-text-muted) 30%, transparent);
}

.retry-info,
.error-block {
  margin-bottom: 1rem;
  padding: 0.85rem 1rem;
  border-radius: var(--vl-radius);
  border: 1px solid color-mix(in srgb, var(--vl-warning) 28%, transparent);
  background: var(--vl-warning-dim);
}

.error-block {
  border-color: color-mix(in srgb, var(--vl-danger) 30%, transparent);
  background: var(--vl-danger-dim);
}

.error-block h4 {
  margin: 0 0 0.4rem;
  font-size: 0.9rem;
  color: var(--vl-danger);
}

.error-text {
  margin: 0;
  font-size: 0.86rem;
  line-height: 1.55;
  color: var(--vl-danger);
  white-space: pre-wrap;
  font-family: var(--vl-font-mono);
}

.retry-row {
  display: flex;
  gap: 0.65rem;
  margin-bottom: 0.4rem;
  font-size: 0.84rem;
}

.retry-row:last-child {
  margin-bottom: 0;
}

.retry-label {
  flex-shrink: 0;
  min-width: 2.5rem;
  font-weight: 600;
  color: var(--vl-warning);
}

.retry-text {
  color: var(--vl-text);
  font-family: var(--vl-font-mono);
  word-break: break-word;
}

.jobs-section {
  margin-top: 0.25rem;
}

.jobs-toggle {
  width: 100%;
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: 0.75rem;
  appearance: none;
  border: 1px solid var(--vl-border);
  background: var(--vl-surface-hover);
  border-radius: var(--vl-radius-sm);
  padding: 0.55rem 0.75rem;
  cursor: pointer;
  margin-bottom: 0.55rem;
  color: inherit;
  text-align: left;
}

.jobs-toggle:hover {
  border-color: var(--vl-border-strong);
  background: var(--vl-white-a03);
}

.jobs-toggle .section-title {
  margin: 0;
}

.jobs-toggle-meta {
  font-size: 0.72rem;
  color: var(--vl-text-muted);
  font-family: var(--vl-font-mono);
  white-space: nowrap;
}

.section-title {
  margin: 0 0 0.65rem;
  font-size: 0.88rem;
  font-weight: 700;
  font-family: var(--vl-font-display);
  color: var(--vl-text);
}

.jobs-list {
  display: flex;
  flex-direction: column;
  gap: 0.55rem;
}

.job-item {
  padding: 0.75rem 0.85rem;
  border-radius: var(--vl-radius-sm);
  border: 1px solid var(--vl-border);
  background: var(--vl-surface);
}

.job-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 0.5rem;
  margin-bottom: 0.4rem;
}

.job-type {
  font-weight: 600;
  font-size: 0.88rem;
  color: var(--vl-text);
}

.job-status {
  padding: 0.15rem 0.45rem;
  border-radius: 0.35rem;
  font-size: 0.7rem;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.03em;
}

.job-status-queued {
  background: var(--vl-info-dim);
  color: var(--vl-info);
}
.job-status-running {
  background: var(--vl-primary-dim);
  color: var(--vl-primary);
}
.job-status-completed {
  background: var(--vl-success-dim);
  color: var(--vl-success);
}
.job-status-failed {
  background: var(--vl-danger-dim);
  color: var(--vl-danger);
}
.job-status-retrying {
  background: var(--vl-warning-dim);
  color: var(--vl-warning);
}

.job-row {
  display: flex;
  gap: 0.5rem;
  font-size: 0.8rem;
  margin-top: 0.25rem;
}

.job-label {
  flex-shrink: 0;
  min-width: 2.25rem;
  color: var(--vl-text-muted);
  font-weight: 600;
}

.job-value {
  color: var(--vl-text-secondary);
  font-family: var(--vl-font-mono);
}

.job-error-text {
  color: var(--vl-danger);
  font-family: var(--vl-font-mono);
  word-break: break-word;
}

.overview-hints {
  display: flex;
  flex-direction: column;
  gap: 0.45rem;
  margin-top: 0.5rem;
}

.hint-ok,
.hint-muted {
  margin: 0;
  font-size: 0.86rem;
  line-height: 1.5;
}

.hint-ok {
  color: var(--vl-success);
}

.hint-muted {
  color: var(--vl-text-muted);
}

.result-toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 0.75rem;
  margin-bottom: 0.75rem;
}

.result-toolbar h4 {
  margin: 0;
  font-size: 0.95rem;
  font-weight: 700;
  color: var(--vl-text);
}

.result-actions {
  display: flex;
  gap: 0.4rem;
}

.icon-btn {
  padding: 0.35rem 0.65rem;
  border-radius: var(--vl-radius-sm);
  border: 1px solid var(--vl-primary-glow);
  background: var(--vl-primary-dim);
  color: var(--vl-primary);
  font-size: 0.78rem;
  font-weight: 600;
  cursor: pointer;
  transition: border-color 0.2s, background 0.2s;
}

.icon-btn:hover {
  border-color: var(--vl-border-focus);
  background: var(--vl-primary-glow);
}

.result-text {
  margin: 0;
  padding: 1rem 1.1rem;
  border-radius: var(--vl-radius);
  border: 1px solid var(--vl-border);
  background: var(--vl-surface);
  font-family: var(--vl-font-mono);
  font-size: 0.86rem;
  line-height: 1.75;
  white-space: pre-wrap;
  color: var(--vl-text-secondary);
  max-height: none;
  overflow: visible;
}

.result-markdown {
  padding: 1rem 1.1rem;
  border-radius: var(--vl-radius);
  border: 1px solid var(--vl-border);
  background: var(--vl-surface);
  line-height: 1.8;
  overflow: visible;
}

.result-markdown :deep(h2),
.result-markdown :deep(h3) {
  color: var(--vl-text);
  margin: 1.1rem 0 0.55rem;
  font-weight: 700;
}

.result-markdown :deep(p) {
  margin: 0 0 0.85rem;
  color: var(--vl-text-secondary);
  font-size: 0.9rem;
}

.result-markdown :deep(strong) {
  color: var(--vl-primary);
  font-weight: 600;
}

.result-markdown :deep(ul) {
  padding-left: 1.35rem;
  margin: 0 0 0.85rem;
}

.result-markdown :deep(li) {
  margin-bottom: 0.4rem;
  color: var(--vl-text-secondary);
}

.result-markdown :deep(li::marker) {
  color: var(--vl-primary);
}

.result-markdown :deep(code) {
  background: var(--vl-primary-dim);
  padding: 0.12rem 0.4rem;
  border-radius: 0.3rem;
  font-family: var(--vl-font-mono);
  font-size: 0.82rem;
  color: var(--vl-primary);
  border: 1px solid color-mix(in srgb, var(--vl-primary) 20%, transparent);
}

.expand-btn {
  width: 100%;
  margin-top: 0.75rem;
  padding: 0.55rem 1rem;
  border-radius: var(--vl-radius-sm);
  border: 1px solid var(--vl-border);
  background: transparent;
  color: var(--vl-text-secondary);
  font-weight: 600;
  font-size: 0.82rem;
  cursor: pointer;
  transition: border-color 0.2s, color 0.2s, background 0.2s;
}

.expand-btn:hover {
  border-color: var(--vl-border-focus);
  color: var(--vl-primary);
  background: var(--vl-primary-dim);
}

.result-empty {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 0.85rem;
  min-height: 12rem;
  text-align: center;
  color: var(--vl-text-muted);
  font-size: 0.9rem;
}

.result-empty p {
  margin: 0;
}

/* 移动端全屏 sheet：由父级控制定位 */
.detail-panel.mobile {
  border-radius: 0;
  border: none;
  height: 100%;
}
</style>
