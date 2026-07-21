import assert from 'node:assert/strict'

import {
  ASR_PROVIDERS,
  EMBEDDING_PROVIDERS,
  LLM_PROVIDERS,
  PRODUCT_PROVIDER,
  applyAsrProviderChange,
  applyEmbeddingProviderChange,
  applyLlmProviderChange,
  coerceProviderId,
  createDefaultAIProfileForm,
  deriveEmbeddingEndpoint,
  keyPlaceholder,
  normalizeFormForSave,
  normalizeProviderId,
  profileToFormData,
  shouldReplacePresetField,
  suggestedModels,
} from '../src/aiProviderPresets.js'

assert.equal(normalizeProviderId(' OpenAI_Compatible '), 'openai_compatible')
assert.equal(normalizeProviderId(null), '')

assert.equal(coerceProviderId('mimo', ASR_PROVIDERS), PRODUCT_PROVIDER)
assert.equal(
  coerceProviderId('unknown-vendor', ASR_PROVIDERS, PRODUCT_PROVIDER),
  PRODUCT_PROVIDER,
)

assert.equal(shouldReplacePresetField('', 'https://a'), true)
assert.equal(shouldReplacePresetField('https://a', 'https://a'), true)
assert.equal(shouldReplacePresetField('https://custom', 'https://a'), false)

assert.equal(
  deriveEmbeddingEndpoint('https://api.example.com/v1'),
  'https://api.example.com/v1/embeddings',
)
assert.equal(
  deriveEmbeddingEndpoint('https://api.example.com/v1/embeddings'),
  'https://api.example.com/v1/embeddings',
)

const fresh = createDefaultAIProfileForm()
assert.equal(fresh.asr_provider, PRODUCT_PROVIDER)
assert.equal(fresh.llm_provider, PRODUCT_PROVIDER)
assert.equal(fresh.embedding_provider, PRODUCT_PROVIDER)
assert.equal(fresh.asr_base_url, '')
assert.equal(fresh.llm_base_url, '')
assert.equal(fresh.embedding_dim, 1536)
assert.equal(fresh.is_default, false)

const formFromProfile = profileToFormData({
  name: 'prod',
  asr_provider: 'siliconflow',
  asr_base_url: 'https://api.siliconflow.cn/v1',
  asr_model: 'x',
  llm_provider: 'weird',
  llm_base_url: 'https://custom-llm/v1',
  llm_model: 'y',
  embedding_provider: 'openai_compatible',
  embedding_endpoint: 'https://custom-llm/v1/embeddings',
  embedding_model: 'emb',
  embedding_dim: 1024,
  is_default: true,
})
assert.equal(formFromProfile.llm_provider, PRODUCT_PROVIDER)
assert.equal(formFromProfile.llm_base_url, 'https://custom-llm/v1')
assert.equal(formFromProfile.asr_provider, 'siliconflow')
assert.equal(formFromProfile.is_default, true)

const saved = normalizeFormForSave({
  ...fresh,
  name: 'n',
  llm_base_url: 'https://x/v1',
  llm_model: 'm',
  asr_base_url: 'https://x/v1',
  asr_model: 'a',
  embedding_endpoint: 'https://x/v1/embeddings',
  embedding_model: 'e',
})
assert.equal(saved.llm_provider, PRODUCT_PROVIDER)
assert.equal(saved.asr_provider, PRODUCT_PROVIDER)

assert.ok(suggestedModels('llm', PRODUCT_PROVIDER).length >= 0)
assert.equal(keyPlaceholder('llm', PRODUCT_PROVIDER).includes('sk'), true)

assert.ok(LLM_PROVIDERS.some((p) => p.id === PRODUCT_PROVIDER))
assert.ok(!ASR_PROVIDERS.some((p) => p.id === 'mimo'), 'product ASR list has no mimo')
assert.ok(!LLM_PROVIDERS.some((p) => p.id === 'mimo'), 'product LLM list has no mimo')
assert.ok(EMBEDDING_PROVIDERS.every((p) => p.id !== 'mimo'))

// legacy switch helpers still work for siliconflow preset data
const asrSwitch = applyAsrProviderChange(
  {
    asr_provider: PRODUCT_PROVIDER,
    asr_base_url: '',
    asr_model: '',
  },
  'siliconflow',
)
assert.equal(asrSwitch.asr_provider, 'siliconflow')

const llmSwitch = applyLlmProviderChange(
  {
    llm_provider: PRODUCT_PROVIDER,
    llm_base_url: '',
    llm_model: '',
  },
  PRODUCT_PROVIDER,
)
assert.equal(llmSwitch.llm_provider, PRODUCT_PROVIDER)

const embSwitch = applyEmbeddingProviderChange(
  {
    embedding_provider: PRODUCT_PROVIDER,
    embedding_endpoint: '',
    embedding_model: '',
    embedding_dim: null,
  },
  PRODUCT_PROVIDER,
)
assert.equal(embSwitch.embedding_provider, PRODUCT_PROVIDER)

console.log('aiProviderPresets tests passed')
