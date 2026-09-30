import { useQuery } from '@tanstack/react-query'
import { api } from '@/lib/api'

export type AICapabilityKey = 'llm' | 'asr' | 'embedding' | 'vision'

export interface AICapabilityStatus {
  key: AICapabilityKey
  label: string
  model: string
  ready: boolean
}

const capabilityLabels: Record<AICapabilityKey, string> = {
  llm: '对话模型',
  asr: '语音识别',
  embedding: '向量模型',
  vision: '视觉理解',
}

const emptyCapabilities: AICapabilityStatus[] = (Object.keys(capabilityLabels) as AICapabilityKey[]).map(key => ({
  key,
  label: capabilityLabels[key],
  model: '',
  ready: false,
}))

export function useAIAvailability(readOnly = false) {
  const query = useQuery({
    queryKey: ['ai-action-availability'],
    queryFn: async () => {
      const profiles = await api.listProfiles()
      const profile = profiles.find(candidate => candidate.is_default)
      if (!profile) return { ready: false, name: '', hostedPaused: false, capabilities: emptyCapabilities }

      const capabilities: AICapabilityStatus[] = [
        { key: 'llm', label: capabilityLabels.llm, model: profile.llm_model || '', ready: !!profile.llm_model?.trim() && !!profile.llm_base_url?.trim() },
        { key: 'asr', label: capabilityLabels.asr, model: profile.asr_model || '', ready: !!profile.asr_model?.trim() && !!profile.asr_base_url?.trim() },
        { key: 'embedding', label: capabilityLabels.embedding, model: profile.embedding_model || '', ready: !!profile.embedding_model?.trim() && !!profile.embedding_endpoint?.trim() && profile.embedding_dim > 0 },
        { key: 'vision', label: capabilityLabels.vision, model: profile.vision_model || '', ready: !!profile.vision_model?.trim() && !!profile.vision_base_url?.trim() },
      ]
      const missing = capabilities.some(capability => !capability.ready)
      if (profile.source !== 'hosted') {
        return { ready: !missing, name: profile.name || '', hostedPaused: false, capabilities }
      }
      const hosted = await api.hostedAI()
      return { ready: !missing && hosted.enabled, name: profile.name || '', hostedPaused: !hosted.enabled, capabilities }
    },
    enabled: !readOnly,
    staleTime: 0,
    refetchOnWindowFocus: true,
  })

  const capabilities = query.data?.capabilities || emptyCapabilities
  const missing = capabilities.filter(capability => !capability.ready).map(capability => capability.label)
  const reason = query.error
    ? 'AI 配置状态读取失败，请重试'
    : query.isPending && !readOnly
      ? '正在检查默认 AI 配置'
      : query.data?.hostedPaused
        ? 'Free API 暂停，可在设置中选择自己的 API 配置'
        : !readOnly && !query.data?.ready
          ? query.data?.name
            ? `默认配置「${query.data.name}」还未配齐：${missing.join('、') || '请启用 AI 服务'}`
            : `请先配置默认 AI 服务：${missing.join('、')}`
          : ''

  return {
    ...query,
    ready: readOnly || (!query.error && !!query.data?.ready),
    reason,
    capabilities,
    missing,
    profileName: query.data?.name || '',
    hostedPaused: !!query.data?.hostedPaused,
  }
}
