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
    :class="{ mobile: mobileSheet, focus: readingFocus }"
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
            <VlIcon
              :name="detailedStatus.icon"
              size="sm"
              :spin="detailedStatus.class === 'running' || detailedStatus.class === 'retrying'"
            />
            {{ detailedStatus.text }}
          </span>
          <span class="meta-sep">·</span>
          <span class="meta-muted">{{ formatTime(task.created_at) }}</span>
          <span class="meta-sep">·</span>
          <span class="meta-muted">{{ formatFileSize(task.file_size) }}</span>
        </div>
      </div>
      <div class="header-tools">
        <button
          v-if="!mobileSheet"
          type="button"
          class="tool-btn"
          :class="{ active: readingFocus }"
          :title="readingFocus ? '退出专注阅读' : '专注阅读（隐藏列表）'"
          :aria-pressed="readingFocus"
          @click="$emit('toggle-focus')"
        >
          {{ readingFocus ? '退出专注' : '专注阅读' }}
        </button>
        <button
          v-if="canUseRAG"
          type="button"
          class="tool-btn chat"
          @click="goChat"
        >
          去对话
        </button>
        <div class="more-wrap" ref="moreWrapRef">
          <button
            type="button"
            class="tool-btn ghost"
            :aria-expanded="moreOpen"
            aria-haspopup="menu"
            @click="moreOpen = !moreOpen"
          >
            更多
          </button>
          <div v-if="moreOpen" class="more-menu" role="menu">
            <button
              type="button"
              role="menuitem"
              class="more-item"
              :disabled="txPrimaryDisabled"
              @click="runAndCloseMore(() => $emit('transcribe'))"
            >
              {{ hasTx ? '查看文字提取' : '提取文字' }}
            </button>
            <button
              type="button"
              role="menuitem"
              class="more-item"
              :disabled="sumPrimaryDisabled"
              @click="runAndCloseMore(() => $emit('analyze'))"
            >
              {{ hasSum ? '查看 AI 总结' : 'AI 总结' }}
            </button>
            <button
              v-if="hasTx"
              type="button"
              role="menuitem"
              class="more-item subtle"
              :disabled="isActionDisabled"
              @click="runAndCloseMore(() => $emit('retranscribe'))"
            >
              重新提取
            </button>
            <button
              v-if="hasSum"
              type="button"
              role="menuitem"
              class="more-item subtle"
              :disabled="isActionDisabled"
              @click="runAndCloseMore(() => $emit('reanalyze'))"
            >
              重新总结
            </button>
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
      </div>
    </header>

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
        <span v-if="tab.badge" class="tab-badge" aria-label="已完成">
          <VlIcon :name="ICON.check" size="sm" />
        </span>
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
              <VlIcon
                :name="detailedStatus.icon"
                size="sm"
                :spin="detailedStatus.class === 'running' || detailedStatus.class === 'retrying'"
              />
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
          <div class="result-article" :class="{ expanded: transcriptionExpanded, collapsed: showTranscriptionExpand && !transcriptionExpanded }">
            <pre class="result-text">{{ transcriptionPreview }}</pre>
            <div
              v-if="showTranscriptionExpand && !transcriptionExpanded"
              class="result-fade"
              aria-hidden="true"
            ></div>
          </div>
          <div v-if="showTranscriptionExpand" class="expand-bar">
            <button
              type="button"
              class="expand-btn"
              :aria-expanded="transcriptionExpanded"
              @click="toggleTranscriptionExpand"
            >
              <span class="expand-chevron" aria-hidden="true">{{ transcriptionExpanded ? '▴' : '▾' }}</span>
              <span class="expand-label">{{ transcriptionExpandLabel }}</span>
            </button>
            <span v-if="!transcriptionExpanded" class="expand-hint">{{ transcriptionExpandMeta.label }}</span>
          </div>
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
          <div class="result-article" :class="{ expanded: summaryExpanded, collapsed: showSummaryExpand && !summaryExpanded }">
            <div class="result-markdown" v-html="renderMarkdown(summaryPreview)"></div>
            <div
              v-if="showSummaryExpand && !summaryExpanded"
              class="result-fade"
              aria-hidden="true"
            ></div>
          </div>
          <div v-if="showSummaryExpand" class="expand-bar">
            <button
              type="button"
              class="expand-btn"
              :aria-expanded="summaryExpanded"
              @click="toggleSummaryExpand"
            >
              <span class="expand-chevron" aria-hidden="true">{{ summaryExpanded ? '▴' : '▾' }}</span>
              <span class="expand-label">{{ summaryExpandLabel }}</span>
            </button>
            <span v-if="!summaryExpanded" class="expand-hint">{{ summaryExpandMeta.label }}</span>
          </div>
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
import { ref, computed, watch, onMounted, onUnmounted } from 'vue'
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
  FOCUS_SUMMARY_PREVIEW_OPTIONS,
  FOCUS_TRANSCRIPTION_PREVIEW_OPTIONS,
  taskResultExpandButtonLabel,
  taskResultExpandMeta,
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
import VlIcon from './VlIcon.vue'
import { ICON } from '../icons.js'

const props = defineProps({
  task: Object,
  loading: Boolean,
  /** 移动端全屏 sheet 模式（显示返回按钮） */
  mobileSheet: { type: Boolean, default: false },
  /** 实验：专注阅读（父级隐藏列表） */
  readingFocus: { type: Boolean, default: false },
})

const emit = defineEmits(['close', 'transcribe', 'analyze', 'retranscribe', 'reanalyze', 'toggle-focus', 'toast'])

const router = useRouter()
const activeTab = ref('overview')
const transcriptionExpanded = ref(false)
const summaryExpanded = ref(false)
const jobsExpanded = ref(false)
const moreOpen = ref(false)
const moreWrapRef = ref(null)

const runAndCloseMore = (fn) => {
  moreOpen.value = false
  fn?.()
}

const onDocClick = (e) => {
  if (!moreOpen.value) return
  const el = moreWrapRef.value
  if (el && !el.contains(e.target)) moreOpen.value = false
}

onMounted(() => document.addEventListener('click', onDocClick))
onUnmounted(() => document.removeEventListener('click', onDocClick))

const tabs = computed(() => [
  { key: 'overview', label: '概览' },
  {
    key: 'transcription',
    label: '文字提取',
    badge: !!props.task?.transcription?.content,
  },
  {
    key: 'summary',
    label: 'AI 总结',
    badge: !!props.task?.summary?.content,
  },
])

const transcriptionOptions = computed(() =>
  props.readingFocus ? FOCUS_TRANSCRIPTION_PREVIEW_OPTIONS : DEFAULT_TRANSCRIPTION_PREVIEW_OPTIONS,
)
const summaryOptions = computed(() =>
  props.readingFocus ? FOCUS_SUMMARY_PREVIEW_OPTIONS : DEFAULT_SUMMARY_PREVIEW_OPTIONS,
)

const transcriptionPreview = computed(() => {
  const content = props.task?.transcription?.content || ''
  return taskResultTextForDisplay(content, transcriptionExpanded.value, transcriptionOptions.value)
})

const summaryPreview = computed(() => {
  const content = props.task?.summary?.content || ''
  return taskResultTextForDisplay(content, summaryExpanded.value, summaryOptions.value)
})

const showTranscriptionExpand = computed(() => {
  const content = props.task?.transcription?.content || ''
  return taskResultNeedsExpansion(content, transcriptionOptions.value)
})

const showSummaryExpand = computed(() => {
  const content = props.task?.summary?.content || ''
  return taskResultNeedsExpansion(content, summaryOptions.value)
})

const transcriptionExpandMeta = computed(() =>
  taskResultExpandMeta(props.task?.transcription?.content || ''),
)
const summaryExpandMeta = computed(() => taskResultExpandMeta(props.task?.summary?.content || ''))

const transcriptionExpandLabel = computed(() =>
  taskResultExpandButtonLabel(transcriptionExpanded.value, props.task?.transcription?.content || ''),
)
const summaryExpandLabel = computed(() =>
  taskResultExpandButtonLabel(summaryExpanded.value, props.task?.summary?.content || ''),
)

const toggleTranscriptionExpand = () => {
  transcriptionExpanded.value = !transcriptionExpanded.value
}
const toggleSummaryExpand = () => {
  summaryExpanded.value = !summaryExpanded.value
}

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
  padding: 0.85rem 1.15rem 0.7rem;
  border-bottom: 1px solid var(--vl-border);
  flex-shrink: 0;
}

.detail-panel.focus .detail-header {
  padding: 1rem 1.5rem 0.85rem;
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
  margin: 0 0 0.3rem;
  font-family: var(--vl-font-display);
  font-size: 1.18rem;
  font-weight: 700;
  color: var(--vl-text);
  line-height: 1.35;
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
  overflow: hidden;
}

.detail-panel.focus .detail-title {
  font-size: 1.35rem;
  -webkit-line-clamp: 3;
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

.header-tools {
  display: flex;
  align-items: center;
  gap: 0.4rem;
  flex-shrink: 0;
}

.tool-btn {
  appearance: none;
  border: 1px solid var(--vl-border);
  background: var(--vl-surface-hover);
  color: var(--vl-text-secondary);
  font-size: 0.8rem;
  font-weight: 600;
  padding: 0.4rem 0.7rem;
  border-radius: var(--vl-radius-sm);
  cursor: pointer;
  white-space: nowrap;
  transition: border-color 0.15s, color 0.15s, background 0.15s;
}

.tool-btn:hover {
  border-color: var(--vl-border-focus);
  color: var(--vl-primary);
  background: var(--vl-primary-dim);
}

.tool-btn.active {
  border-color: var(--vl-border-focus);
  color: var(--vl-primary);
  background: var(--vl-primary-dim);
  box-shadow: inset 0 0 0 1px color-mix(in srgb, var(--vl-primary) 20%, transparent);
}

.tool-btn.chat {
  border-color: color-mix(in srgb, var(--vl-info) 35%, transparent);
  color: var(--vl-info);
  background: var(--vl-info-dim);
}

.tool-btn.chat:hover {
  border-color: color-mix(in srgb, var(--vl-info) 55%, transparent);
  background: color-mix(in srgb, var(--vl-info) 18%, transparent);
}

.tool-btn.ghost {
  background: transparent;
}

.more-wrap {
  position: relative;
}

.more-menu {
  position: absolute;
  top: calc(100% + 0.35rem);
  right: 0;
  min-width: 9.5rem;
  padding: 0.3rem;
  border-radius: var(--vl-radius);
  border: 1px solid var(--vl-border-strong);
  background: var(--vl-panel);
  box-shadow: var(--vl-shadow);
  z-index: 20;
  display: flex;
  flex-direction: column;
  gap: 0.1rem;
}

.more-item {
  appearance: none;
  border: none;
  background: transparent;
  text-align: left;
  padding: 0.5rem 0.65rem;
  border-radius: var(--vl-radius-sm);
  color: var(--vl-text);
  font-size: 0.84rem;
  font-weight: 550;
  cursor: pointer;
}

.more-item:hover:not(:disabled) {
  background: var(--vl-surface-hover);
  color: var(--vl-primary);
}

.more-item.subtle {
  color: var(--vl-text-muted);
  font-weight: 500;
  font-size: 0.8rem;
}

.more-item:disabled {
  opacity: 0.4;
  cursor: not-allowed;
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

/* 空态 / 概览内 CTA 仍用 action-btn */
.action-btn {
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

.action-btn.solid {
  border-color: var(--vl-border-focus);
  color: var(--vl-text-inverse);
  background: linear-gradient(135deg, var(--vl-primary), var(--vl-primary-deep));
}

.action-btn:disabled {
  opacity: 0.4;
  cursor: not-allowed;
}

.detail-tabs {
  display: flex;
  gap: 0.25rem;
  padding: 0.4rem 1rem 0;
  border-bottom: 1px solid var(--vl-border);
  flex-shrink: 0;
  overflow-x: auto;
}

.detail-panel.focus .detail-tabs {
  padding-left: 1.5rem;
  padding-right: 1.5rem;
}

.tab-btn {
  position: relative;
  padding: 0.55rem 0.95rem 0.65rem;
  border: none;
  background: transparent;
  color: var(--vl-text-muted);
  font-weight: 600;
  font-size: 0.86rem;
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
  padding: 1.1rem 1.25rem 1.5rem;
  scrollbar-width: thin;
  scrollbar-color: var(--vl-primary-glow) transparent;
}

.detail-panel.focus .detail-body {
  padding: 1.35rem 1.75rem 2rem;
}

.tab-pane {
  animation: vl-fade-in-up 0.28s var(--vl-ease);
  max-width: 48rem;
}

.detail-panel.focus .tab-pane {
  max-width: 52rem;
  margin: 0 auto;
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
  margin-bottom: 0.9rem;
}

.result-toolbar h4 {
  margin: 0;
  font-size: 1rem;
  font-weight: 700;
  color: var(--vl-text);
  font-family: var(--vl-font-display);
}

.result-actions {
  display: flex;
  gap: 0.4rem;
  flex-wrap: wrap;
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

.icon-btn.subtle {
  opacity: 0.8;
  font-weight: 500;
  border-color: var(--vl-border);
  background: transparent;
  color: var(--vl-text-muted);
}

.icon-btn.subtle:hover:not(:disabled) {
  color: var(--vl-text-secondary);
  border-color: var(--vl-border-strong);
  background: var(--vl-surface-hover);
}

.icon-btn.subtle:disabled {
  opacity: 0.4;
  cursor: not-allowed;
}

/* 文档式阅读：弱化「卡片套卡片」，更像正文纸 */
.result-text {
  margin: 0;
  padding: 0.15rem 0.1rem 0.5rem;
  border: none;
  border-radius: 0;
  background: transparent;
  font-family: var(--vl-font), var(--vl-font-mono);
  font-size: 0.95rem;
  line-height: 1.85;
  letter-spacing: 0.01em;
  white-space: pre-wrap;
  color: var(--vl-text);
  max-height: none;
  overflow: visible;
}

.detail-panel.focus .result-text {
  font-size: 1.02rem;
  line-height: 1.9;
}

.result-markdown {
  padding: 0.15rem 0.1rem 0.5rem;
  border: none;
  border-radius: 0;
  background: transparent;
  line-height: 1.85;
  overflow: visible;
}

.detail-panel.focus .result-markdown {
  font-size: 1.02rem;
}

.result-markdown :deep(h2),
.result-markdown :deep(h3) {
  color: var(--vl-text);
  margin: 1.25rem 0 0.6rem;
  font-weight: 700;
  font-family: var(--vl-font-display);
  line-height: 1.35;
}

.result-markdown :deep(p) {
  margin: 0 0 0.95rem;
  color: var(--vl-text);
  font-size: 0.96rem;
}

.detail-panel.focus .result-markdown :deep(p) {
  font-size: 1.02rem;
}

.result-markdown :deep(strong) {
  color: var(--vl-primary);
  font-weight: 600;
}

.result-markdown :deep(ul) {
  padding-left: 1.35rem;
  margin: 0 0 0.95rem;
}

.result-markdown :deep(li) {
  margin-bottom: 0.45rem;
  color: var(--vl-text);
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

.result-article {
  position: relative;
}

.result-article.collapsed .result-text,
.result-article.collapsed .result-markdown {
  /* 折叠时略压行距，视觉更「摘要」 */
  max-height: none;
}

.result-fade {
  pointer-events: none;
  position: absolute;
  left: 0;
  right: 0;
  bottom: 0;
  height: 4.5rem;
  background: linear-gradient(
    180deg,
    color-mix(in srgb, var(--vl-panel) 0%, transparent) 0%,
    color-mix(in srgb, var(--vl-panel) 55%, transparent) 45%,
    var(--vl-panel) 100%
  );
}

.expand-bar {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: center;
  gap: 0.55rem 0.85rem;
  margin-top: 0.35rem;
  padding: 0.65rem 0 0.15rem;
  border-top: 1px dashed color-mix(in srgb, var(--vl-border) 80%, transparent);
}

.expand-btn {
  display: inline-flex;
  align-items: center;
  gap: 0.4rem;
  width: auto;
  min-width: 10rem;
  margin: 0;
  padding: 0.5rem 1.05rem;
  border-radius: 999px;
  border: 1px solid var(--vl-border-strong);
  background: color-mix(in srgb, var(--vl-surface) 70%, transparent);
  color: var(--vl-text);
  font-weight: 650;
  font-size: 0.84rem;
  cursor: pointer;
  box-shadow: var(--vl-shadow-sm);
  transition: border-color 0.18s, color 0.18s, background 0.18s, transform 0.18s, box-shadow 0.18s;
}

.expand-btn:hover {
  border-color: var(--vl-border-focus);
  color: var(--vl-primary);
  background: var(--vl-primary-dim);
  transform: translateY(-1px);
  box-shadow: 0 4px 14px color-mix(in srgb, var(--vl-primary) 12%, transparent);
}

.expand-btn:active {
  transform: translateY(0);
}

.expand-chevron {
  font-size: 0.78rem;
  line-height: 1;
  opacity: 0.85;
  color: var(--vl-primary);
}

.expand-label {
  letter-spacing: 0.01em;
}

.expand-hint {
  font-size: 0.75rem;
  color: var(--vl-text-muted);
  font-family: var(--vl-font-mono);
  letter-spacing: 0.01em;
}

@media (prefers-reduced-motion: reduce) {
  .expand-btn {
    transition: none;
  }
  .expand-btn:hover {
    transform: none;
  }
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
