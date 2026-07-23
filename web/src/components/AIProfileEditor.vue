<template>
  <div class="ai-profile-editor">
    <!-- List -->
    <div v-if="!isEditing" class="config-list">
      <div v-if="profilesLoading" class="empty-state muted">
        <p>加载配置中…</p>
      </div>
      <div v-else-if="profiles.length === 0" class="empty-state">
        <div class="empty-icon" aria-hidden="true">
          <VlIcon :name="ICON.settings" size="xl" />
        </div>
        <p>还没有配置模型服务</p>
        <p class="empty-hint">转写、总结和对话都依赖 OpenAI 兼容的 BYOK 配置。</p>
        <button type="button" class="btn-amber" @click="startCreate">
          <VlIcon :name="ICON.plus" size="sm" />
          新建配置
        </button>
      </div>
      <div v-else>
        <div class="config-actions">
          <button type="button" class="btn-secondary" @click="startCreate">
            <VlIcon :name="ICON.plus" size="sm" />
            新建配置
          </button>
        </div>
        <div class="profile-grid">
          <div
            v-for="profile in profiles"
            :key="profile.id"
            class="profile-card"
            :class="{
              default: profile.is_default,
              testing: testingId === profile.id,
              'test-ok': testFeedback?.profileId === profile.id && testFeedback.success,
              'test-bad': testFeedback?.profileId === profile.id && !testFeedback.success,
            }"
          >
            <div class="profile-header">
              <h4>{{ profile.name }}</h4>
              <span v-if="profile.is_default" class="active-badge">使用中</span>
            </div>
            <div class="profile-details">
              <div class="detail-row">
                <span class="label">对话</span>
                <span class="value">{{ profile.llm_model || '—' }}</span>
              </div>
              <div class="detail-row">
                <span class="label">语音</span>
                <span class="value">{{ profile.asr_model || '—' }}</span>
              </div>
              <div class="detail-row">
                <span class="label">向量</span>
                <span class="value">{{ profile.embedding_model || '—' }}</span>
              </div>
              <div v-if="profile.vision_model" class="detail-row">
                <span class="label">画面</span>
                <span class="value">{{ profile.vision_model }}</span>
              </div>
            </div>
            <div class="profile-actions">
              <button
                v-if="!profile.is_default"
                type="button"
                class="action-btn"
                :disabled="busy"
                @click="setAsCurrent(profile)"
              >
                使用此配置
              </button>
              <button type="button" class="action-btn" :disabled="busy" @click="startEdit(profile)">
                编辑
              </button>
              <button
                type="button"
                class="action-btn test"
                :class="{ running: testingId === profile.id }"
                :disabled="busy"
                :title="testButtonTitle"
                @click="handleTest(profile)"
              >
                <span v-if="testingId === profile.id" class="btn-spinner" aria-hidden="true"></span>
                {{ testingId === profile.id ? '探测中' : '测试连接' }}
              </button>
              <button type="button" class="action-btn danger" :disabled="busy" @click="handleDelete(profile.id)">
                删除
              </button>
            </div>

            <transition name="test-panel">
              <div
                v-if="testingId === profile.id || (testFeedback && testFeedback.profileId === profile.id)"
                class="test-panel"
                :class="{
                  running: testingId === profile.id,
                  success: testFeedback?.profileId === profile.id && testFeedback.success,
                  error: testFeedback?.profileId === profile.id && !testFeedback.success,
                }"
              >
                <div class="test-panel-head">
                  <span class="test-panel-title">
                    {{
                      testingId === profile.id
                        ? '正在探测服务…'
                        : testFeedback?.success
                          ? '连接正常'
                          : '探测未通过'
                    }}
                  </span>
                  <span v-if="testingId === profile.id" class="test-panel-pulse" aria-hidden="true"></span>
                </div>
                <ul class="test-steps" aria-label="探测项">
                  <li
                    class="test-step"
                    :class="stepClass(profile.id, 'llm')"
                  >
                    <span class="step-icon" aria-hidden="true"></span>
                    <span class="step-label">对话 LLM</span>
                    <span class="step-meta">chat · ping</span>
                  </li>
                  <li
                    class="test-step"
                    :class="stepClass(profile.id, 'embedding')"
                  >
                    <span class="step-icon" aria-hidden="true"></span>
                    <span class="step-label">Embedding</span>
                    <span class="step-meta">向量{{ profile.embedding_dim ? ` · ${profile.embedding_dim}d` : '' }}</span>
                  </li>
                  <li class="test-step skipped">
                    <span class="step-icon" aria-hidden="true"></span>
                    <span class="step-label">ASR 语音</span>
                    <span class="step-meta">需音频 · 跳过</span>
                  </li>
                </ul>
                <p
                  v-if="testFeedback?.profileId === profile.id && testFeedback.message"
                  class="test-panel-msg"
                >
                  {{ testFeedback.message }}
                </p>
              </div>
            </transition>
          </div>
        </div>
      </div>
    </div>

    <!-- Form -->
    <form v-else class="config-form" @submit.prevent="handleSubmit">
      <div class="form-sticky-bar">
        <div class="form-sticky-title">
          {{ isEditMode ? '编辑配置' : '新建配置' }}
          <span class="protocol-tag">OpenAI 兼容</span>
        </div>
        <div class="form-sticky-actions">
          <button type="button" class="btn-secondary" :disabled="loading" @click="cancelEdit">取消</button>
          <button type="submit" class="btn-amber" :disabled="loading">
            {{ loading ? '保存中…' : '保存' }}
          </button>
        </div>
      </div>

      <div class="form-body">
        <section class="form-section meta-row">
          <div class="form-group grow">
            <label for="ai-profile-name">配置名称</label>
            <input
              id="ai-profile-name"
              v-model="formData.name"
              class="form-input"
              placeholder="例如：我的中转 / 生产环境"
              required
            />
          </div>
          <label class="check-row check-inline">
            <input v-model="formData.is_default" type="checkbox" />
            设为当前使用的配置
          </label>
        </section>

        <!-- Primary: LLM (always full) -->
        <section class="form-section service-card">
          <div class="service-head">
            <h3>对话 · LLM</h3>
            <button
              type="button"
              class="btn-ghost-sm"
              :disabled="fetchingModels.llm"
              @click="fetchModels('llm')"
            >
              {{ fetchingModels.llm ? '拉取中…' : '拉取模型' }}
            </button>
          </div>
          <div class="field-grid">
            <div class="form-group">
              <label for="llm-base-url">Base URL</label>
              <input
                id="llm-base-url"
                v-model="formData.llm_base_url"
                class="form-input"
                placeholder="https://api.example.com/v1"
                @blur="onLlmBaseBlur"
              />
            </div>
            <div class="form-group">
              <label for="llm-api-key">API Key {{ isEditMode ? '（留空不改）' : '' }}</label>
              <input
                id="llm-api-key"
                v-model="formData.llm_api_key"
                type="password"
                class="form-input"
                :placeholder="isEditMode ? '保持原有' : 'sk-…'"
                autocomplete="off"
              />
              <div v-if="isEditMode && editingProfile?.llm_api_key_masked" class="masked-key">
                当前 {{ editingProfile.llm_api_key_masked }}
              </div>
            </div>
          </div>
          <div class="form-group">
            <label for="llm-model">模型</label>
            <input
              id="llm-model"
              v-model="formData.llm_model"
              class="form-input"
              placeholder="模型 id，可手输或拉取后点选"
              list="llm-fetched-list"
            />
            <datalist id="llm-fetched-list">
              <option v-for="m in modelOptions.llm" :key="'llm-' + m" :value="m" />
            </datalist>
            <div v-if="modelOptions.llm.length" class="chip-row">
              <button
                v-for="m in chipModels('llm')"
                :key="'chip-llm-' + m"
                type="button"
                class="model-chip"
                :class="{ active: formData.llm_model === m }"
                @click="formData.llm_model = m"
              >
                {{ shortModel(m) }}
              </button>
            </div>
            <p v-if="modelErrors.llm" class="field-error">{{ modelErrors.llm }}</p>
          </div>
        </section>

        <!-- ASR: compact when synced -->
        <section class="form-section service-card compact-ok">
          <div class="service-head">
            <h3>语音 · ASR</h3>
            <div class="service-head-actions">
              <label class="check-row check-tight">
                <input v-model="syncAsrFromLlm" type="checkbox" @change="applyAsrSync" />
                跟随对话 URL/Key
              </label>
              <button
                type="button"
                class="btn-ghost-sm"
                :disabled="fetchingModels.asr"
                @click="fetchModels('asr')"
              >
                {{ fetchingModels.asr ? '…' : '拉取' }}
              </button>
            </div>
          </div>
          <div v-if="!syncAsrFromLlm" class="field-grid">
            <div class="form-group">
              <label for="asr-base-url">Base URL</label>
              <input
                id="asr-base-url"
                v-model="formData.asr_base_url"
                class="form-input"
                placeholder="https://…"
              />
            </div>
            <div class="form-group">
              <label for="asr-api-key">API Key {{ isEditMode ? '（留空不改）' : '' }}</label>
              <input
                id="asr-api-key"
                v-model="formData.asr_api_key"
                type="password"
                class="form-input"
                :placeholder="isEditMode ? '保持原有' : 'sk-…'"
                autocomplete="off"
              />
              <div v-if="isEditMode && editingProfile?.asr_api_key_masked" class="masked-key">
                当前 {{ editingProfile.asr_api_key_masked }}
              </div>
            </div>
          </div>
          <div v-else class="sync-banner">使用与对话相同的 Base URL / Key，只需填 ASR 模型</div>
          <div class="form-group form-group-tight">
            <label for="asr-model">ASR 模型</label>
            <input
              id="asr-model"
              v-model="formData.asr_model"
              class="form-input"
              placeholder="ASR 模型 id"
              list="asr-fetched-list"
            />
            <datalist id="asr-fetched-list">
              <option v-for="m in modelOptions.asr" :key="'asr-' + m" :value="m" />
            </datalist>
            <div v-if="modelOptions.asr.length" class="chip-row">
              <button
                v-for="m in chipModels('asr')"
                :key="'chip-asr-' + m"
                type="button"
                class="model-chip"
                :class="{ active: formData.asr_model === m }"
                @click="formData.asr_model = m"
              >
                {{ shortModel(m) }}
              </button>
            </div>
            <p v-if="modelErrors.asr" class="field-error">{{ modelErrors.asr }}</p>
          </div>
        </section>

        <!-- Embedding: compact when synced -->
        <section class="form-section service-card compact-ok">
          <div class="service-head">
            <h3>向量 · Embedding</h3>
            <div class="service-head-actions">
              <label class="check-row check-tight">
                <input v-model="syncEmbFromLlm" type="checkbox" @change="applyEmbSync" />
                跟随对话 Key / 推导 Endpoint
              </label>
              <button
                type="button"
                class="btn-ghost-sm"
                :disabled="fetchingModels.embedding"
                @click="fetchModels('embedding')"
              >
                {{ fetchingModels.embedding ? '…' : '拉取' }}
              </button>
            </div>
          </div>
          <div v-if="!syncEmbFromLlm" class="field-grid">
            <div class="form-group">
              <label for="embedding-endpoint">Endpoint</label>
              <input
                id="embedding-endpoint"
                v-model="formData.embedding_endpoint"
                class="form-input"
                placeholder="…/v1/embeddings"
              />
            </div>
            <div class="form-group">
              <label for="embedding-api-key">API Key {{ isEditMode ? '（留空不改）' : '' }}</label>
              <input
                id="embedding-api-key"
                v-model="formData.embedding_api_key"
                type="password"
                class="form-input"
                :placeholder="isEditMode ? '保持原有' : 'sk-…'"
                autocomplete="off"
              />
              <div
                v-if="isEditMode && editingProfile?.embedding_api_key_masked"
                class="masked-key"
              >
                当前 {{ editingProfile.embedding_api_key_masked }}
              </div>
            </div>
          </div>
          <div v-else class="sync-banner">
            Endpoint 将由对话 Base URL 推导为 <code>…/embeddings</code>，Key 跟随对话
          </div>
          <div class="emb-model-dim">
            <div class="form-group form-group-tight emb-model-col">
              <label for="embedding-model">Embedding 模型</label>
              <input
                id="embedding-model"
                v-model="formData.embedding_model"
                class="form-input"
                placeholder="embedding 模型 id"
                list="emb-fetched-list"
              />
              <datalist id="emb-fetched-list">
                <option v-for="m in modelOptions.embedding" :key="'emb-' + m" :value="m" />
              </datalist>
              <div v-if="modelOptions.embedding.length" class="chip-row">
                <button
                  v-for="m in chipModels('embedding')"
                  :key="'chip-emb-' + m"
                  type="button"
                  class="model-chip"
                  :class="{ active: formData.embedding_model === m }"
                  @click="formData.embedding_model = m"
                >
                  {{ shortModel(m) }}
                </button>
              </div>
              <p v-if="modelErrors.embedding" class="field-error">{{ modelErrors.embedding }}</p>
            </div>
            <div class="form-group form-group-tight emb-dim-col">
              <label for="embedding-dim">维度</label>
              <div class="dim-row">
                <input
                  id="embedding-dim"
                  v-model.number="formData.embedding_dim"
                  type="number"
                  min="1"
                  inputmode="numeric"
                  class="form-input dim-input"
                  placeholder="如 1024"
                />
                <button
                  type="button"
                  class="btn-ghost-sm dim-probe-btn"
                  :disabled="probingDim"
                  title="调用一次 embeddings，用向量长度作为维度"
                  @click="probeEmbeddingDim"
                >
                  {{ probingDim ? '检测中…' : '自动检测' }}
                </button>
              </div>
              <p v-if="dimProbeMsg" class="field-hint" :class="{ error: dimProbeIsError }">
                {{ dimProbeMsg }}
              </p>
            </div>
          </div>
        </section>

        <!-- Advanced: Vision -->
        <section class="form-section advanced-block">
          <button type="button" class="advanced-toggle" @click="advancedOpen = !advancedOpen">
            {{ advancedOpen ? '收起高级' : '高级 · Vision（可选）' }}
          </button>
          <div v-if="advancedOpen" class="advanced-body">
            <p class="form-hint">画面理解；不支持可留空（OCR 降级）。</p>
            <div class="field-grid">
              <div class="form-group">
                <label for="vision-base-url">Base URL</label>
                <input
                  id="vision-base-url"
                  v-model="formData.vision_base_url"
                  class="form-input"
                  placeholder="可与对话相同"
                />
              </div>
              <div class="form-group">
                <label for="vision-api-key">API Key</label>
                <input
                  id="vision-api-key"
                  v-model="formData.vision_api_key"
                  type="password"
                  class="form-input"
                  autocomplete="off"
                />
              </div>
            </div>
            <div class="form-group">
              <label for="vision-model">模型</label>
              <div class="inline-fetch">
                <input
                  id="vision-model"
                  v-model="formData.vision_model"
                  class="form-input"
                  placeholder="留空 = 不用 Vision"
                  list="vision-fetched-list"
                />
                <button
                  type="button"
                  class="btn-ghost-sm"
                  :disabled="fetchingModels.vision"
                  @click="fetchModels('vision')"
                >
                  {{ fetchingModels.vision ? '…' : '拉取' }}
                </button>
              </div>
              <datalist id="vision-fetched-list">
                <option v-for="m in modelOptions.vision" :key="'vis-' + m" :value="m" />
              </datalist>
              <p v-if="modelErrors.vision" class="field-error">{{ modelErrors.vision }}</p>
            </div>
          </div>
        </section>
      </div>

      <div v-if="testResult" class="test-result" :class="testResult.success ? 'success' : 'error'">
        <h4>{{ testResult.success ? '测试成功' : '测试失败' }}</h4>
        <p>{{ testResult.message }}</p>
      </div>
    </form>
  </div>
</template>

<script setup>
import { computed, inject, onMounted, reactive, ref, watch } from 'vue'
import api from '../api'
import { ICON } from '../icons.js'
import VlIcon from './VlIcon.vue'
import { normalizeListResponse } from '../apiEnvelope.js'
import {
  PRODUCT_PROVIDER,
  createDefaultAIProfileForm,
  deriveEmbeddingEndpoint,
  normalizeFormForSave,
  profileToFormData,
} from '../aiProviderPresets.js'

const emit = defineEmits(['updated'])
const app = inject('appCtx', null)

const profiles = ref([])
const profilesLoading = ref(false)
const isEditing = ref(false)
const isEditMode = ref(false)
const editingProfile = ref(null)
const loading = ref(false)
const testingId = ref(null)
/** 列表卡片上的就近反馈 */
const testFeedback = ref(null)
/** 编辑表单底部反馈（保留兼容） */
const testResult = ref(null)
const formData = ref(createDefaultAIProfileForm())
const advancedOpen = ref(false)

const busy = computed(() => loading.value || testingId.value != null)
const testButtonTitle = '探测对话 LLM 与 Embedding 是否可用。ASR 需音频，本次跳过。'

const stepClass = (profileId) => {
  if (testingId.value === profileId) return 'pending'
  const fb = testFeedback.value
  if (!fb || fb.profileId !== profileId) return ''
  return fb.success ? 'ok' : 'fail'
}

const syncAsrFromLlm = ref(false)
const syncEmbFromLlm = ref(false)

const modelOptions = reactive({ llm: [], asr: [], embedding: [], vision: [] })
const modelErrors = reactive({ llm: '', asr: '', embedding: '', vision: '' })
const fetchingModels = reactive({ llm: false, asr: false, embedding: false, vision: false })
const probingDim = ref(false)
const dimProbeMsg = ref('')
const dimProbeIsError = ref(false)

const prompt = (opts) => {
  if (app?.openConfirm) {
    app.openConfirm(opts)
    return
  }
  if (!opts.showCancel) {
    window.alert(opts.message || opts.title || '')
    return
  }
  if (window.confirm(opts.message || opts.title || '') && opts.onConfirm) opts.onConfirm()
}

const shortModel = (m) => {
  const s = String(m || '')
  if (s.length <= 28) return s
  const parts = s.split('/')
  return parts[parts.length - 1] || s.slice(0, 28)
}

const chipModels = (kind) => {
  // 只展示已拉取的列表，避免未拉时一堆预设 chips 撑高页面
  const fetched = modelOptions[kind] || []
  return fetched.slice(0, 10)
}

const loadProfiles = async () => {
  profilesLoading.value = true
  try {
    profiles.value = normalizeListResponse(await api.getAIProfiles())
  } catch (err) {
    console.error(err)
    prompt({
      title: '加载失败',
      message: err.message || '无法加载模型配置',
      confirmText: '知道了',
      showCancel: false,
      type: 'danger',
      icon: ICON.xCircle,
    })
  } finally {
    profilesLoading.value = false
  }
}

const resetModelUi = () => {
  for (const k of ['llm', 'asr', 'embedding', 'vision']) {
    modelOptions[k] = []
    modelErrors[k] = ''
    fetchingModels[k] = false
  }
}

const startCreate = () => {
  isEditing.value = true
  isEditMode.value = false
  editingProfile.value = null
  formData.value = createDefaultAIProfileForm()
  // 默认「跟随对话」：同中转同 Key 时一屏可填完；不同中转再取消勾选
  syncAsrFromLlm.value = true
  syncEmbFromLlm.value = true
  advancedOpen.value = false
  testResult.value = null
  resetModelUi()
  applyAsrSync()
  applyEmbSync()
}

const startEdit = (profile) => {
  isEditing.value = true
  isEditMode.value = true
  editingProfile.value = profile
  formData.value = profileToFormData(profile)
  const sameAsrUrl =
    (profile.asr_base_url || '') === (profile.llm_base_url || '')
  syncAsrFromLlm.value = sameAsrUrl
  // Keys are masked — never auto-sync embedding key on edit
  syncEmbFromLlm.value = false
  advancedOpen.value = !!(profile.vision_provider || profile.vision_model)
  testResult.value = null
  resetModelUi()
}

const cancelEdit = () => {
  isEditing.value = false
  editingProfile.value = null
}

/** 列表一键切换当前使用的配置（后端 is_default） */
const setAsCurrent = async (profile) => {
  if (!profile?.id || profile.is_default) return
  loading.value = true
  try {
    const form = profileToFormData(profile)
    form.is_default = true
    // 编辑态不传 Key → 服务端保留原密文
    const payload = normalizeFormForSave(form, {
      keepLegacyProviders: ['mimo', 'siliconflow'].includes(
        String(profile.llm_provider || '').toLowerCase(),
      ),
    })
    delete payload.asr_api_key
    delete payload.llm_api_key
    delete payload.embedding_api_key
    delete payload.vision_api_key
    await api.updateAIProfile(profile.id, payload)
    await loadProfiles()
    emit('updated')
    app?.showToast?.(`已切换为「${profile.name}」`)
  } catch (err) {
    prompt({
      title: '切换失败',
      message: err.message || '无法设为当前配置',
      confirmText: '知道了',
      showCancel: false,
      type: 'danger',
      icon: ICON.xCircle,
    })
  } finally {
    loading.value = false
  }
}

const applyAsrSync = () => {
  if (!syncAsrFromLlm.value) return
  formData.value.asr_base_url = formData.value.llm_base_url
  formData.value.asr_api_key = formData.value.llm_api_key
  formData.value.asr_provider = formData.value.llm_provider || PRODUCT_PROVIDER
}

const applyEmbSync = () => {
  if (!syncEmbFromLlm.value) return
  formData.value.embedding_endpoint = deriveEmbeddingEndpoint(formData.value.llm_base_url)
  formData.value.embedding_api_key = formData.value.llm_api_key
  formData.value.embedding_provider = formData.value.llm_provider || PRODUCT_PROVIDER
}

const onLlmBaseBlur = () => {
  if (syncAsrFromLlm.value) applyAsrSync()
  if (syncEmbFromLlm.value) applyEmbSync()
}

watch(
  () => formData.value.llm_base_url,
  () => {
    if (syncAsrFromLlm.value) formData.value.asr_base_url = formData.value.llm_base_url
    if (syncEmbFromLlm.value) {
      formData.value.embedding_endpoint = deriveEmbeddingEndpoint(formData.value.llm_base_url)
    }
  },
)

watch(
  () => formData.value.llm_api_key,
  () => {
    if (syncAsrFromLlm.value) formData.value.asr_api_key = formData.value.llm_api_key
    if (syncEmbFromLlm.value) formData.value.embedding_api_key = formData.value.llm_api_key
  },
)

const resolveListPayload = (purpose) => {
  const fd = formData.value
  const payload = { purpose }
  if (purpose === 'llm') {
    payload.base_url = fd.llm_base_url
    payload.api_key = fd.llm_api_key
  } else if (purpose === 'asr') {
    payload.base_url = syncAsrFromLlm.value ? fd.llm_base_url : fd.asr_base_url
    payload.api_key = syncAsrFromLlm.value ? fd.llm_api_key : fd.asr_api_key
  } else if (purpose === 'embedding') {
    payload.base_url = syncEmbFromLlm.value
      ? deriveEmbeddingEndpoint(fd.llm_base_url)
      : fd.embedding_endpoint
    payload.api_key = syncEmbFromLlm.value ? fd.llm_api_key : fd.embedding_api_key
  } else {
    payload.base_url = fd.vision_base_url || fd.llm_base_url
    payload.api_key = fd.vision_api_key || fd.llm_api_key
  }
  if (isEditMode.value && editingProfile.value?.id && !payload.api_key) {
    payload.profile_id = editingProfile.value.id
  }
  return payload
}

const fetchModels = async (purpose) => {
  modelErrors[purpose] = ''
  fetchingModels[purpose] = true
  try {
    const res = await api.listAIModels(resolveListPayload(purpose))
    const list = Array.isArray(res?.models) ? res.models : Array.isArray(res) ? res : []
    modelOptions[purpose] = list
    if (!list.length) {
      modelErrors[purpose] = '未返回模型，可手动输入模型 id'
    }
  } catch (err) {
    modelOptions[purpose] = []
    modelErrors[purpose] = err.message || '拉取失败，请手输模型 id'
  } finally {
    fetchingModels[purpose] = false
  }
}

const resolveEmbeddingProbePayload = () => {
  const fd = formData.value
  const payload = {
    endpoint: syncEmbFromLlm.value
      ? deriveEmbeddingEndpoint(fd.llm_base_url)
      : fd.embedding_endpoint,
    api_key: syncEmbFromLlm.value ? fd.llm_api_key : fd.embedding_api_key,
    model: fd.embedding_model,
  }
  if (isEditMode.value && editingProfile.value?.id && !payload.api_key) {
    payload.profile_id = editingProfile.value.id
  }
  return payload
}

const probeEmbeddingDim = async () => {
  dimProbeMsg.value = ''
  dimProbeIsError.value = false
  if (syncEmbFromLlm.value) applyEmbSync()
  const payload = resolveEmbeddingProbePayload()
  if (!payload.endpoint?.trim()) {
    dimProbeIsError.value = true
    dimProbeMsg.value = '请先填写 Embedding Endpoint（或勾选跟随对话）'
    return
  }
  if (!payload.model?.trim()) {
    dimProbeIsError.value = true
    dimProbeMsg.value = '请先填写 Embedding 模型'
    return
  }
  if (!payload.api_key && !payload.profile_id) {
    dimProbeIsError.value = true
    dimProbeMsg.value = '请填写 API Key（编辑已保存配置时可留空用原 Key）'
    return
  }
  probingDim.value = true
  try {
    const res = await api.probeEmbeddingDim(payload)
    const dim = Number(res?.dimension ?? res?.dim)
    if (!Number.isFinite(dim) || dim <= 0) {
      throw new Error('未返回有效维度')
    }
    formData.value.embedding_dim = dim
    dimProbeIsError.value = false
    dimProbeMsg.value = `已检测：${dim} 维`
  } catch (err) {
    dimProbeIsError.value = true
    dimProbeMsg.value = err.message || '检测失败，请手填维度'
  } finally {
    probingDim.value = false
  }
}

const handleSubmit = async () => {
  if (syncAsrFromLlm.value) applyAsrSync()
  if (syncEmbFromLlm.value) applyEmbSync()

  if (!formData.value.name?.trim()) {
    prompt({
      title: '提示',
      message: '请输入配置名称',
      confirmText: '知道了',
      showCancel: false,
      type: 'warning',
      icon: ICON.fileText,
    })
    return
  }

  // Vision: only send if model set
  if (formData.value.vision_model?.trim()) {
    formData.value.vision_provider = formData.value.vision_provider || PRODUCT_PROVIDER
    if (!formData.value.vision_base_url?.trim()) {
      formData.value.vision_base_url = formData.value.llm_base_url
    }
  } else {
    formData.value.vision_provider = ''
    formData.value.vision_base_url = ''
    formData.value.vision_api_key = ''
    formData.value.vision_model = ''
  }

  // Keep legacy provider ids when editing existing non-compatible profiles
  const keepLegacy =
    isEditMode.value &&
    editingProfile.value &&
    ['mimo', 'siliconflow'].includes(
      String(editingProfile.value.llm_provider || '').toLowerCase(),
    )

  const payload = normalizeFormForSave({ ...formData.value }, { keepLegacyProviders: keepLegacy })

  if (!payload.vision_provider) {
    payload.vision_provider = ''
    payload.vision_base_url = ''
    payload.vision_model = ''
    payload.vision_api_key = ''
  }

  if (isEditMode.value) {
    if (!payload.asr_api_key) delete payload.asr_api_key
    if (!payload.llm_api_key) delete payload.llm_api_key
    if (!payload.embedding_api_key) delete payload.embedding_api_key
    if (!payload.vision_api_key) delete payload.vision_api_key
  }

  loading.value = true
  try {
    if (isEditMode.value) {
      await api.updateAIProfile(editingProfile.value.id, payload)
    } else {
      await api.createAIProfile(payload)
    }
    await loadProfiles()
    cancelEdit()
    emit('updated')
    app?.showToast?.('配置已更新')
  } catch (err) {
    prompt({
      title: '保存失败',
      message: err.message || '保存失败',
      confirmText: '知道了',
      showCancel: false,
      type: 'danger',
      icon: ICON.xCircle,
    })
  } finally {
    loading.value = false
  }
}

const handleTest = async (profile) => {
  if (!profile?.id || testingId.value != null) return
  testFeedback.value = null
  testResult.value = null
  testingId.value = profile.id
  try {
    await api.testAIProfile({ id: profile.id })
    const dimHint = profile.embedding_dim ? `，维度 ${profile.embedding_dim}` : ''
    const message = `对话与向量服务可用${dimHint}。ASR 未测。`
    testFeedback.value = { profileId: profile.id, success: true, message }
    app?.showToast?.('配置探测通过')
  } catch (err) {
    const message = err.message || '探测失败，请检查 URL / Key / 模型'
    testFeedback.value = { profileId: profile.id, success: false, message }
    app?.showToast?.(message, true)
  } finally {
    testingId.value = null
  }
}

const handleDelete = (id) => {
  prompt({
    title: '确认删除',
    message: '此操作不可恢复，确定删除该模型配置吗？',
    confirmText: '删除',
    showCancel: true,
    type: 'danger',
    icon: ICON.trash,
    onConfirm: async () => {
      try {
        await api.deleteAIProfile(id)
        await loadProfiles()
        emit('updated')
        app?.showToast?.('已删除配置')
      } catch (err) {
        prompt({
          title: '删除失败',
          message: err.message || '删除失败',
          confirmText: '知道了',
          showCancel: false,
          type: 'danger',
          icon: ICON.xCircle,
        })
      }
    },
  })
}

onMounted(() => {
  loadProfiles()
})

defineExpose({ loadProfiles, startCreate })
</script>

<style scoped>
/* temporary fragment used to rebuild Vue scoped styles */
.ai-profile-editor {
  width: 100%;
}

.empty-state {
  text-align: center;
  padding: 2rem 1rem;
}
.empty-state.muted p {
  color: var(--vl-text-muted);
}
.empty-icon {
  width: 3.25rem;
  height: 3.25rem;
  margin: 0 auto 0.65rem;
  border-radius: 1rem;
  display: grid;
  place-items: center;
  color: var(--vl-primary);
  background: linear-gradient(145deg, var(--vl-primary-dim), var(--vl-info-dim));
  border: 1px solid var(--vl-primary-glow);
}
.empty-state p {
  margin: 0 0 0.4rem;
  color: var(--vl-text-secondary);
}
.empty-hint {
  font-size: 0.84rem !important;
  color: var(--vl-text-muted) !important;
  margin-bottom: 1.25rem !important;
}

.config-actions {
  margin-bottom: 1rem;
}

.profile-grid {
  display: grid;
  gap: 0.85rem;
  grid-template-columns: repeat(auto-fill, minmax(280px, 1fr));
}

.profile-card {
  border: 1px solid var(--vl-border-strong);
  border-radius: 0.9rem;
  padding: 1rem 1.1rem;
  background: var(--vl-surface);
  transition: border-color 0.25s ease, box-shadow 0.25s ease, transform 0.2s ease;
}
.profile-card.default {
  border-color: var(--vl-border-focus);
}
.profile-card.testing {
  border-color: color-mix(in srgb, var(--vl-info) 35%, transparent);
  box-shadow: 0 0 0 1px var(--vl-info-dim), 0 8px 28px var(--vl-info-dim);
}
.profile-card.test-ok {
  border-color: color-mix(in srgb, var(--vl-success) 35%, transparent);
  box-shadow: 0 0 0 1px var(--vl-success-dim);
}
.profile-card.test-bad {
  border-color: color-mix(in srgb, var(--vl-danger) 50%, transparent);
  box-shadow: 0 0 0 1px var(--vl-danger-dim);
}
.profile-header {
  display: flex;
  justify-content: space-between;
  gap: 0.5rem;
  margin-bottom: 0.65rem;
}
.profile-header h4 {
  margin: 0;
  font-size: 1rem;
  color: var(--vl-text);
}
.default-badge,
.active-badge {
  font-size: 0.72rem;
  font-weight: 600;
  color: var(--vl-primary);
  border: 1px solid var(--vl-primary-glow);
  border-radius: 999px;
  padding: 0.15rem 0.55rem;
  white-space: nowrap;
}
.detail-row {
  display: flex;
  gap: 0.65rem;
  font-size: 0.84rem;
  margin-bottom: 0.3rem;
}
.detail-row .label {
  color: var(--vl-text-muted);
  min-width: 2.5rem;
}
.detail-row .value {
  color: var(--vl-text-secondary);
  font-family: var(--vl-font-mono);
  word-break: break-all;
}
.profile-actions {
  display: flex;
  gap: 0.45rem;
  margin-top: 0.75rem;
  flex-wrap: wrap;
}
.action-btn.test.running {
  color: var(--vl-info);
  border-color: color-mix(in srgb, var(--vl-info) 35%, transparent);
  background: var(--vl-info-dim);
}
.btn-spinner {
  width: 0.7rem;
  height: 0.7rem;
  border: 1.5px solid var(--vl-info-dim);
  border-top-color: var(--vl-info);
  border-radius: 50%;
  display: inline-block;
  margin-right: 0.35rem;
  vertical-align: -0.1rem;
  animation: spin 0.7s linear infinite;
}
@keyframes spin {
  to { transform: rotate(360deg); }
}

.test-panel {
  margin-top: 0.75rem;
  padding: 0.7rem 0.75rem;
  border-radius: 0.7rem;
  border: 1px solid var(--vl-border);
  background: var(--vl-surface);
  overflow: hidden;
}
.test-panel.running {
  border-color: color-mix(in srgb, var(--vl-info) 35%, transparent);
  background: linear-gradient(135deg, var(--vl-info-dim), var(--vl-surface));
}
.test-panel.success {
  border-color: color-mix(in srgb, var(--vl-success) 35%, transparent);
  background: linear-gradient(135deg, var(--vl-success-dim), var(--vl-surface));
}
.test-panel.error {
  border-color: color-mix(in srgb, var(--vl-danger) 35%, transparent);
  background: linear-gradient(135deg, var(--vl-danger-dim), var(--vl-surface));
}
.test-panel-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 0.5rem;
  margin-bottom: 0.55rem;
}
.test-panel-title {
  font-size: 0.82rem;
  font-weight: 650;
  color: var(--vl-text);
  letter-spacing: 0.02em;
}
.test-panel.success .test-panel-title { color: var(--vl-success); }
.test-panel.error .test-panel-title { color: var(--vl-danger); }
.test-panel.running .test-panel-title { color: var(--vl-info); }
.test-panel-pulse {
  width: 0.5rem;
  height: 0.5rem;
  border-radius: 50%;
  background: var(--vl-info);
  box-shadow: 0 0 0 0 color-mix(in srgb, var(--vl-info) 35%, transparent);
  animation: pulse 1.2s ease-out infinite;
}
@keyframes pulse {
  0% { box-shadow: 0 0 0 0 color-mix(in srgb, var(--vl-info) 50%, transparent); }
  70% { box-shadow: 0 0 0 8px transparent; }
  100% { box-shadow: 0 0 0 0 transparent; }
}
.test-steps {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 0.35rem;
}
.test-step {
  display: grid;
  grid-template-columns: 0.85rem 1fr auto;
  align-items: center;
  gap: 0.45rem;
  font-size: 0.76rem;
  color: var(--vl-text-secondary);
  padding: 0.28rem 0.2rem;
  border-radius: 0.4rem;
}
.step-icon {
  width: 0.55rem;
  height: 0.55rem;
  border-radius: 50%;
  border: 1.5px solid var(--vl-border-strong);
  background: transparent;
  justify-self: center;
}
.test-step.pending .step-icon {
  border-color: var(--vl-info);
  border-top-color: transparent;
  animation: spin 0.8s linear infinite;
  border-radius: 50%;
}
.test-step.ok .step-icon {
  border-color: var(--vl-success);
  background: var(--vl-success);
  box-shadow: 0 0 0 3px var(--vl-success-dim);
}
.test-step.fail .step-icon {
  border-color: var(--vl-danger);
  background: var(--vl-danger);
}
.test-step.skipped {
  opacity: 0.55;
}
.test-step.skipped .step-icon {
  border-style: dashed;
  background: transparent;
}
.step-label { font-weight: 550; color: var(--vl-text-secondary); }
.test-step.ok .step-label { color: var(--vl-success); }
.test-step.fail .step-label { color: var(--vl-danger); }
.step-meta {
  font-family: var(--vl-font-mono);
  font-size: 0.68rem;
  color: var(--vl-text-muted);
}
.test-panel-msg {
  margin: 0.55rem 0 0;
  font-size: 0.76rem;
  line-height: 1.45;
  color: var(--vl-text-muted);
}
.test-panel.error .test-panel-msg { color: var(--vl-danger); }
.test-panel.success .test-panel-msg { color: var(--vl-text-secondary); }

.test-panel-enter-active,
.test-panel-leave-active {
  transition: opacity 0.22s ease, transform 0.22s ease, max-height 0.28s ease;
}
.test-panel-enter-from,
.test-panel-leave-to {
  opacity: 0;
  transform: translateY(-4px);
}

@media (prefers-reduced-motion: reduce) {
  .btn-spinner,
  .test-step.pending .step-icon,
  .test-panel-pulse {
    animation: none;
  }
  .profile-card,
  .test-panel-enter-active,
  .test-panel-leave-active {
    transition: none;
  }
}
.action-btn {
  flex: 1;
  min-width: 4rem;
  border: 1px solid var(--vl-border);
  background: var(--vl-surface-hover);
  color: var(--vl-text-secondary);
  border-radius: 0.55rem;
  padding: 0.45rem 0.6rem;
  font-size: 0.84rem;
  cursor: pointer;
}
.action-btn.test { color: var(--vl-info); }
.action-btn.danger { color: var(--vl-danger); }
.action-btn:hover:not(:disabled) { border-color: var(--vl-border-focus); }

.config-form {
  display: flex;
  flex-direction: column;
}

.form-sticky-bar {
  position: sticky;
  top: var(--vl-nav-h, 3.5rem);
  z-index: 5;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 0.75rem;
  flex-wrap: wrap;
  padding: 0.65rem 0;
  margin-bottom: 0.35rem;
  background: linear-gradient(180deg, var(--vl-panel, var(--vl-bg-elevated)) 75%, transparent);
  backdrop-filter: blur(8px);
}
.form-sticky-title {
  font-weight: 650;
  color: var(--vl-text);
  display: flex;
  align-items: center;
  gap: 0.5rem;
  font-size: 0.98rem;
}
.protocol-tag {
  font-size: 0.68rem;
  font-weight: 600;
  color: var(--vl-primary);
  border: 1px solid var(--vl-primary-glow);
  border-radius: 999px;
  padding: 0.12rem 0.45rem;
  font-family: var(--vl-font-mono);
}
.form-sticky-actions { display: flex; gap: 0.5rem; }

.form-body {
  display: flex;
  flex-direction: column;
  gap: 0.7rem;
  padding-bottom: 1.5rem;
}

.form-section {
  border: 1px solid var(--vl-border);
  border-radius: 0.85rem;
  padding: 0.85rem 1rem;
  background: var(--vl-surface);
}
.form-section h3 {
  margin: 0;
  font-size: 0.92rem;
  color: var(--vl-primary);
  font-weight: 600;
}

.meta-row {
  display: flex;
  align-items: flex-end;
  gap: 1rem;
  flex-wrap: wrap;
}
.meta-row .grow {
  flex: 1;
  min-width: 12rem;
  margin-bottom: 0;
}
.check-inline {
  margin-bottom: 0.35rem;
  white-space: nowrap;
}

.service-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 0.65rem;
  margin-bottom: 0.55rem;
  flex-wrap: wrap;
}
.service-head-actions {
  display: flex;
  align-items: center;
  gap: 0.65rem;
  flex-wrap: wrap;
}

.field-grid {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 0.65rem 0.85rem;
}
.emb-model-dim {
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(11.5rem, 14rem);
  gap: 0.65rem 0.85rem;
  align-items: start;
}
.dim-row {
  display: flex;
  align-items: center;
  gap: 0.4rem;
  min-width: 0;
}
.dim-input {
  flex: 1 1 auto;
  min-width: 4.5rem;
  max-width: 7rem;
  /* 避免 number spinner 把旁边按钮挤没；仍可用键盘改数字 */
  appearance: textfield;
  -moz-appearance: textfield;
}
.dim-input::-webkit-outer-spin-button,
.dim-input::-webkit-inner-spin-button {
  -webkit-appearance: none;
  margin: 0;
}
.dim-probe-btn {
  flex: 0 0 auto;
  white-space: nowrap;
}
@media (max-width: 720px) {
  .field-grid,
  .emb-model-dim {
    grid-template-columns: 1fr;
  }
  .dim-input {
    max-width: none;
  }
}

.form-group { margin-bottom: 0.55rem; }
.form-group-tight { margin-bottom: 0; }
.form-group label {
  display: block;
  font-size: 0.78rem;
  color: var(--vl-text-secondary);
  margin-bottom: 0.25rem;
  font-weight: 500;
}
.form-input {
  width: 100%;
  box-sizing: border-box;
  background: var(--vl-bg-elevated);
  border: 1px solid var(--vl-border);
  border-radius: 0.6rem;
  padding: 0.55rem 0.75rem;
  color: var(--vl-text);
  font-size: 0.88rem;
  outline: none;
}
.form-input:focus {
  border-color: var(--vl-primary);
  box-shadow: 0 0 0 3px var(--vl-primary-dim);
}
.form-input:disabled { opacity: 0.55; cursor: not-allowed; }
.masked-key {
  margin-top: 0.25rem;
  font-size: 0.72rem;
  color: var(--vl-text-muted);
  font-family: var(--vl-font-mono);
}
.check-row {
  display: flex;
  align-items: center;
  gap: 0.4rem;
  font-size: 0.8rem;
  color: var(--vl-text-secondary);
  cursor: pointer;
  user-select: none;
}
.check-tight { margin: 0; font-size: 0.76rem; }

.sync-banner {
  font-size: 0.78rem;
  color: var(--vl-text-muted);
  background: var(--vl-primary-dim);
  border: 1px dashed var(--vl-primary-glow);
  border-radius: 0.5rem;
  padding: 0.45rem 0.65rem;
  margin-bottom: 0.55rem;
  line-height: 1.4;
}
.sync-banner code {
  font-family: var(--vl-font-mono);
  color: var(--vl-primary);
  font-size: 0.74rem;
}

.chip-row {
  display: flex;
  flex-wrap: wrap;
  gap: 0.3rem;
  margin-top: 0.4rem;
  max-height: 4.5rem;
  overflow-y: auto;
}
.model-chip {
  appearance: none;
  border: 1px solid var(--vl-border);
  background: var(--vl-surface);
  color: var(--vl-text-secondary);
  border-radius: 999px;
  padding: 0.22rem 0.5rem;
  font-size: 0.7rem;
  font-family: var(--vl-font-mono);
  cursor: pointer;
  max-width: 100%;
  overflow: hidden;
  text-overflow: ellipsis;
}
.model-chip.active,
.model-chip:hover {
  border-color: var(--vl-border-focus);
  color: var(--vl-primary);
}
.field-error {
  margin: 0.3rem 0 0;
  font-size: 0.76rem;
  color: var(--vl-danger);
}
.field-hint {
  margin: 0.3rem 0 0;
  font-size: 0.76rem;
  color: var(--vl-text-muted);
}
.field-hint.error {
  color: var(--vl-danger);
}

.btn-amber {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 0.35rem;
  background: var(--vl-primary);
  color: var(--vl-text-inverse);
  border: none;
  padding: 0.55rem 1.1rem;
  border-radius: 0.65rem;
  font-weight: 600;
  cursor: pointer;
  font-size: 0.88rem;
}
.btn-amber:disabled { opacity: 0.5; cursor: not-allowed; }
.btn-secondary {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 0.35rem;
  background: transparent;
  border: 1px solid var(--vl-border-strong);
  color: var(--vl-text-secondary);
  padding: 0.55rem 1.1rem;
  border-radius: 0.65rem;
  font-weight: 600;
  cursor: pointer;
  font-size: 0.88rem;
}
.btn-ghost-sm {
  appearance: none;
  border: 1px dashed var(--vl-border-strong);
  background: transparent;
  color: var(--vl-text-muted);
  font-size: 0.72rem;
  padding: 0.28rem 0.55rem;
  border-radius: 999px;
  cursor: pointer;
  white-space: nowrap;
}
.btn-ghost-sm:hover:not(:disabled) {
  color: var(--vl-primary);
  border-color: var(--vl-border-focus);
}
.btn-ghost-sm:disabled { opacity: 0.5; cursor: not-allowed; }

.advanced-block { padding: 0.55rem 0.85rem; }
.advanced-toggle {
  width: 100%;
  appearance: none;
  border: none;
  background: transparent;
  color: var(--vl-text-secondary);
  text-align: left;
  cursor: pointer;
  font-size: 0.86rem;
  font-weight: 600;
  padding: 0.3rem 0;
}
.advanced-body { margin-top: 0.55rem; }
.form-hint {
  margin: 0 0 0.55rem;
  font-size: 0.76rem;
  color: var(--vl-text-muted);
  line-height: 1.4;
}
.inline-fetch {
  display: flex;
  gap: 0.45rem;
  align-items: center;
}
.inline-fetch .form-input { flex: 1; }

.test-result {
  margin-top: 0.65rem;
  padding: 0.75rem 0.9rem;
  border-radius: 0.75rem;
}
.test-result.success {
  border: 1px solid color-mix(in srgb, var(--vl-success) 30%, transparent);
  color: var(--vl-success);
  background: var(--vl-success-dim);
}
.test-result.error {
  border: 1px solid color-mix(in srgb, var(--vl-danger) 30%, transparent);
  color: var(--vl-danger);
  background: var(--vl-danger-dim);
}
.test-result h4 { margin: 0 0 0.25rem; font-size: 0.88rem; }
.test-result p { margin: 0; font-size: 0.82rem; }

</style>