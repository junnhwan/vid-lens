import { useQuery } from '@tanstack/react-query'
import { api, getToken } from '@/lib/api'
import type { CapabilityActionKey } from '@/lib/types'

const labels: Record<string, string> = { llm: '对话模型', asr: '语音识别', embedding: '向量模型', vision: '视觉理解', ocr: '本地 OCR', alignment: '句子对齐', rerank: '检索重排' }
const reasons: Record<string, string> = {
  missing_configuration: '请补齐本次操作所需的默认 AI 配置', hosted_paused: 'Free API 暂停，可在设置中选择自己的 API 配置',
  deployment_disabled: '服务端尚未开启本次操作所需能力', dependency_missing: '本次操作的本地依赖尚未安装', user_disabled: '此能力已关闭',
}

// This projection checks model/tool admission; submission still checks resources.
export function useAIAvailability(readOnly = false, action: CapabilityActionKey = 'chat') {
  const query = useQuery({ queryKey: ['ai-action-availability', getToken()], queryFn: () => api.optionalCapabilities(), enabled: !readOnly && action !== 'upload', staleTime: 0, refetchOnWindowFocus: true })
  const admission = query.data?.actions?.[action]
  const required = admission?.required_capabilities || []
  const capabilities = required.map(key => {
    const state = query.data?.capabilities?.find(candidate => candidate.key === key)
    return { key, label: labels[key] || key, model: state?.model || '', ready: !!state?.effective_enabled }
  })
  const missing = capabilities.filter(capability => !capability.ready).map(capability => capability.label)
  const ready = readOnly || action === 'upload' || (!query.error && !!admission?.allowed)
  const reason = ready ? '' : query.error ? '能力状态读取失败，请重试' : query.isPending ? '正在检查本次操作所需能力' : `${reasons[admission?.reason_code || ''] || '能力状态尚未确认，请刷新后重试'}${missing.length ? `：${missing.join('、')}` : ''}`
  return { ...query, ready, reason, capabilities, missing, profileName: '', hostedPaused: admission?.reason_code === 'hosted_paused' }
}
