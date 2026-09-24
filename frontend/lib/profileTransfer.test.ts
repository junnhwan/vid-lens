import assert from 'node:assert/strict'
import { it } from 'node:test'
import { exportProfile, parseProfileImport } from './profileTransfer.ts'
import type { AIProfile } from './types.ts'

const profile: AIProfile = {
  id: 9, name: '示例', llm_provider: 'siliconflow', llm_base_url: 'https://api.siliconflow.cn/v1', llm_model: 'chat', llm_context_tokens: 131072, llm_api_key_masked: 'sk-****cret',
  asr_provider: 'siliconflow', asr_base_url: 'https://api.siliconflow.cn/v1', asr_model: 'asr', asr_api_key_masked: 'sk-****cret',
  embedding_provider: 'siliconflow', embedding_endpoint: 'https://api.siliconflow.cn/v1/embeddings', embedding_model: 'embed', embedding_dim: 1024, embedding_api_key_masked: 'sk-****cret',
  vision_provider: '', vision_base_url: '', vision_model: '', vision_api_key_masked: '', is_default: true,
  agent_budget: { version: 1, max_tool_calls: 4, max_duration_seconds: 60, max_input_tokens: 2000, max_output_tokens: 1000 },
}

it('exports a round-trippable profile without credentials or default selection', () => {
  const text = exportProfile(profile)
  assert.equal(text.includes('sk-'), false)
  const imported = parseProfileImport(text)
  assert.equal(imported.profile.embedding_dim, 1024)
  assert.equal(imported.profile.llm_context_tokens, 131072)
  assert.equal(imported.profile.is_default, false)
  assert.equal(imported.profile.agent_budget?.max_tool_calls, 4)
})

it('ignores plaintext keys in imported files and rejects missing required fields', () => {
  const data = JSON.parse(exportProfile(profile))
  data.profile.llm_api_key = 'SECRET-DO-NOT-SHOW'
  const imported = parseProfileImport(JSON.stringify(data))
  assert.equal(imported.ignoredSecrets, true)
  assert.equal('llm_api_key' in imported.profile, false)
  delete data.profile.embedding_model
  assert.throws(() => parseProfileImport(JSON.stringify(data)), /embedding_model/)
})

it('accepts existing flat profile JSON while discarding masked credentials', () => {
  const imported = parseProfileImport(JSON.stringify(profile))
  assert.equal(imported.profile.llm_model, 'chat')
  assert.equal(imported.ignoredSecrets, true)
  assert.equal('llm_api_key_masked' in imported.profile, false)
})

it('rejects duplicate paths and malformed budget before preview', () => {
  const data = JSON.parse(exportProfile(profile))
  data.profile.llm_base_url += '/v1'
  assert.throws(() => parseProfileImport(JSON.stringify(data)), /重复/)
  data.profile.llm_base_url = profile.llm_base_url
  data.profile.agent_budget.max_tool_calls = 'invalid'
  assert.throws(() => parseProfileImport(JSON.stringify(data)), /agent_budget/)
  data.profile.agent_budget.max_tool_calls = 4
  data.profile.llm_context_tokens = -1
  assert.throws(() => parseProfileImport(JSON.stringify(data)), /llm_context_tokens/)
})
