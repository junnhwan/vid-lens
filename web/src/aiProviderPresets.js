/**
 * AI 配置表单：产品路径统一为 OpenAI 兼容（Base URL + Key + 模型）。
 * 后端仍接受 siliconflow/mimo 等历史 provider 值；前端新建默认写入 openai_compatible。
 * 真正校验在服务端。
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

/** Product UI only lists OpenAI-compatible; presets still hold optional shortcuts for chips. */
export const PRODUCT_PROVIDER = 'openai_compatible'

/** ASR — product options (no MiMo in UI) */
export const ASR_PROVIDERS = /** @type {const} */ ([
  { id: 'openai_compatible', label: 'OpenAI 兼容', description: '兼容 /audio 或同类 ASR 端点' },
  { id: 'siliconflow', label: 'SiliconFlow', description: '历史兼容，新建请用 OpenAI 兼容' },
])

export const LLM_PROVIDERS = /** @type {const} */ ([
  { id: 'openai_compatible', label: 'OpenAI 兼容', description: 'DeepSeek / 通义 / 中转 / 自建' },
  { id: 'siliconflow', label: 'SiliconFlow', description: '历史兼容' },
])

export const VISION_PROVIDERS = /** @type {const} */ ([
  { id: 'openai_compatible', label: 'OpenAI 兼容', description: 'GPT-4o / Qwen-VL / 自建多模态' },
  { id: 'siliconflow', label: 'SiliconFlow', description: '历史兼容' },
])

export const EMBEDDING_PROVIDERS = /** @type {const} */ ([
  { id: 'openai_compatible', label: 'OpenAI 兼容', description: 'OpenAI / 兼容 embeddings' },
  { id: 'siliconflow', label: 'SiliconFlow', description: '历史兼容' },
])

export const ASR_PRESETS = {
  openai_compatible: {
    baseURL: '',
    model: '',
    models: [],
    keyPlaceholder: 'sk-xxx',
  },
  siliconflow: {
    baseURL: 'https://api.siliconflow.cn/v1',
    model: 'FunAudioLLM/SenseVoiceSmall',
    models: ['FunAudioLLM/SenseVoiceSmall', 'TeleAI/TeleSpeechASR'],
    keyPlaceholder: 'sk-xxx',
  },
  mimo: {
    baseURL: 'https://api.xiaomimimo.com/v1',
    model: 'mimo-v2.5-asr',
    models: ['mimo-v2.5-asr'],
    keyPlaceholder: 'tp-xxx / sk-xxx',
  },
}

export const LLM_PRESETS = {
  openai_compatible: {
    baseURL: '',
    model: '',
    models: ['gpt-4o-mini', 'gpt-4o', 'deepseek-chat', 'deepseek-ai/DeepSeek-V3'],
    keyPlaceholder: 'sk-xxx',
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
  mimo: {
    baseURL: 'https://api.xiaomimimo.com/v1',
    model: 'mimo-v2.5',
    models: ['mimo-v2.5', 'mimo-v2'],
    keyPlaceholder: 'tp-xxx / sk-xxx',
  },
}

export const VISION_PRESETS = {
  openai_compatible: {
    baseURL: '',
    model: '',
    models: ['gpt-4o-mini', 'gpt-4o', 'qwen-vl-plus'],
    keyPlaceholder: 'sk-xxx',
  },
  siliconflow: {
    baseURL: 'https://api.siliconflow.cn/v1',
    model: 'Qwen/Qwen2.5-VL-72B-Instruct',
    models: ['Qwen/Qwen2.5-VL-72B-Instruct', 'Qwen/Qwen2-VL-72B-Instruct'],
    keyPlaceholder: 'sk-xxx',
  },
  mimo: {
    baseURL: 'https://api.xiaomimimo.com/v1',
    model: '',
    models: [],
    keyPlaceholder: 'tp-xxx / sk-xxx',
  },
}

export const EMBEDDING_PRESETS = {
  openai_compatible: {
    endpoint: '',
    model: '',
    models: ['text-embedding-3-small', 'text-embedding-3-large', 'BAAI/bge-m3'],
    dim: 1536,
    keyPlaceholder: 'sk-xxx',
  },
  siliconflow: {
    endpoint: 'https://api.siliconflow.cn/v1/embeddings',
    model: 'BAAI/bge-m3',
    models: ['BAAI/bge-m3', 'BAAI/bge-large-zh-v1.5', 'netease-youdao/bce-embedding-base_v1'],
    dim: 1024,
    keyPlaceholder: 'sk-xxx',
  },
}

/**
 * @param {unknown} provider
 * @returns {string}
 */
export function normalizeProviderId(provider) {
  return String(provider ?? '')
    .trim()
    .toLowerCase()
}

/**
 * Coerce unknown/legacy providers for form state.
 * Product UI always prefers openai_compatible for display when unknown.
 * @param {string} value
 * @param {readonly { id: string }[]} options
 * @param {string} [fallback]
 * @returns {string}
 */
export function coerceProviderId(value, options, fallback = PRODUCT_PROVIDER) {
  const id = normalizeProviderId(value)
  if (options.some((o) => o.id === id)) return id
  // Keep mimo only if somehow still in options; otherwise map to product default
  if (id === 'mimo') return fallback
  return fallback
}

/**
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
 * @param {{ asr_provider: string, asr_base_url: string, asr_model: string }} form
 * @param {string} nextProvider
 */
export function applyAsrProviderChange(form, nextProvider) {
  const next = coerceProviderId(nextProvider, ASR_PROVIDERS, PRODUCT_PROVIDER)
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
  const next = coerceProviderId(nextProvider, LLM_PROVIDERS, PRODUCT_PROVIDER)
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
 * @param {{ vision_provider: string, vision_base_url: string, vision_model: string }} form
 * @param {string} nextProvider
 */
export function applyVisionProviderChange(form, nextProvider) {
  if (!String(nextProvider ?? '').trim()) {
    return {
      vision_provider: '',
      vision_base_url: form.vision_base_url || '',
      vision_model: form.vision_model || '',
    }
  }
  const next = coerceProviderId(nextProvider, VISION_PROVIDERS, PRODUCT_PROVIDER)
  const prev = normalizeProviderId(form.vision_provider)
  const prevPreset = VISION_PRESETS[prev] || {}
  const nextPreset = VISION_PRESETS[next] || {}
  return {
    vision_provider: next,
    vision_base_url: shouldReplacePresetField(form.vision_base_url, prevPreset.baseURL)
      ? nextPreset.baseURL || ''
      : form.vision_base_url,
    vision_model: shouldReplacePresetField(form.vision_model, prevPreset.model)
      ? nextPreset.model || ''
      : form.vision_model,
  }
}

/**
 * @param {{ embedding_provider: string, embedding_endpoint: string, embedding_model: string, embedding_dim: number | null }} form
 * @param {string} nextProvider
 */
export function applyEmbeddingProviderChange(form, nextProvider) {
  const next = coerceProviderId(nextProvider, EMBEDDING_PROVIDERS, PRODUCT_PROVIDER)
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
 * Derive embedding endpoint from an OpenAI-compatible chat base URL.
 * @param {string} llmBaseURL
 * @returns {string}
 */
export function deriveEmbeddingEndpoint(llmBaseURL) {
  const base = String(llmBaseURL ?? '').trim().replace(/\/+$/, '')
  if (!base) return ''
  if (/\/embeddings$/i.test(base)) return base
  return `${base}/embeddings`
}

/**
 * Fresh form for product "新建配置" — all openai_compatible, empty URLs.
 * @returns {Record<string, unknown>}
 */
export function createDefaultAIProfileForm() {
  return {
    name: '',
    asr_provider: PRODUCT_PROVIDER,
    asr_base_url: '',
    asr_api_key: '',
    asr_model: '',
    llm_provider: PRODUCT_PROVIDER,
    llm_base_url: '',
    llm_api_key: '',
    llm_model: '',
    embedding_provider: PRODUCT_PROVIDER,
    embedding_endpoint: '',
    embedding_api_key: '',
    embedding_model: '',
    embedding_dim: 1536,
    vision_provider: '',
    vision_base_url: '',
    vision_api_key: '',
    vision_model: '',
    is_default: false,
  }
}

/**
 * Map saved profile into form; legacy mimo/siliconflow keep URL/model, provider coerced for UI.
 * @param {Record<string, any>} profile
 */
export function profileToFormData(profile) {
  // Preserve raw provider for backend save when still supported; UI treats all as free-form OpenAI-compatible fields.
  const asrRaw = normalizeProviderId(profile.asr_provider) || PRODUCT_PROVIDER
  const llmRaw = normalizeProviderId(profile.llm_provider) || PRODUCT_PROVIDER
  const embRaw = normalizeProviderId(profile.embedding_provider) || PRODUCT_PROVIDER
  const visionRaw = normalizeProviderId(profile.vision_provider)

  return {
    name: profile.name || '',
    asr_provider: asrRaw === 'mimo' ? 'mimo' : asrRaw === 'siliconflow' ? 'siliconflow' : PRODUCT_PROVIDER,
    asr_base_url: profile.asr_base_url || '',
    asr_api_key: '',
    asr_model: profile.asr_model || '',
    llm_provider: llmRaw === 'mimo' ? 'mimo' : llmRaw === 'siliconflow' ? 'siliconflow' : PRODUCT_PROVIDER,
    llm_base_url: profile.llm_base_url || '',
    llm_api_key: '',
    llm_model: profile.llm_model || '',
    embedding_provider: embRaw === 'siliconflow' ? 'siliconflow' : PRODUCT_PROVIDER,
    embedding_endpoint: profile.embedding_endpoint || '',
    embedding_api_key: '',
    embedding_model: profile.embedding_model || '',
    embedding_dim: profile.embedding_dim ?? 1536,
    vision_provider: visionRaw || '',
    vision_base_url: profile.vision_base_url || '',
    vision_api_key: '',
    vision_model: profile.vision_model || '',
    is_default: !!profile.is_default,
  }
}

/**
 * When saving from product form, force openai_compatible unless editing legacy profile that still uses siliconflow/mimo.
 * @param {Record<string, any>} formData
 * @param {{ keepLegacyProviders?: boolean }} [opts]
 */
export function normalizeFormForSave(formData, opts = {}) {
  const keep = !!opts.keepLegacyProviders
  const force = (p) => {
    const id = normalizeProviderId(p)
    if (keep && (id === 'siliconflow' || id === 'mimo')) return id
    return PRODUCT_PROVIDER
  }
  return {
    ...formData,
    asr_provider: force(formData.asr_provider),
    llm_provider: force(formData.llm_provider),
    embedding_provider: force(formData.embedding_provider),
    vision_provider: formData.vision_provider
      ? force(formData.vision_provider)
      : '',
  }
}

/**
 * @param {'asr' | 'llm' | 'embedding' | 'vision'} kind
 * @param {string} provider
 * @returns {string[]}
 */
export function suggestedModels(kind, provider) {
  const id = normalizeProviderId(provider) || PRODUCT_PROVIDER
  if (kind === 'asr') return ASR_PRESETS[id]?.models || ASR_PRESETS[PRODUCT_PROVIDER].models || []
  if (kind === 'llm') return LLM_PRESETS[id]?.models || LLM_PRESETS[PRODUCT_PROVIDER].models || []
  if (kind === 'embedding') {
    return EMBEDDING_PRESETS[id]?.models || EMBEDDING_PRESETS[PRODUCT_PROVIDER].models || []
  }
  if (kind === 'vision') return VISION_PRESETS[id]?.models || VISION_PRESETS[PRODUCT_PROVIDER].models || []
  return []
}

/**
 * @param {'asr' | 'llm' | 'embedding' | 'vision'} kind
 * @param {string} provider
 */
export function keyPlaceholder(kind, provider) {
  const id = normalizeProviderId(provider) || PRODUCT_PROVIDER
  if (kind === 'asr') return ASR_PRESETS[id]?.keyPlaceholder || 'sk-xxx'
  if (kind === 'llm') return LLM_PRESETS[id]?.keyPlaceholder || 'sk-xxx'
  if (kind === 'embedding') return EMBEDDING_PRESETS[id]?.keyPlaceholder || 'sk-xxx'
  if (kind === 'vision') return VISION_PRESETS[id]?.keyPlaceholder || 'sk-xxx'
  return 'sk-xxx'
}

/**
 * @param {string} provider
 */
export function isCustomCompatibleProvider(provider) {
  const id = normalizeProviderId(provider)
  return id === 'openai_compatible' || id === '' || id === 'siliconflow'
}
