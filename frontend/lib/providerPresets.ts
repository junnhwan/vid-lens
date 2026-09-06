export interface ProviderPreset {
  id: string
  label: string
  provider: string
  baseUrl: string
}

export const PROVIDER_PRESETS: ProviderPreset[] = [
  { id: 'openai-compat', label: 'OpenAI 兼容', provider: 'openai', baseUrl: '' },
  { id: 'siliconflow', label: '硅基流动', provider: 'siliconflow', baseUrl: 'https://api.siliconflow.cn/v1' },
  { id: 'deepseek', label: 'DeepSeek', provider: 'deepseek', baseUrl: 'https://api.deepseek.com/v1' },
  { id: 'openai', label: 'OpenAI', provider: 'openai', baseUrl: 'https://api.openai.com/v1' },
]

export function matchPreset(provider: string, baseUrl: string): string {
  const url = baseUrl.replace(/\/+$/, '')
  const exact = PROVIDER_PRESETS.find(p => p.baseUrl && p.baseUrl.replace(/\/+$/, '') === url)
  if (exact) return exact.id
  const byName = PROVIDER_PRESETS.find(p => p.provider === provider && p.id !== 'openai-compat')
  if (byName && !baseUrl) return byName.id
  if (provider === 'openai' || provider === '') return 'openai-compat'
  return 'openai-compat'
}
