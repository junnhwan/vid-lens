import type { AIProfile, AIProfileRequest } from './types'

export const PROFILE_EXPORT_FORMAT = 'vidlens-ai-profile'

const textFields = ['name', 'llm_provider', 'llm_base_url', 'llm_model', 'asr_provider', 'asr_base_url', 'asr_model', 'embedding_provider', 'embedding_endpoint', 'embedding_model'] as const
const optionalFields = ['vision_provider', 'vision_base_url', 'vision_model'] as const

type TransferProfile = Omit<AIProfileRequest, 'llm_api_key' | 'asr_api_key' | 'embedding_api_key' | 'vision_api_key'>

export function exportProfile(profile: AIProfile): string {
  const value: TransferProfile = {
    name: profile.name,
    llm_provider: profile.llm_provider, llm_base_url: profile.llm_base_url, llm_model: profile.llm_model, llm_context_tokens: profile.llm_context_tokens || 0,
    asr_provider: profile.asr_provider, asr_base_url: profile.asr_base_url, asr_model: profile.asr_model,
    embedding_provider: profile.embedding_provider, embedding_endpoint: profile.embedding_endpoint,
    embedding_model: profile.embedding_model, embedding_dim: profile.embedding_dim,
    vision_provider: profile.vision_provider || '', vision_base_url: profile.vision_base_url || '', vision_model: profile.vision_model || '',
    agent_budget: profile.agent_budget || null,
    is_default: false,
  }
  return JSON.stringify({ format: PROFILE_EXPORT_FORMAT, version: 1, profile: value }, null, 2) + '\n'
}

export function parseProfileImport(text: string): { profile: TransferProfile; ignoredSecrets: boolean } {
  if (text.length > 100_000) throw new Error('配置文件过大（上限 100 KB）')
  let root: unknown
  try { root = JSON.parse(text) } catch { throw new Error('不是有效的 JSON 文件') }
  if (!root || typeof root !== 'object' || Array.isArray(root)) throw new Error('配置文件结构错误')
  const envelope = root as Record<string, unknown>
  if (envelope.format !== undefined && (envelope.format !== PROFILE_EXPORT_FORMAT || envelope.version !== 1)) throw new Error('不支持的配置格式或版本')
  const raw = envelope.format === PROFILE_EXPORT_FORMAT ? envelope.profile : envelope
  if (!raw || typeof raw !== 'object' || Array.isArray(raw)) throw new Error('缺少 profile 对象')
  const p = raw as Record<string, unknown>
  for (const key of textFields) {
    if (typeof p[key] !== 'string' || !p[key].trim()) throw new Error(`缺少必填字段 ${key}`)
  }
  for (const key of optionalFields) if (p[key] !== undefined && typeof p[key] !== 'string') throw new Error(`${key} 格式错误`)
  if (!Number.isSafeInteger(p.embedding_dim) || (p.embedding_dim as number) <= 0) throw new Error('embedding_dim 必须是正整数')
  if (p.llm_context_tokens !== undefined && (!Number.isSafeInteger(p.llm_context_tokens) || ((p.llm_context_tokens as number) !== 0 && ((p.llm_context_tokens as number) < 8192 || (p.llm_context_tokens as number) > 1048576)))) throw new Error('llm_context_tokens 格式错误')
  for (const [key, endpoint] of [['llm_base_url', false], ['asr_base_url', false], ['embedding_endpoint', true], ['vision_base_url', false]] as const) {
    if (key === 'vision_base_url' && !p[key]) continue
    let url: URL
    try { url = new URL(String(p[key])) } catch { throw new Error(`${key} 不是有效 URL`) }
    if (!['http:', 'https:'].includes(url.protocol) || !url.hostname || url.username || url.password || url.search || url.hash) throw new Error(`${key} 需为不含账号或参数的 http(s) 地址`)
    const path = url.pathname.replace(/\/+$/, '').toLowerCase()
    if (path.includes('/v1/v1')) throw new Error(`${key} 的 /v1 重复`)
    if (!endpoint && !path && ['api.siliconflow.cn', 'api.openai.com'].includes(url.hostname.toLowerCase())) throw new Error(`${key} 缺少 /v1`)
    if (endpoint ? !path.endsWith('/embeddings') : /\/(chat\/completions|audio\/transcriptions|embeddings|models)$/.test(path)) throw new Error(`${key} 的接口路径格式不正确`)
  }
  const vision = optionalFields.map(key => String(p[key] || '').trim())
  if (vision.some(Boolean) && !vision.every(Boolean)) throw new Error('视觉配置需同时填写服务商、地址和模型')
  if (p.agent_budget !== undefined && p.agent_budget !== null && (typeof p.agent_budget !== 'object' || Array.isArray(p.agent_budget))) throw new Error('agent_budget 格式错误')
  if (p.agent_budget && typeof p.agent_budget === 'object') {
    const budget = p.agent_budget as Record<string, unknown>
    for (const [key, value] of Object.entries(budget)) {
      if (!['version', 'max_tool_calls', 'max_duration_seconds', 'max_input_tokens', 'max_output_tokens', 'max_visual_frames'].includes(key) || !Number.isSafeInteger(value) || (value as number) <= 0 || (key === 'version' && value !== 1)) throw new Error('agent_budget 包含未知字段或无效数值')
    }
  }
  const profile: TransferProfile = {
    name: String(p.name).trim(), llm_provider: String(p.llm_provider).trim(), llm_base_url: String(p.llm_base_url).trim(), llm_model: String(p.llm_model).trim(), llm_context_tokens: (p.llm_context_tokens as number) || 0,
    asr_provider: String(p.asr_provider).trim(), asr_base_url: String(p.asr_base_url).trim(), asr_model: String(p.asr_model).trim(),
    embedding_provider: String(p.embedding_provider).trim(), embedding_endpoint: String(p.embedding_endpoint).trim(), embedding_model: String(p.embedding_model).trim(),
    embedding_dim: p.embedding_dim as number, vision_provider: vision[0], vision_base_url: vision[1], vision_model: vision[2],
    agent_budget: p.agent_budget as TransferProfile['agent_budget'] || null, is_default: false,
  }
  const ignoredSecrets = Object.keys(p).some(key => /(?:api_key|secret|token|password)/i.test(key))
  return { profile, ignoredSecrets }
}
