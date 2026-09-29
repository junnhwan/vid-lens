import { useQuery } from '@tanstack/react-query'
import { api } from '@/lib/api'

export function useAIAvailability(readOnly = false) {
  const query = useQuery({ queryKey: ['ai-action-availability'], queryFn: async () => {
    const profiles = await api.listProfiles()
    const profile = profiles.find(profile => profile.is_default)
    if (profile?.source !== 'hosted') return { ready: !!profile, name: profile?.name || '', hostedPaused: false }
    const hosted = await api.hostedAI()
    return { ready: hosted.enabled, name: profile.name, hostedPaused: !hosted.enabled }
  }, enabled: !readOnly, staleTime: 0, refetchOnWindowFocus: true })
  return { ...query, ready: readOnly || !!query.data?.ready, reason: query.error ? 'AI 配置状态读取失败，请重试' : query.isPending && !readOnly ? '正在检查默认 AI 配置' : query.data?.hostedPaused ? '免费 AI 暂停服务，可在设置中选择自备服务' : !readOnly && !query.data?.ready ? '请先在设置中启用免费 AI，或配置自备服务并设为默认' : '' }
}
