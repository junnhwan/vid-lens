import assert from 'node:assert/strict'

import {
  ASR_PROVIDERS,
  EMBEDDING_PROVIDERS,
  LLM_PROVIDERS,
  applyAsrProviderChange,
  applyEmbeddingProviderChange,
  applyLlmProviderChange,
  coerceProviderId,
  createDefaultAIProfileForm,
  keyPlaceholder,
  normalizeProviderId,
  profileToFormData,
  shouldReplacePresetField,
  suggestedModels,
} from '../src/aiProviderPresets.js'

assert.equal(normalizeProviderId(' MiMo '), 'mimo')
assert.equal(normalizeProviderId(null), '')

assert.equal(
  coerceProviderId('mimo', ASR_PROVIDERS),
  'mimo',
  'known ASR provider stays',
)
assert.equal(
  coerceProviderId('unknown-vendor', ASR_PROVIDERS, 'openai_compatible'),
  'openai_compatible',
  'unknown provider falls back',
)

assert.equal(shouldReplacePresetField('', 'https://a'), true)
assert.equal(shouldReplacePresetField('https://a', 'https://a'), true)
assert.equal(
  shouldReplacePresetField('https://custom', 'https://a'),
  false,
  'user-customized URL must not be overwritten',
)

const asrSwitch = applyAsrProviderChange(
  {
    asr_provider: 'mimo',
    asr_base_url: 'https://api.xiaomimimo.com/v1',
    asr_model: 'mimo-v2.5-asr',
  },
  'siliconflow',
)
assert.equal(asrSwitch.asr_provider, 'siliconflow')
assert.match(asrSwitch.asr_base_url, /siliconflow/)
assert.ok(asrSwitch.asr_model)

const asrKeepCustom = applyAsrProviderChange(
  {
    asr_provider: 'mimo',
    asr_base_url: 'https://my-proxy.example/v1',
    asr_model: 'custom-asr',
  },
  'siliconflow',
)
assert.equal(asrKeepCustom.asr_base_url, 'https://my-proxy.example/v1')
assert.equal(asrKeepCustom.asr_model, 'custom-asr')

const llmSwitch = applyLlmProviderChange(
  {
    llm_provider: 'openai_compatible',
    llm_base_url: 'https://api.openai.com/v1',
    llm_model: 'gpt-4o-mini',
  },
  'mimo',
)
assert.equal(llmSwitch.llm_provider, 'mimo')
assert.match(llmSwitch.llm_base_url, /xiaomimimo|mimo/i)

const embSwitch = applyEmbeddingProviderChange(
  {
    embedding_provider: 'openai_compatible',
    embedding_endpoint: 'https://api.openai.com/v1/embeddings',
    embedding_model: 'text-embedding-3-small',
    embedding_dim: 1536,
  },
  'siliconflow',
)
assert.equal(embSwitch.embedding_provider, 'siliconflow')
assert.equal(embSwitch.embedding_dim, 1024)
assert.match(embSwitch.embedding_endpoint, /embeddings/)

const fresh = createDefaultAIProfileForm()
assert.equal(fresh.asr_provider, 'mimo')
assert.equal(fresh.llm_provider, 'openai_compatible')
assert.equal(fresh.embedding_provider, 'siliconflow')
assert.ok(fresh.asr_base_url)
assert.ok(fresh.llm_base_url)
assert.ok(fresh.embedding_endpoint)
assert.equal(fresh.embedding_dim, 1024)
assert.equal(fresh.asr_api_key, '')
assert.equal(fresh.is_default, false)

const formFromProfile = profileToFormData({
  name: 'prod',
  asr_provider: 'MiMo',
  asr_base_url: 'https://custom-asr',
  asr_model: 'x',
  llm_provider: 'weird',
  llm_base_url: 'https://custom-llm',
  llm_model: 'y',
  embedding_provider: 'siliconflow',
  embedding_endpoint: 'https://api.siliconflow.cn/v1/embeddings',
  embedding_model: 'BAAI/bge-m3',
  embedding_dim: 1024,
  is_default: true,
})
assert.equal(formFromProfile.asr_provider, 'mimo')
assert.equal(formFromProfile.llm_provider, 'openai_compatible', 'unknown llm coerced for select')
assert.equal(formFromProfile.llm_base_url, 'https://custom-llm', 'custom URL preserved')
assert.equal(formFromProfile.is_default, true)

assert.ok(suggestedModels('llm', 'siliconflow').length > 0)
assert.ok(suggestedModels('embedding', 'siliconflow').includes('BAAI/bge-m3'))
assert.equal(keyPlaceholder('asr', 'mimo').includes('xxx'), true)

assert.ok(ASR_PROVIDERS.some((p) => p.id === 'mimo'))
assert.ok(LLM_PROVIDERS.some((p) => p.id === 'openai_compatible'))
assert.ok(EMBEDDING_PROVIDERS.every((p) => p.id !== 'mimo'), 'embedding has no mimo strategy')

console.log('aiProviderPresets tests passed')
