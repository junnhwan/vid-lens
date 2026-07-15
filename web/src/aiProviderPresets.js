/**
 * AI 配置表单：与后端 internal/ai/factory.go 支持的 provider 对齐。
 * 仅用于前端下拉与默认值填充；真正校验仍在服务端。
 */

/** @typedef {{ id: string, label: string, description?: string }} ProviderOption */

/** @typedef {{
 *   baseURL?: string,
 *   model?: string,
 *   models?: string[],
 *   endpoint?: string,
 *   dim?: number,
 *   keyPlaceholder?: string,
 * }} ProviderPreset */

/** ASR providers accepted by ai.Factory.NewASRStrategy */
export const ASR_PROVIDERS = /** @type {const} */ ([
  { id: 'mimo', label: 'MiMo', description: '小米 MiMo ASR' },
  { id: 'siliconflow', label: 'SiliconFlow', description: '硅基流动 ASR' },
  { id: 'openai_compatible', label: 'OpenAI 兼容', description: '任意 OpenAI-compatible ASR' },
])

/** LLM providers accepted by ai.Factory.NewChatClient */
export const LLM_PROVIDERS = /** @type {const} */ ([
  { id: 'mimo', label: 'MiMo', description: '小米 MiMo LLM' },
  { id: 'siliconflow', label: 'SiliconFlow', description: '硅基流动 LLM' },
  { id: 'openai_compatible', label: 'OpenAI 兼容', description: 'DeepSeek / 通义 / 自建等' },
])

/** Embedding providers accepted by ai.Factory.NewEmbeddingClient */
export const EMBEDDING_PROVIDERS = /** @type {const} */ ([
  { id: 'siliconflow', label: 'SiliconFlow', description: '硅基流动 Embedding' },
  { id: 'openai_compatible', label: 'OpenAI 兼容', description: 'OpenAI / 兼容端点' },
])

/** @type {Record<string, ProviderPreset>} */
export const ASR_PRESETS = {
  mimo: {
    baseURL: 'https://api.xiaomimimo.com/v1',
    model: 'mimo-v2.5-asr',
    models: ['mimo-v2.5-asr'],
    keyPlaceholder: 'tp-xxx / sk-xxx',
  },
  siliconflow: {
    baseURL: 'https://api.siliconflow.cn/v1',
    model: 'FunAudioLLM/SenseVoiceSmall',
    models: ['FunAudioLLM/SenseVoiceSmall', 'TeleAI/TeleSpeechASR'],
    keyPlaceholder: 'sk-xxx',
  },
  openai_compatible: {
    baseURL: '',
    model: '',
    models: [],
    keyPlaceholder: 'sk-xxx',
  },
}

/** @type {Record<string, ProviderPreset>} */
export const LLM_PRESETS = {
  mimo: {
    baseURL: 'https://api.xiaomimimo.com/v1',
    model: 'mimo-v2.5',
    models: ['mimo-v2.5', 'mimo-v2'],
    keyPlaceholder: 'tp-xxx / sk-xxx',
  },
  siliconflow: {
    baseURL: 'https://api.siliconflow.cn/v1',
    model: 'deepseek-ai/DeepSeek-V3',
    models: [
      'deepseek-ai/DeepSeek-V3',
      'deepseek-ai/DeepSeek-R1',
      'Qwen/Qwen2.5-72B-Instruct',
      'Qwen/Qwen2.5-7B-Instruct',
    ],
    keyPlaceholder: 'sk-xxx',
  },
  openai_compatible: {
    baseURL: 'https://api.openai.com/v1',
    model: 'gpt-4o-mini',
    models: ['gpt-4o-mini', 'gpt-4o', 'deepseek-chat'],
    keyPlaceholder: 'sk-xxx',
  },
}

/** @type {Record<string, ProviderPreset>} */
export const EMBEDDING_PRESETS = {
  siliconflow: {
    endpoint: 'https://api.siliconflow.cn/v1/embeddings',
    model: 'BAAI/bge-m3',
    models: ['BAAI/bge-m3', 'BAAI/bge-large-zh-v1.5', 'netease-youdao/bce-embedding-base_v1'],
    dim: 1024,
    keyPlaceholder: 'sk-xxx',
  },
  openai_compatible: {
    endpoint: 'https://api.openai.com/v1/embeddings',
    model: 'text-embedding-3-small',
    models: ['text-embedding-3-small', 'text-embedding-3-large'],
    dim: 1536,
    keyPlaceholder: 'sk-xxx',
  },
}

/**
 * Normalize provider id for comparison (trim + lower).
 * @param {unknown} provider
 * @returns {string}
 */
export function normalizeProviderId(provider) {
  return String(provider ?? '')
    .trim()
    .toLowerCase()
}

/**
 * Ensure the current value is one of the allowed options; otherwise fall back.
 * Unknown values (legacy profiles) are kept only if allowUnknown is true and value non-empty —
 * callers that want a strict select should pass allowUnknown=false and append an option separately.
 * @param {string} value
 * @param {readonly { id: string }[]} options
 * @param {string} [fallback]
 * @returns {string}
 */
export function coerceProviderId(value, options, fallback = options[0]?.id || '') {
  const id = normalizeProviderId(value)
  if (options.some((o) => o.id === id)) return id
  return fallback
}

/**
 * Whether a free-text field still matches the previous provider's auto-filled default
 * (or is empty), so switching provider can safely overwrite it.
 * @param {string} current
 * @param {string | undefined} previousDefault
 * @returns {boolean}
 */
export function shouldReplacePresetField(current, previousDefault) {
  const cur = String(current ?? '').trim()
  if (!cur) return true
  const prev = String(previousDefault ?? '').trim()
  if (!prev) return false
  return cur === prev
}

/**
 * Apply ASR provider switch: fill base URL / model when empty or still on old defaults.
 * @param {{ asr_provider: string, asr_base_url: string, asr_model: string }} form
 * @param {string} nextProvider
 * @returns {{ asr_provider: string, asr_base_url: string, asr_model: string }}
 */
export function applyAsrProviderChange(form, nextProvider) {
  const next = coerceProviderId(nextProvider, ASR_PROVIDERS, 'mimo')
  const prev = normalizeProviderId(form.asr_provider)
  const prevPreset = ASR_PRESETS[prev] || {}
  const nextPreset = ASR_PRESETS[next] || {}
  return {
    asr_provider: next,
    asr_base_url: shouldReplacePresetField(form.asr_base_url, prevPreset.baseURL)
      ? nextPreset.baseURL || ''
      : form.asr_base_url,
    asr_model: shouldReplacePresetField(form.asr_model, prevPreset.model)
      ? nextPreset.model || ''
      : form.asr_model,
  }
}

/**
 * @param {{ llm_provider: string, llm_base_url: string, llm_model: string }} form
 * @param {string} nextProvider
 */
export function applyLlmProviderChange(form, nextProvider) {
  const next = coerceProviderId(nextProvider, LLM_PROVIDERS, 'openai_compatible')
  const prev = normalizeProviderId(form.llm_provider)
  const prevPreset = LLM_PRESETS[prev] || {}
  const nextPreset = LLM_PRESETS[next] || {}
  return {
    llm_provider: next,
    llm_base_url: shouldReplacePresetField(form.llm_base_url, prevPreset.baseURL)
      ? nextPreset.baseURL || ''
      : form.llm_base_url,
    llm_model: shouldReplacePresetField(form.llm_model, prevPreset.model)
      ? nextPreset.model || ''
      : form.llm_model,
  }
}

/**
 * @param {{ embedding_provider: string, embedding_endpoint: string, embedding_model: string, embedding_dim: number | null }} form
 * @param {string} nextProvider
 */
export function applyEmbeddingProviderChange(form, nextProvider) {
  const next = coerceProviderId(nextProvider, EMBEDDING_PROVIDERS, 'siliconflow')
  const prev = normalizeProviderId(form.embedding_provider)
  const prevPreset = EMBEDDING_PRESETS[prev] || {}
  const nextPreset = EMBEDDING_PRESETS[next] || {}
  const dimEmpty = form.embedding_dim == null || form.embedding_dim === 0
  const dimMatchesPrev =
    prevPreset.dim != null && Number(form.embedding_dim) === Number(prevPreset.dim)
  return {
    embedding_provider: next,
    embedding_endpoint: shouldReplacePresetField(form.embedding_endpoint, prevPreset.endpoint)
      ? nextPreset.endpoint || ''
      : form.embedding_endpoint,
    embedding_model: shouldReplacePresetField(form.embedding_model, prevPreset.model)
      ? nextPreset.model || ''
      : form.embedding_model,
    embedding_dim: dimEmpty || dimMatchesPrev ? nextPreset.dim ?? form.embedding_dim : form.embedding_dim,
  }
}

/**
 * Fresh form defaults for "新建配置".
 * @returns {Record<string, unknown>}
 */
export function createDefaultAIProfileForm() {
  const asr = applyAsrProviderChange(
    { asr_provider: '', asr_base_url: '', asr_model: '' },
    'mimo',
  )
  const llm = applyLlmProviderChange(
    { llm_provider: '', llm_base_url: '', llm_model: '' },
    'openai_compatible',
  )
  const emb = applyEmbeddingProviderChange(
    {
      embedding_provider: '',
      embedding_endpoint: '',
      embedding_model: '',
      embedding_dim: null,
    },
    'siliconflow',
  )
  return {
    name: '',
    ...asr,
    asr_api_key: '',
    ...llm,
    llm_api_key: '',
    ...emb,
    embedding_api_key: '',
    is_default: false,
  }
}

/**
 * Map a saved profile into form fields, coercing unknown providers into the select list
 * while keeping custom base URL / model intact.
 * @param {Record<string, any>} profile
 */
export function profileToFormData(profile) {
  const asrId = coerceProviderId(profile.asr_provider, ASR_PROVIDERS, 'openai_compatible')
  const llmId = coerceProviderId(profile.llm_provider, LLM_PROVIDERS, 'openai_compatible')
  const embId = coerceProviderId(profile.embedding_provider, EMBEDDING_PROVIDERS, 'openai_compatible')
  return {
    name: profile.name || '',
    asr_provider: asrId,
    asr_base_url: profile.asr_base_url || '',
    asr_api_key: '',
    asr_model: profile.asr_model || '',
    llm_provider: llmId,
    llm_base_url: profile.llm_base_url || '',
    llm_api_key: '',
    llm_model: profile.llm_model || '',
    embedding_provider: embId,
    embedding_endpoint: profile.embedding_endpoint || '',
    embedding_api_key: '',
    embedding_model: profile.embedding_model || '',
    embedding_dim: profile.embedding_dim ?? null,
    is_default: !!profile.is_default,
  }
}

/**
 * Suggested models for a provider (for datalist).
 * @param {'asr' | 'llm' | 'embedding'} kind
 * @param {string} provider
 * @returns {string[]}
 */
export function suggestedModels(kind, provider) {
  const id = normalizeProviderId(provider)
  if (kind === 'asr') return ASR_PRESETS[id]?.models || []
  if (kind === 'llm') return LLM_PRESETS[id]?.models || []
  if (kind === 'embedding') return EMBEDDING_PRESETS[id]?.models || []
  return []
}

/**
 * Key input placeholder for provider.
 * @param {'asr' | 'llm' | 'embedding'} kind
 * @param {string} provider
 */
export function keyPlaceholder(kind, provider) {
  const id = normalizeProviderId(provider)
  if (kind === 'asr') return ASR_PRESETS[id]?.keyPlaceholder || 'sk-xxx'
  if (kind === 'llm') return LLM_PRESETS[id]?.keyPlaceholder || 'sk-xxx'
  if (kind === 'embedding') return EMBEDDING_PRESETS[id]?.keyPlaceholder || 'sk-xxx'
  return 'sk-xxx'
}

/**
 * Whether Base URL / Endpoint should stay editable.
 * openai_compatible is always free-form; known presets can still be edited (advanced).
 * Kept for UI hints only.
 * @param {string} provider
 */
export function isCustomCompatibleProvider(provider) {
  return normalizeProviderId(provider) === 'openai_compatible'
}
