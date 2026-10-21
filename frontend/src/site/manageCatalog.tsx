import { ApiError } from "../api/error"
import { ChoiceMenu, type ChoiceOption } from "../ui/ChoiceMenu"
import { liveStatusText } from "./probeStatus"
export interface Provider {
  id: string
  revision: number
  revision_id: string
  name: string
  base_url: string
  enabled: boolean
  key_configured: boolean
  proxy_policy: string
}

export interface DiscoveredModel {
  id: string
  display_name?: string
}

export interface Model {
  id: string
  revision: number
  revision_id: string
  provider_id: string
  provider_revision_id: string
  display_name: string
  brand_id: string
  description: string
  upstream_id: string
  enabled: boolean
  provider_enabled: boolean
  verified: boolean
  is_default: boolean
  recommended: boolean
  max_output_tokens: number
  max_input_chars: number
  input_chars_limit: number
  context_tokens: number
  disable_thinking: boolean
  cache_policy: string
  route_priority: number
  route_family: string
}

export interface Probe {
  id: string
  status: string
  mode: string
  submissions: number
  created_at: string
  started_at?: string
  updated_at?: string
  failure_code?: string
  result?: {
    diagnostic?: { http_status?: number; provider_error_code?: string; provider_error_param?: string }
    scope?: string
    probe_output_tokens?: number
    configured_output_tokens?: number
    cache_hit_observed?: boolean
    usage_reported?: boolean
    cost_reporting?: string
  }
}

export interface ProviderDraft {
  id?: string
  name: string
  base_url: string
  api_key: string
  enabled: boolean
  expected_revision: number
  proxy_policy: string
}

export interface ModelDraft {
  id?: string
  provider_id: string
  display_name: string
  brand_id: string
  description: string
  upstream_id: string
  enabled: boolean
  expected_revision: number
  max_output_tokens: number
  max_input_chars: number
  input_chars_limit: number
  context_tokens: number
  disable_thinking: boolean
  cache_policy: string
}

export const emptyProvider: ProviderDraft = {
  name: "",
  base_url: "",
  api_key: "",
  enabled: true,
  expected_revision: 0,
  proxy_policy: "server",
}

export const emptyModel: ModelDraft = {
  provider_id: "",
  display_name: "",
  brand_id: "generic",
  description: "",
  upstream_id: "",
  enabled: false,
  expected_revision: 0,
  max_output_tokens: 10000,
  max_input_chars: 180000,
  input_chars_limit: 140000,
  context_tokens: 0,
  disable_thinking: false,
  cache_policy: "provider_default",
}

export const labels: Record<string, string> = {
  queued: "准备检查",
  running: "正在检查",
  succeeded: "检查通过",
  failed: "检查失败",
  unknown: "结果未知",
}

export const sections = [
  ["models", "模型"],
  ["providers", "供应商"],
  ["diagnostics", "缓存与用量"],
  ["execution", "任务与队列"],
  ["network", "联网搜索"],
  ["videos", "视频与存储"],
  ["examples", "公开示例"],
  ["announcement", "公告"],
] as const

export const usagePageSize = 20

export const proxyOptions: ChoiceOption[] = [
  { id: "server", label: "使用服务器代理配置", hint: "沿用这台机器已有的出站代理" },
  { id: "direct", label: "直接连接公开端点", hint: "不经过服务器代理" },
]

export const cacheOptions: ChoiceOption[] = [
  { id: "provider_default", label: "供应商默认", hint: "沿用端点自己的缓存行为" },
  { id: "disabled_hints", label: "不发送显式提示", hint: "请求里不附带缓存提示" },
  { id: "qwen_explicit", label: "Qwen 显式", hint: "须经探测验证后才启用提示" },
]

export const brandOptions: ChoiceOption[] = [
  ["generic", "通用", "中性标记；显示名仍可识别已知品牌"],
  ["openai", "OpenAI"],
  ["claude", "Claude"],
  ["gemini", "Gemini"],
  ["grok", "Grok"],
  ["meta", "Llama", "Meta 开源模型"],
  ["mistral", "Mistral"],
  ["gemma", "Gemma", "Google 开源系列"],
  ["cohere", "Cohere"],
  ["qwen", "Qwen"],
  ["deepseek", "DeepSeek"],
  ["kimi", "Kimi"],
  ["glm", "GLM"],
  ["minimax", "MiniMax"],
  ["mimo", "MiMo"],
  ["doubao", "豆包"],
  ["hunyuan", "混元"],
  ["wenxin", "文心", "百度文心 / ERNIE"],
  ["spark", "星火", "讯飞星火"],
  ["stepfun", "阶跃", "阶跃星辰 Step"],
  ["yi", "零一", "零一万物 Yi"],
  ["baichuan", "百川", "百川智能"],
].map(([id, label, hint]) => ({ id, label, brand: id, hint }))

export function DeskSelect({
  label,
  value,
  options,
  placeholder,
  disabled,
  unavailable,
  onChange,
}: {
  label: string
  value: string
  options: ChoiceOption[]
  placeholder: string
  disabled?: boolean
  unavailable?: string
  onChange: (id: string) => void
}) {
  return (
    <div className="desk-field">
      <span>{label}</span>
      <ChoiceMenu
        label={label}
        value={value}
        options={options}
        placeholder={placeholder}
        disabled={disabled}
        unavailable={unavailable}
        onChange={onChange}
      />
    </div>
  )
}

export function InkSwitch({ checked, label, disabled, onChange }: { checked: boolean; label: string; disabled?: boolean; onChange: (checked: boolean) => void }) {
  return (
    <label className="ink-check">
      <input type="checkbox" checked={checked} disabled={disabled} onChange={(e) => onChange(e.target.checked)} />
      <span />
      {label}
    </label>
  )
}

export function LensGlyph() {
  return (
    <svg viewBox="0 0 16 16" aria-hidden="true">
      <circle cx="7" cy="7" r="4.2" fill="none" stroke="currentColor" strokeWidth="1.4" />
      <path d="M10.2 10.2 13.2 13.2" fill="none" stroke="currentColor" strokeWidth="1.4" strokeLinecap="round" />
    </svg>
  )
}

export function message(e: unknown) {
  return e instanceof ApiError ? e.detail : e instanceof Error ? e.message : "操作未完成，请重新读取状态。"
}

export function sameRevision(draft: ModelDraft, model: Model) {
  return (
    draft.provider_id === model.provider_id &&
    draft.display_name === model.display_name &&
    draft.brand_id === model.brand_id &&
    draft.description === model.description &&
    draft.upstream_id === model.upstream_id &&
    draft.max_output_tokens === model.max_output_tokens &&
    draft.max_input_chars === model.max_input_chars &&
    draft.input_chars_limit === (model.input_chars_limit || 0) &&
    draft.context_tokens === (model.context_tokens || 0) &&
    draft.disable_thinking === model.disable_thinking &&
    draft.cache_policy === model.cache_policy
  )
}

export type ModelTone = "ready" | "held" | "wait" | "dim" | "live"

export function modelState(model: Model, active: Probe | undefined, now: number): { text: string; tone: ModelTone } {
  if (active) return { text: liveStatusText(active, now), tone: "live" }
  if (!model.provider_enabled) return { text: "供应商已停用", tone: "dim" }
  if (model.enabled && model.verified) return { text: "可选用", tone: "ready" }
  if (model.verified) return { text: "已检查 · 未启用", tone: "held" }
  if (model.enabled) return { text: "已启用 · 待复核", tone: "wait" }
  return { text: "待检查", tone: "wait" }
}

export function catalogLine(model: Model, active: Probe | undefined, now: number): { text: string; tone: ModelTone } {
  const state = modelState(model, active, now)
  if (!active && model.provider_enabled && model.enabled && model.verified && model.is_default) return { ...state, text: "可选用 · 默认" }
  return state
}

export function checkedFirst(items: Model[]) {
  return items.slice().sort((a, b) => Number(b.verified) - Number(a.verified))
}

export function nextStep(model: Model): "" | "probe" | "enable" | "default" {
  if (!model.provider_enabled) return ""
  if (!model.verified) return "probe"
  if (!model.enabled) return "enable"
  if (!model.is_default) return "default"
  return ""
}

export function actionNote(model: Model, unsaved: boolean, checking: boolean) {
  if (unsaved) return "先保存或撤销参数，再检查、启用或设为默认。"
  if (!model.provider_enabled) return "供应商已停用。先打开它，才能检查或选用这个模型。"
  if (checking) return "这次检查还在进行，切换到其他模型也不会中断。"
  if (!model.verified) return "先做一次图片与工具检查。通过之后才能启用。检查会真实调用模型。"
  if (!model.enabled) return "检查已经通过。启用后，这个模型会出现在访客面前。"
  return ""
}

export function providerDraft(p: Provider): ProviderDraft {
  return {
    id: p.id,
    name: p.name,
    base_url: p.base_url,
    api_key: "",
    enabled: p.enabled,
    expected_revision: p.revision,
    proxy_policy: p.proxy_policy || "server",
  }
}

export function modelDraft(m: Model): ModelDraft {
  return {
    id: m.id,
    provider_id: m.provider_id,
    display_name: m.display_name,
    brand_id: m.brand_id,
    description: m.description,
    upstream_id: m.upstream_id,
    enabled: m.enabled,
    expected_revision: m.revision,
    max_output_tokens: m.max_output_tokens,
    max_input_chars: m.max_input_chars,
    input_chars_limit: m.input_chars_limit || 0,
    context_tokens: m.context_tokens || 0,
    disable_thinking: m.disable_thinking,
    cache_policy: m.cache_policy,
  }
}
