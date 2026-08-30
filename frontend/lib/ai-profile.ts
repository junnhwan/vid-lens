import type { AIProfile, AIProfileRequest } from './types'

export type ProfileGroupDraft = {
  provider: string
  base: string
  apiKey: string
  model: string
}

export type ProfileGroupsDraft = {
  asr: ProfileGroupDraft
  llm: ProfileGroupDraft
  embedding: ProfileGroupDraft & { dim: number }
  vision: ProfileGroupDraft
}

const emptyGroup = (): ProfileGroupDraft => ({
  provider: '',
  base: '',
  apiKey: '',
  model: '',
})

export function emptyProfileGroups(): ProfileGroupsDraft {
  return {
    asr: emptyGroup(),
    llm: emptyGroup(),
    embedding: { ...emptyGroup(), dim: 0 },
    vision: emptyGroup(),
  }
}

export function profileToGroups(profile: AIProfile): ProfileGroupsDraft {
  return {
    asr: {
      provider: profile.asr_provider,
      base: profile.asr_base_url,
      apiKey: '',
      model: profile.asr_model,
    },
    llm: {
      provider: profile.llm_provider,
      base: profile.llm_base_url,
      apiKey: '',
      model: profile.llm_model,
    },
    embedding: {
      provider: profile.embedding_provider,
      base: profile.embedding_endpoint,
      apiKey: '',
      model: profile.embedding_model,
      dim: profile.embedding_dim,
    },
    vision: {
      provider: profile.vision_provider || '',
      base: profile.vision_base_url || '',
      apiKey: '',
      model: profile.vision_model || '',
    },
  }
}

/** 将已有 Profile 转为后端 PUT 所需的完整请求体（密钥留空表示保留旧值）。 */
export function profileToRequest(
  profile: AIProfile,
  opts?: { is_default?: boolean; groups?: ProfileGroupsDraft },
): AIProfileRequest {
  const groups = opts?.groups ?? profileToGroups(profile)
  return buildProfileRequest(profile.name, groups, { is_default: opts?.is_default ?? profile.is_default })
}

export function buildProfileRequest(
  name: string,
  groups: ProfileGroupsDraft,
  opts?: { is_default?: boolean },
): AIProfileRequest {
  const body: AIProfileRequest = {
    name: name.trim(),
    is_default: opts?.is_default,
    llm_provider: groups.llm.provider.trim(),
    llm_base_url: groups.llm.base.trim(),
    llm_model: groups.llm.model.trim(),
    asr_provider: groups.asr.provider.trim(),
    asr_base_url: groups.asr.base.trim(),
    asr_model: groups.asr.model.trim(),
    embedding_provider: groups.embedding.provider.trim(),
    embedding_endpoint: groups.embedding.base.trim(),
    embedding_model: groups.embedding.model.trim(),
    embedding_dim: groups.embedding.dim,
  }

  if (groups.llm.apiKey) body.llm_api_key = groups.llm.apiKey
  if (groups.asr.apiKey) body.asr_api_key = groups.asr.apiKey
  if (groups.embedding.apiKey) body.embedding_api_key = groups.embedding.apiKey

  const visionFilled =
    groups.vision.provider.trim() ||
    groups.vision.base.trim() ||
    groups.vision.model.trim() ||
    groups.vision.apiKey
  if (visionFilled) {
    body.vision_provider = groups.vision.provider.trim()
    body.vision_base_url = groups.vision.base.trim()
    body.vision_model = groups.vision.model.trim()
    if (groups.vision.apiKey) body.vision_api_key = groups.vision.apiKey
  }

  return body
}

export function validateProfileGroups(groups: ProfileGroupsDraft, requireKeys: boolean): string | null {
  const checks: { label: string; group: ProfileGroupDraft }[] = [
    { label: 'LLM', group: groups.llm },
    { label: 'ASR', group: groups.asr },
    { label: 'Embedding', group: groups.embedding },
  ]
  for (const { label, group } of checks) {
    if (!group.provider.trim()) return `${label} Provider 不能为空`
    if (!group.base.trim()) return `${label} ${label === 'Embedding' ? 'Endpoint' : 'Base URL'} 不能为空`
    if (!group.model.trim()) return `${label} Model 不能为空`
  }
  if (groups.embedding.dim <= 0) return 'Embedding 维度必须大于 0（可点「探测维度」或手动填写）'
  if (requireKeys) {
    if (!groups.llm.apiKey.trim() || !groups.asr.apiKey.trim() || !groups.embedding.apiKey.trim()) {
      return '创建 Profile 需在三组 Tab 下分别填写 LLM / ASR / Embedding 的 API Key'
    }
  }
  return null
}
