import { useEffect, useRef, useState } from "react"
import { NetworkSettings } from "./NetworkSettings"
import { HttpApi } from "../api/http"
import { ApiError } from "../api/error"
import { canReplay, newIdempotencyKey, pendingWrite, rememberWrite, forgetWrite, memoryStore } from "../api/writes"
import { SiteHeader, SiteFooter } from "../ui/SiteHeader"
import { ModelIcon } from "../ui/ModelIdentity"
import { ChoiceMenu, type ChoiceOption } from "../ui/ChoiceMenu"
import { useRise } from "../ui/countUp"
import { isOpenProbe, liveStatusText, modelActivity, openProbe, probeTiming } from "./probeStatus"

interface Provider {
  id: string
  revision: number
  revision_id: string
  name: string
  base_url: string
  enabled: boolean
  key_configured: boolean
  proxy_policy: string
}
interface Model {
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
  max_output_tokens: number
  max_input_chars: number
  input_chars_limit: number
  context_tokens: number
  disable_thinking: boolean
  cache_policy: string
}
interface Probe {
  id: string
  status: string
  mode: string
  submissions: number
  created_at: string
  updated_at?: string
  failure_code?: string
  result?: {
    cache_hit_observed?: boolean
    usage_reported?: boolean
    cost_reporting?: string
  }
}
interface ProviderDraft {
  id?: string
  name: string
  base_url: string
  api_key: string
  enabled: boolean
  expected_revision: number
  proxy_policy: string
}
interface ModelDraft {
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
const emptyProvider: ProviderDraft = {
  name: "",
  base_url: "",
  api_key: "",
  enabled: true,
  expected_revision: 0,
  proxy_policy: "server",
}
const emptyModel: ModelDraft = {
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
const labels: Record<string, string> = {
  queued: "排队中",
  running: "正在检查",
  succeeded: "检查通过",
  failed: "检查失败",
  unknown: "结果未知",
}
const sections = [
  ["models", "模型"],
  ["providers", "供应商"],
  ["diagnostics", "缓存与用量"],
  ["network", "联网搜索"],
] as const
const usagePageSize = 20
const proxyOptions: ChoiceOption[] = [
  { id: "server", label: "使用服务器代理配置", hint: "沿用这台机器已有的出站代理" },
  { id: "direct", label: "直接连接公开端点", hint: "不经过服务器代理" },
]
const cacheOptions: ChoiceOption[] = [
  { id: "provider_default", label: "供应商默认", hint: "沿用端点自己的缓存行为" },
  { id: "disabled_hints", label: "不发送显式提示", hint: "请求里不附带缓存提示" },
  { id: "qwen_explicit", label: "Qwen 显式", hint: "须经探测验证后才启用提示" },
]
const brandOptions: ChoiceOption[] = [
  ["generic", "通用", "中性标记；显示名仍可识别已知品牌"],
  ["openai", "OpenAI"],
  ["claude", "Claude"],
  ["gemini", "Gemini"],
  ["qwen", "Qwen"],
  ["deepseek", "DeepSeek"],
  ["kimi", "Kimi"],
  ["glm", "GLM"],
  ["mimo", "MiMo"],
  ["doubao", "豆包"],
  ["hunyuan", "混元"],
].map(([id, label, hint]) => ({ id, label, brand: id, hint }))
function DeskSelect({
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
function InkSwitch({ checked, label, onChange }: { checked: boolean; label: string; onChange: (checked: boolean) => void }) {
  return (
    <label className="ink-check">
      <input type="checkbox" checked={checked} onChange={(e) => onChange(e.target.checked)} />
      <span />
      {label}
    </label>
  )
}
function LensGlyph() {
  return (
    <svg viewBox="0 0 16 16" aria-hidden="true">
      <circle cx="7" cy="7" r="4.2" fill="none" stroke="currentColor" strokeWidth="1.4" />
      <path d="M10.2 10.2 13.2 13.2" fill="none" stroke="currentColor" strokeWidth="1.4" strokeLinecap="round" />
    </svg>
  )
}
function message(e: unknown) {
  return e instanceof ApiError ? e.detail : e instanceof Error ? e.message : "操作未完成，请重新读取状态。"
}
function sameRevision(draft: ModelDraft, model: Model) {
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
function catalogLine(model: Model, active: Probe | undefined, now: number): { text: string; tone: "ready" | "held" | "wait" | "dim" | "live" } {
  if (active) return { text: liveStatusText(active, now), tone: "live" }
  if (!model.provider_enabled) return { text: "供应商已停用", tone: "dim" }
  if (model.enabled && model.verified && model.is_default) return { text: "可选用 · 默认", tone: "ready" }
  if (model.enabled && model.verified) return { text: "可选用", tone: "ready" }
  if (model.verified) return { text: "已检查，未启用", tone: "held" }
  if (model.enabled) return { text: "已启用，待复核", tone: "wait" }
  return { text: "待检查", tone: "wait" }
}
function checkedFirst(items: Model[]) {
  return items.slice().sort((a, b) => Number(b.verified) - Number(a.verified))
}
function nextStep(model: Model): "" | "probe" | "enable" | "default" {
  if (!model.provider_enabled) return ""
  if (!model.verified) return "probe"
  if (!model.enabled) return "enable"
  if (!model.is_default) return "default"
  return ""
}
function actionNote(model: Model, unsaved: boolean, checking: boolean) {
  if (unsaved) return "先保存或撤销参数，再检查、启用或设为默认。"
  if (!model.provider_enabled) return "供应商已停用。先打开它，才能检查或选用这个模型。"
  if (checking) return "这次检查还在进行，切换到其他模型也不会中断。"
  if (!model.verified) return "先做一次图片与工具检查。通过之后才能启用。检查会真实调用模型。"
  if (!model.enabled) return "检查已经通过。启用后，这个模型会出现在访客面前。"
  return ""
}
function providerDraft(p: Provider): ProviderDraft {
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
function modelDraft(m: Model): ModelDraft {
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
export function Manage() {
  const api = useRef(new HttpApi())
  const [auth, setAuth] = useState<boolean | null>(null),
    [password, setPassword] = useState(""),
    [reveal, setReveal] = useState(false)
  const [section, setSection] = useState("models"),
    [busy, setBusy] = useState(""),
    [error, setError] = useState(""),
    [notice, setNotice] = useState("")
  const [providers, setProviders] = useState<Provider[]>([]),
    [models, setModels] = useState<Model[]>([]),
    [revision, setRevision] = useState(0)
  const [pd, setPd] = useState<ProviderDraft>(emptyProvider),
    [md, setMd] = useState<ModelDraft>(emptyModel),
    [dirty, setDirty] = useState(false)
  const [discovery, setDiscovery] = useState<string[]>([]),
    [picked, setPicked] = useState<string[]>([]),
    [filter, setFilter] = useState(""),
    [composing, setComposing] = useState(false),
    [advanced, setAdvanced] = useState(false),
    [ready, setReady] = useState(false)
  const [probeByModel, setProbeByModel] = useState<Record<string, Probe[]>>({}),
    [now, setNow] = useState(() => Date.now()),
    [diagnostics, setDiagnostics] = useState<Record<string, unknown> | null>(null),
    [usageOffset, setUsageOffset] = useState(0)
  const storage = useRef(
    (() => {
      try {
        return sessionStorage
      } catch {
        return memoryStore()
      }
    })(),
  )
  useEffect(() => {
    document.title = "站长管理 · VidLens"
    const meta = document.createElement("meta")
    meta.name = "robots"
    meta.content = "noindex,nofollow"
    document.head.appendChild(meta)
    void api.current
      .request<{ csrf_token: string }>("GET", "/api/v1/admin/session")
      .then((r) => {
        api.current.csrfToken = r.body.csrf_token
        setAuth(true)
      })
      .catch(() => setAuth(false))
    return () => meta.remove()
  }, [])
  useEffect(() => {
    if (!dirty) return
    const warn = (e: BeforeUnloadEvent) => {
      e.preventDefault()
      e.returnValue = ""
    }
    addEventListener("beforeunload", warn)
    return () => removeEventListener("beforeunload", warn)
  }, [dirty])
  async function load() {
    const [p, m] = await Promise.all([
      api.current.request<{ items: Provider[] }>("GET", "/api/v1/admin/providers"),
      api.current.request<{ items: Model[]; settings: { revision: number } }>("GET", "/api/v1/admin/models"),
    ])
    setProviders(p.body.items)
    setModels(m.body.items)
    setRevision(m.body.settings.revision)
    setReady(true)
    return m.body.items
  }
  useEffect(() => {
    if (auth) void load().catch((e) => setError(message(e)))
  }, [auth])
  const watched = [
    ...new Set(
      [composing ? "" : md.id, ...Object.keys(probeByModel).filter((id) => modelActivity(probeByModel[id]))].filter(
        (id): id is string => Boolean(id),
      ),
    ),
  ].sort()
  const watchKey = watched.join(",")
  const ticking = watched.some((id) => Boolean(modelActivity(probeByModel[id])))
  useEffect(() => {
    if (!ticking) return
    const timer = setInterval(() => setNow(Date.now()), 1000)
    return () => clearInterval(timer)
  }, [ticking])
  useEffect(() => {
    if (!auth || !watchKey) return
    const ids = watchKey.split(",")
    let stop = false,
      timer: ReturnType<typeof setTimeout> | undefined
    async function poll() {
      if (document.hidden) {
        timer = setTimeout(poll, 2000)
        return
      }
      let failure: unknown
      const pages = await Promise.all(
        ids.map(async (id) => {
          try {
            const r = await api.current.request<{ items: Probe[] }>("GET", `/api/v1/admin/models/${id}/probes`)
            return { id, items: r.body.items, failed: false }
          } catch (e) {
            failure = failure ?? e
            return { id, items: [] as Probe[], failed: true }
          }
        }),
      )
      if (stop) return
      setProbeByModel((prev) => {
        const next = { ...prev }
        for (const page of pages) if (!page.failed) next[page.id] = page.items
        return next
      })
      const still = pages.some((page) => (page.failed ? Boolean(modelActivity(probeByModel[page.id])) : page.items.some(isOpenProbe)))
      if (failure) setError(message(failure))
      if (still) timer = setTimeout(poll, 2000)
      else if (!failure) void load().catch(() => {})
    }
    void poll()
    return () => {
      stop = true
      if (timer) clearTimeout(timer)
    }
  }, [auth, watchKey, busy])
  useEffect(() => {
    if (!auth || composing || md.id || !models.length) return
    const preferred = models.find((m) => m.is_default) || models.find((m) => m.enabled && m.verified && m.provider_enabled) || models[0]
    setMd(modelDraft(preferred))
  }, [auth, composing, md.id, models])
  async function action(name: string, fn: () => Promise<void>) {
    if (busy) return
    setBusy(name)
    setError("")
    setNotice("")
    try {
      await fn()
    } catch (e) {
      if (e instanceof ApiError && e.status === 401) setAuth(false)
      setError(message(e))
    } finally {
      setBusy("")
    }
  }
  async function login() {
    await action("login", async () => {
      const r = await api.current.request<{ csrf_token: string }>("POST", "/api/v1/admin/login", { password })
      api.current.csrfToken = r.body.csrf_token
      setPassword("")
      setAuth(true)
    })
  }
  function editProvider(p?: Provider) {
    if (dirty) {
      setError("当前表单有未保存内容，请先保存或撤销。")
      return
    }
    setPd(p ? providerDraft(p) : emptyProvider)
    setDiscovery([])
    setPicked([])
  }
  function editModel(m?: Model) {
    if (dirty) {
      setError("请先保存或撤销当前表单。")
      return
    }
    if (m && m.id === md.id && !composing) return
    setError("")
    setAdvanced(false)
    if (!m) {
      setComposing(true)
      setMd({ ...emptyModel, provider_id: providers[0]?.id || "" })
      return
    }
    setComposing(false)
    setMd(modelDraft(m))
  }
  function cancelCompose() {
    if (dirty) {
      setError("请先保存或撤销当前表单。")
      return
    }
    setComposing(false)
    setAdvanced(false)
    setError("")
    if (!models.length) {
      setMd(emptyModel)
      return
    }
    const preferred = models.find((m) => m.is_default) || models[0]
    setMd(modelDraft(preferred))
  }
  function openSection(id: string) {
    if (dirty) {
      setError("请先保存或撤销当前表单。")
      return false
    }
    setError("")
    setSection(id)
    if (id === "diagnostics") loadDiagnostics(0)
    return true
  }
  function openProviders(target?: Provider) {
    if (!openSection("providers")) return
    if (target) editProvider(target)
    else if (!pd.id && providers[0]) editProvider(providers[0])
  }
  function beginAddProvider() {
    if (!openSection("providers")) return
    editProvider()
  }
  function loadDiagnostics(offset = 0) {
    void action("diagnostics", async () => {
      const r = await api.current.request<Record<string, unknown>>(
        "GET",
        `/api/v1/admin/diagnostics?limit=${usagePageSize}&offset=${offset}`,
      )
      setUsageOffset(offset)
      setDiagnostics(r.body)
    })
  }
  async function saveProvider() {
    await action("provider", async () => {
      const { id, ...body } = pd
      const r = await api.current.request<Provider>(
        id ? "PUT" : "POST",
        id ? `/api/v1/admin/providers/${id}` : "/api/v1/admin/providers",
        body,
      )
      setPd(providerDraft(r.body))
      setDirty(false)
      setNotice("供应商已保存。更换密钥或端点不会改写旧请求的修订。")
      await load()
    })
  }
  async function saveModel() {
    await action("model", async () => {
      const { id, ...body } = md
      const current = models.find((item) => item.id === id)
      const unchanged = Boolean(id && current && sameRevision(md, current))
      const r = await api.current.request<Model>(id ? "PUT" : "POST", id ? `/api/v1/admin/models/${id}` : "/api/v1/admin/models", {
        ...body,
        enabled: unchanged ? Boolean(current?.enabled) : false,
      })
      setMd(modelDraft(r.body))
      setDirty(false)
      setComposing(false)
      setAdvanced(false)
      setNotice(
        !id
          ? "已保存为停用模型。通过图片与工具检查后再启用。"
          : unchanged
            ? "模型已保存。"
            : "已保存为新修订，并先停用。通过图片与工具检查后再启用。",
      )
      await load()
    })
  }
  async function toggleModel(m: Model) {
    await action(m.id, async () => {
      const { id, ...body } = modelDraft(m)
      await api.current.request("PUT", `/api/v1/admin/models/${id}`, {
        ...body,
        enabled: !m.enabled,
      })
      const items = await load()
      const fresh = items.find((item) => item.id === m.id)
      if (fresh) setMd((draft) => (draft.id === fresh.id ? modelDraft(fresh) : draft))
      setNotice(m.enabled ? "模型已停用。旧文章仍可阅读，新的解读不会再调用它。" : "模型已启用，访客可以选到它。")
    })
  }
  async function makeDefault(m: Model) {
    await action("default", async () => {
      await api.current.request("PUT", "/api/v1/admin/model-settings", {
        default_model_id: m.id,
        expected_revision: revision,
      })
      await load()
      setNotice("默认模型已更新，只影响新请求的预选。")
    })
  }
  async function importCurrent() {
    await action("import-current", async () => {
      await api.current.request("POST", "/api/v1/admin/models/import-current", {})
      await load()
      setNotice("现有模型已导入为停用候选。先检查，再启用。")
    })
  }
  async function probe(m: Model, mode: string) {
    await action("probe", async () => {
      const path = `/api/v1/admin/models/${m.id}/probes`,
        body = { revision_id: m.revision_id, mode },
        slot = `probe:${m.id}:${mode}`,
        encoded = JSON.stringify(body),
        old = pendingWrite(storage.current, slot),
        key = old?.body === encoded && canReplay(old, Date.now()) ? old.key : newIdempotencyKey()
      rememberWrite(storage.current, {
        slot,
        key,
        path,
        body: encoded,
        startedAt: Date.now(),
        expiresAt: Date.now() + 86400000,
      })
      await api.current.request("POST", path, body, key)
      forgetWrite(storage.current, slot)
      const listed = await api.current.request<{ items: Probe[] }>("GET", `/api/v1/admin/models/${m.id}/probes`)
      setProbeByModel((prev) => ({ ...prev, [m.id]: listed.body.items }))
      setNow(Date.now())
      setNotice("已接受检查。浏览器超时后会读取原任务，结果未知不会自动重复扣费调用。")
    })
  }
  async function discover() {
    if (!pd.id) return
    await action("discover", async () => {
      const r = await api.current.request<{ items: { id: string }[] }>("POST", `/api/v1/admin/providers/${pd.id}/discover`, {})
      setDiscovery(r.body.items.map((x) => x.id))
      setPicked([])
      setNotice("仅发现模型名字；未探测、未启用，也未更改默认模型。")
    })
  }
  async function importModels() {
    if (!pd.id) return
    await action("import", async () => {
      for (const id of picked)
        await api.current.request("POST", "/api/v1/admin/models", {
          ...emptyModel,
          provider_id: pd.id,
          display_name: id,
          upstream_id: id,
        })
      setPicked([])
      await load()
      setNotice("所选候选已保存为停用模型。请逐个检查实际图片和工具能力。")
    })
  }
  const selected = models.find((m) => m.id === md.id)
  const enabledCount = models.filter((m) => m.enabled && m.provider_enabled).length
  const risenEnabled = useRise(auth ? enabledCount : undefined, auth === true)
  const filtered = checkedFirst(
    models.filter((m) =>
      [m.display_name, m.upstream_id, providers.find((p) => p.id === m.provider_id)?.name].join(" ").toLowerCase().includes(filter.toLowerCase()),
    ),
  )
  const tab = String(Math.max(0, sections.findIndex(([id]) => id === section)))
  const selectedProbes = selected ? (probeByModel[selected.id] ?? []) : []
  const capabilityProbe = openProbe(selectedProbes, "capabilities")
  const cacheProbe = openProbe(selectedProbes, "cache")
  const checking = Boolean(capabilityProbe)
  const cacheChecking = Boolean(cacheProbe)
  function providerName(id: string) {
    return providers.find((p) => p.id === id)?.name || "未关联供应商"
  }
  function modelMeta(model: { display_name: string; upstream_id: string; provider_id: string }) {
    const name = providerName(model.provider_id)
    return model.upstream_id === model.display_name ? name : `${model.upstream_id} · ${name}`
  }
  function patchModel(partial: Partial<ModelDraft>) {
    setMd((draft) => ({ ...draft, ...partial }))
    setDirty(true)
  }
  function revertModel() {
    setDirty(false)
    setMd(selected ? modelDraft(selected) : { ...emptyModel, provider_id: providers[0]?.id || "" })
  }
  return (
    <>
      <SiteHeader />
      <main className={`manage-layout ${auth ? "" : "manage-login-layout"}`}>
        <header className="manage-heading">
          <div>
            <p className="eyebrow">
              <span />
              VIDLENS / OWNER DESK
            </p>
            <h1>{auth ? "站长管理" : "站长登录"}</h1>
            <p>{auth ? "配置模型、供应商和网络补充，查看实际用量。" : "管理供应商、模型、联网搜索与运行诊断。"}</p>
          </div>
          {auth ? (
            <button
              type="button"
              className="btn quiet desk-pill"
              disabled={!!busy}
              onClick={() =>
                void action("logout", async () => {
                  await api.current.request("DELETE", "/api/v1/admin/session", {})
                  setAuth(false)
                })
              }
            >
              退出登录 <span className="nudge">↗</span>
            </button>
          ) : null}
        </header>
        {auth === null ? (
          <p className="desk-wait" role="status">
            <i />
            正在读取管理会话…
          </p>
        ) : !auth ? (
          <form
            className="owner-login"
            onSubmit={(e) => {
              e.preventDefault()
              void login()
            }}
          >
            <p className="eyebrow">
              <span />
              PASSWORD
            </p>
            <label htmlFor="owner-password">站长密码</label>
            <div className="owner-secret">
              <input
                id="owner-password"
                type={reveal ? "text" : "password"}
                value={password}
                autoComplete="current-password"
                onChange={(e) => setPassword(e.target.value)}
                required
              />
              <button type="button" className="text-action" aria-pressed={reveal} onClick={() => setReveal((open) => !open)}>
                {reveal ? "隐藏" : "显示"}
              </button>
            </div>
            <button className="btn primary" disabled={!!busy}>
              {busy === "login" ? (
                "正在校验"
              ) : (
                <>
                  进入管理后台 <span className="nudge-forward">→</span>
                </>
              )}
            </button>
            <p>初始密码或重置需在服务器运行交互式命令。本站没有默认密码、公开注册或邮件找回。</p>
            <code>vidlens -config … -set-owner-password</code>
          </form>
        ) : (
          <>
            <div className="desk-nav">
              <nav className="manage-tabs" data-tab={tab} aria-label="管理分区">
                <i className="tab-indicator" aria-hidden="true" />
                {sections.map(([id, label]) => (
                  <button
                    key={id}
                    type="button"
                    aria-current={section === id ? "page" : undefined}
                    onClick={() => (id === "providers" ? openProviders() : openSection(id))}
                  >
                    {label}
                  </button>
                ))}
              </nav>
              <p className="desk-census">
                <strong>{risenEnabled ?? 0}</strong>
                <span>个启用模型</span>
              </p>
            </div>
            {section === "models" && ready && models.length > 0 && enabledCount === 0 ? (
              <p className="desk-quiet-note">目录里还没有可选用的模型。检查通过并启用后，才会出现在访客面前。</p>
            ) : null}
            {error ? (
              <p className="management-message error" role="alert">
                {error}
              </p>
            ) : null}
            {notice ? (
              <p className="management-message" role="status">
                {notice}
              </p>
            ) : null}
            <div className="desk-stage" key={section}>
            {section === "network" ? (
              <NetworkSettings api={api.current} onDirty={setDirty} onAuthLost={() => setAuth(false)} />
            ) : section === "providers" ? (
              <div className="manage-columns">
                <section className="provider-rail">
                  <div className="manage-list-head">
                    <h2>供应商</h2>
                    <div className="head-actions">
                      <button type="button" className="text-action" onClick={() => openSection("models")}>
                        返回模型
                      </button>
                      <button type="button" className="btn quiet desk-pill" onClick={() => editProvider()}>
                        新增
                      </button>
                    </div>
                  </div>
                  {providers.length ? (
                    providers.map((p) => (
                      <button
                        type="button"
                        className="provider-item"
                        key={p.id}
                        aria-current={pd.id === p.id ? "true" : undefined}
                        onClick={() => editProvider(p)}
                      >
                        <span className={`status-dot ${p.enabled ? "on" : ""}`} />
                        <span>
                          <strong>{p.name}</strong>
                          <small>
                            {p.enabled ? "已启用" : "已停用"} · {p.key_configured ? "密钥已配置" : "未配置密钥"}
                          </small>
                        </span>
                        <span className="nudge">↗</span>
                      </button>
                    ))
                  ) : (
                    <p className="empty-state">还没有供应商。从右侧添加一个 OpenAI 兼容端点。</p>
                  )}
                </section>
                <section className="manage-form">
                  <h2>
                    {pd.id ? "编辑供应商" : "添加供应商"}
                    {dirty ? <span className="desk-dirty">未保存</span> : null}
                  </h2>
                  <form
                    onSubmit={(e) => {
                      e.preventDefault()
                      void saveProvider()
                    }}
                  >
                    <label className="desk-field">
                      <span>名称</span>
                      <input
                        value={pd.name}
                        onChange={(e) => {
                          setPd({ ...pd, name: e.target.value })
                          setDirty(true)
                        }}
                        required
                      />
                    </label>
                    <label className="desk-field">
                      <span>OpenAI 兼容 base URL</span>
                      <input
                        value={pd.base_url}
                        placeholder="https://api.example.com/v1"
                        spellCheck={false}
                        onChange={(e) => {
                          setPd({ ...pd, base_url: e.target.value })
                          setDirty(true)
                        }}
                        required
                      />
                    </label>
                    <label className="desk-field">
                      <span>{pd.id ? "更新密钥（留空保持原值）" : "API Key"}</span>
                      <input
                        type="password"
                        autoComplete="new-password"
                        value={pd.api_key}
                        spellCheck={false}
                        onChange={(e) => {
                          setPd({ ...pd, api_key: e.target.value })
                          setDirty(true)
                        }}
                      />
                    </label>
                    <DeskSelect
                      label="出站策略"
                      value={pd.proxy_policy}
                      options={proxyOptions}
                      placeholder="选择出站策略"
                      onChange={(id) => {
                        setPd({ ...pd, proxy_policy: id })
                        setDirty(true)
                      }}
                    />
                    <div className="desk-switches">
                      <InkSwitch
                        checked={pd.enabled}
                        label="启用供应商"
                        onChange={(checked) => {
                          setPd({ ...pd, enabled: checked })
                          setDirty(true)
                        }}
                      />
                    </div>
                    <p className="fine">Key 保存后只显示配置状态。轮换密钥创建新修订；撤销旧 Key 可能让旧视频暂时不能继续问答。</p>
                    <div className="form-actions">
                      <button className="btn primary" disabled={!!busy}>
                        {busy === "provider" ? "正在保存" : "保存供应商"}
                      </button>
                      <button
                        type="button"
                        className="btn quiet desk-pill"
                        onClick={() => {
                          setDirty(false)
                          setPd(
                            providers.find((p) => p.id === pd.id) ? providerDraft(providers.find((p) => p.id === pd.id)!) : emptyProvider,
                          )
                        }}
                      >
                        撤销修改
                      </button>
                      {pd.id ? (
                        <button type="button" className="btn quiet desk-pill" disabled={!!busy || dirty} onClick={() => void discover()}>
                          {busy === "discover" ? "正在发现" : "拉取模型列表"}
                        </button>
                      ) : null}
                    </div>
                  </form>
                  <div className="fold" data-open={discovery.length ? "true" : "false"}>
                    <div className="fold-clip">
                      {discovery.length ? (
                        <div className="discovered">
                          <div className="discovered-head">
                            <h3>
                              发现的候选 <span>{discovery.length}</span>
                            </h3>
                            <button
                              type="button"
                              className="text-action"
                              onClick={() => setPicked(picked.length === discovery.length ? [] : [...discovery])}
                            >
                              {picked.length === discovery.length ? "取消全选" : "全选"}
                            </button>
                          </div>
                          <p className="fine">发现只表示名字存在，不代表看图或工具能力。</p>
                          {discovery.map((id) => (
                            <label className="ink-check" key={id}>
                              <input
                                type="checkbox"
                                checked={picked.includes(id)}
                                onChange={(e) => setPicked((prev) => (e.target.checked ? [...prev, id] : prev.filter((v) => v !== id)))}
                              />
                              <span />
                              <code>{id}</code>
                            </label>
                          ))}
                          <button type="button" className="btn primary" disabled={!!busy || !picked.length} onClick={() => void importModels()}>
                            导入 {picked.length} 个候选为停用模型
                          </button>
                        </div>
                      ) : null}
                    </div>
                  </div>
                </section>
              </div>
            ) : section === "models" ? (
              !ready ? (
                error ? null : (
                  <p className="desk-wait" role="status">
                    <i />
                    正在读取目录…
                  </p>
                )
              ) : !models.length && !composing ? (
                <section className="setup-sheet manage-form">
                  <p className="eyebrow">
                    <span />
                    先准备一个可选用的模型
                  </p>
                  <h2>三步之后，访客才能选到它</h2>
                  <ol className="setup-rail">
                    <li data-state={providers.length ? "done" : "current"}>
                      <b>1</b>
                      <div>
                        <strong>添加端点</strong>
                        <p>一个 OpenAI 兼容地址，和只保存一次的密钥。</p>
                        {providers.length ? (
                          <button type="button" className="text-action" onClick={() => openProviders()}>
                            查看 {providers.length} 个供应商
                          </button>
                        ) : (
                          <button type="button" className="btn primary" onClick={beginAddProvider}>
                            添加供应商
                          </button>
                        )}
                      </div>
                    </li>
                    <li data-state={providers.length ? "current" : "wait"}>
                      <b>2</b>
                      <div>
                        <strong>把模型放进目录</strong>
                        <p>从端点拉取名字，导入现有配置，或直接填写上游 ID。放进来的模型先保持停用。</p>
                        <div className="setup-actions">
                          <button
                            type="button"
                            className={providers.length ? "btn primary" : "btn quiet desk-pill"}
                            disabled={!providers.length}
                            onClick={() => openProviders()}
                          >
                            去拉取模型列表
                          </button>
                          <button type="button" className="btn quiet desk-pill" disabled={!!busy} onClick={() => void importCurrent()}>
                            导入现有配置
                          </button>
                          <button type="button" className="text-action" disabled={!providers.length} onClick={() => editModel()}>
                            手动填写
                          </button>
                        </div>
                      </div>
                    </li>
                    <li data-state="wait">
                      <b>3</b>
                      <div>
                        <strong>检查后启用</strong>
                        <p>用真实图片和工具试一次。通过并启用之后，才会出现在访客面前。</p>
                      </div>
                    </li>
                  </ol>
                </section>
              ) : (
                <div className={models.length ? "model-stage" : "model-compose"}>
                  {models.length ? (
                    <section className="model-ledger">
                      <div className="ledger-head">
                        <span>目录</span>
                        <span>{filter ? `${filtered.length} / ${models.length}` : models.length}</span>
                      </div>
                      <label className="ledger-search">
                        <span className="sr-only">筛选模型</span>
                        <LensGlyph />
                        <input
                          id="model-filter"
                          placeholder="筛选名称、供应商或上游 ID"
                          value={filter}
                          onChange={(e) => setFilter(e.target.value)}
                        />
                      </label>
                      <div className="ledger-scroll">
                        {filtered.length ? (
                          <ul className="ledger-list">
                            {filtered.map((m) => {
                              const line = catalogLine(m, modelActivity(probeByModel[m.id]), now)
                              return (
                                <li key={m.id} data-current={!composing && md.id === m.id ? "true" : "false"}>
                                  <button
                                    type="button"
                                    className="ledger-open"
                                    aria-current={!composing && md.id === m.id ? "true" : undefined}
                                    onClick={() => editModel(m)}
                                  >
                                    <span className="ledger-mark">
                                      <ModelIcon brand={m.brand_id} name={m.display_name} />
                                    </span>
                                    <span className="ledger-copy">
                                      <strong>{m.display_name}</strong>
                                      <small>{modelMeta(m)}</small>
                                      <em data-tone={line.tone}>{line.text}</em>
                                    </span>
                                  </button>
                                </li>
                              )
                            })}
                          </ul>
                        ) : (
                          <p className="empty-state">没有符合「{filter}」的模型。</p>
                        )}
                      </div>
                      <div className="ledger-foot">
                        <button type="button" className="text-action" aria-current={composing ? "true" : undefined} disabled={!providers.length} onClick={() => editModel()}>
                          手动添加
                        </button>
                      </div>
                    </section>
                  ) : null}
                  {composing || selected ? (
                    <section className="manage-form model-sheet" key={composing ? "new" : selected?.id}>
                      {composing ? (
                        <button type="button" className="text-action sheet-back" onClick={cancelCompose}>
                          {models.length ? "返回这条目录" : "返回准备步骤"}
                        </button>
                      ) : null}
                      <header className="sheet-id">
                        <span className="ledger-mark">
                          <ModelIcon brand={composing ? md.brand_id : selected?.brand_id} name={composing ? md.display_name : selected?.display_name} />
                        </span>
                        <div>
                          <h2>
                            {composing ? "添加模型" : selected?.display_name}
                            {dirty ? <span className="desk-dirty">未保存</span> : null}
                          </h2>
                          <p>{composing ? "先保存为停用。检查通过后才能启用。" : selected ? modelMeta(selected) : ""}</p>
                        </div>
                        {!composing && selected ? (
                          <span className={`desk-seal ${catalogLine(selected, modelActivity(selectedProbes), now).tone}`}>
                            {catalogLine(selected, modelActivity(selectedProbes), now).text}
                          </span>
                        ) : null}
                      </header>
                      {!composing && selected ? (
                        <div className="sheet-actions">
                          <button
                            type="button"
                            className={nextStep(selected) === "probe" && selected.provider_enabled && !dirty && !busy && !checking ? "btn primary" : "btn quiet desk-pill"}
                            disabled={!!busy || dirty || checking || !selected.provider_enabled}
                            onClick={() => void probe(selected, "capabilities")}
                          >
                            {checking && capabilityProbe ? (
                              <>
                                <span className="desk-spin" aria-hidden="true">
                                  ↻
                                </span>
                                {liveStatusText(capabilityProbe, now)}
                              </>
                            ) : (
                              "检查图片与工具"
                            )}
                          </button>
                          <button
                            type="button"
                            className={nextStep(selected) === "enable" && !dirty && !busy && !checking ? "btn primary" : "btn quiet desk-pill"}
                            disabled={!!busy || dirty || checking || (!selected.enabled && (!selected.verified || !selected.provider_enabled))}
                            onClick={() => void toggleModel(selected)}
                          >
                            {selected.enabled ? "停用" : "启用"}
                          </button>
                          <button
                            type="button"
                            className={nextStep(selected) === "default" && !dirty && !busy && !checking ? "btn primary" : "btn quiet desk-pill"}
                            disabled={!!busy || dirty || checking || !selected.enabled || !selected.verified || !selected.provider_enabled || selected.is_default}
                            onClick={() => void makeDefault(selected)}
                          >
                            {selected.is_default ? "已是默认" : "设为默认"}
                          </button>
                        </div>
                      ) : null}
                      {!composing && selected && actionNote(selected, dirty, Boolean(modelActivity(selectedProbes))) ? (
                        <p className="sheet-reason" id="model-action-note">
                          {actionNote(selected, dirty, Boolean(modelActivity(selectedProbes)))}
                          {!selected.provider_enabled ? (
                            <button type="button" className="text-action" onClick={() => openProviders(providers.find((p) => p.id === selected.provider_id))}>
                              打开这个供应商
                            </button>
                          ) : null}
                        </p>
                      ) : null}
                      <form
                        onSubmit={(e) => {
                          e.preventDefault()
                          if (!md.provider_id) {
                            setError("请先选择供应商。")
                            setNotice("")
                            return
                          }
                          void saveModel()
                        }}
                      >
                        {composing ? (
                          <div className="form-grid">
                            <DeskSelect
                              label="供应商"
                              value={md.provider_id}
                              placeholder={providers.length ? "选择供应商" : "还没有供应商"}
                              options={providers.map((p) => ({
                                id: p.id,
                                label: p.name,
                                hint: `${p.enabled ? "已启用" : "已停用"} · ${p.key_configured ? "密钥已配置" : "未配置密钥"}`,
                              }))}
                              onChange={(id) => patchModel({ provider_id: id })}
                            />
                            <label className="desk-field">
                              <span>显示名</span>
                              <input value={md.display_name} onChange={(e) => patchModel({ display_name: e.target.value })} required />
                            </label>
                            <label className="desk-field">
                              <span>上游 model ID</span>
                              <input value={md.upstream_id} spellCheck={false} onChange={(e) => patchModel({ upstream_id: e.target.value })} required />
                            </label>
                          </div>
                        ) : null}
                        <div className="tune" data-open={advanced ? "true" : "false"}>
                          <button
                            type="button"
                            className="tune-toggle"
                            aria-expanded={advanced}
                            onClick={() => {
                              if (advanced && (!md.display_name.trim() || !md.upstream_id.trim() || !md.provider_id)) {
                                setError("显示名、上游 ID 和供应商需要保留。")
                                return
                              }
                              setError("")
                              setAdvanced((open) => !open)
                            }}
                          >
                            <span>
                              <strong>调整参数</strong>
                              <small>额度、缓存、品牌与说明</small>
                            </span>
                            <i aria-hidden="true" />
                          </button>
                          {advanced ? (
                            <div className="tune-body">
                              {composing ? null : (
                                <div className="form-grid">
                                  <DeskSelect
                                    label="供应商"
                                    value={md.provider_id}
                                    placeholder={providers.length ? "选择供应商" : "还没有供应商"}
                                    disabled={!!md.id}
                                    unavailable={md.provider_id && !providers.some((p) => p.id === md.provider_id) ? "原供应商已不在目录中" : undefined}
                                    options={providers.map((p) => ({
                                      id: p.id,
                                      label: p.name,
                                      hint: `${p.enabled ? "已启用" : "已停用"} · ${p.key_configured ? "密钥已配置" : "未配置密钥"}`,
                                    }))}
                                    onChange={(id) => patchModel({ provider_id: id })}
                                  />
                                  <label className="desk-field">
                                    <span>显示名</span>
                                    <input value={md.display_name} onChange={(e) => patchModel({ display_name: e.target.value })} required />
                                  </label>
                                  <label className="desk-field">
                                    <span>上游 model ID</span>
                                    <input value={md.upstream_id} spellCheck={false} onChange={(e) => patchModel({ upstream_id: e.target.value })} required />
                                  </label>
                                </div>
                              )}
                              <div className="tune-grid">
                                <DeskSelect
                                  label="品牌标识"
                                  value={md.brand_id}
                                  options={brandOptions}
                                  placeholder="选择品牌"
                                  onChange={(id) => patchModel({ brand_id: id })}
                                />
                                <label className="desk-field">
                                  <span>输出 tokens 上限</span>
                                  <input
                                    type="number"
                                    min={1024}
                                    value={md.max_output_tokens}
                                    onChange={(e) => patchModel({ max_output_tokens: Number(e.target.value) })}
                                  />
                                </label>
                                <label className="desk-field">
                                  <span>兼容旧版字符额度</span>
                                  <input
                                    type="number"
                                    min={10000}
                                    value={md.max_input_chars}
                                    onChange={(e) => patchModel({ max_input_chars: Number(e.target.value) })}
                                  />
                                </label>
                                <label className="desk-field">
                                  <span>实际输入字符上限（0 沿用旧配置）</span>
                                  <input type="number" min={0} max={8000000} value={md.input_chars_limit} onChange={(e) => patchModel({ input_chars_limit: Number(e.target.value) })} />
                                </label>
                                <label className="desk-field">
                                  <span>共享上下文 tokens（0 表示未知）</span>
                                  <input type="number" min={0} max={4000000} value={md.context_tokens} onChange={(e) => patchModel({ context_tokens: Number(e.target.value) })} />
                                </label>
                                <DeskSelect
                                  label="缓存策略"
                                  value={md.cache_policy}
                                  options={cacheOptions}
                                  placeholder="选择缓存策略"
                                  onChange={(id) => patchModel({ cache_policy: id })}
                                />
                              </div>
                              <label className="desk-field">
                                <span>给访客的一句说明</span>
                                <input maxLength={240} value={md.description} onChange={(e) => patchModel({ description: e.target.value })} />
                              </label>
                              <div className="desk-switches">
                                <InkSwitch
                                  checked={md.disable_thinking}
                                  label="发送关闭思考的兼容参数"
                                  onChange={(checked) => patchModel({ disable_thinking: checked })}
                                />
                              </div>
                              {!composing && selected ? (
                                <div className="tune-extra">
                                  <button
                                    type="button"
                                    className="btn quiet desk-pill"
                                    disabled={!!busy || dirty || cacheChecking || !selected.provider_enabled}
                                    onClick={() => void probe(selected, "cache")}
                                  >
                                    {cacheChecking && cacheProbe ? (
                                      <>
                                        <span className="desk-spin" aria-hidden="true">
                                          ↻
                                        </span>
                                        {cacheProbe.status === "queued" ? "排队中" : "正在检查缓存"} · {probeTiming(cacheProbe, now)}
                                      </>
                                    ) : (
                                      "串行检查缓存"
                                    )}
                                  </button>
                                  <p className="fine">
                                    缓存检查单独串行，不批量探测。显式提示要等端点报告实际创建或命中。一次小样本通过，不代表长视频质量。
                                  </p>
                                </div>
                              ) : (
                                <p className="fine">这些值已按常用额度填好。改输出上限或输入保护会在保存时生成新修订。</p>
                              )}
                            </div>
                          ) : null}
                        </div>
                        {composing || dirty ? (
                          <>
                            <div className="form-actions">
                              <button className="btn primary" disabled={!!busy || (!composing && !dirty)}>
                                {busy === "model" ? "正在保存" : composing ? "保存为停用模型" : "保存参数"}
                              </button>
                              <button type="button" className="btn quiet desk-pill" onClick={revertModel}>
                                撤销
                              </button>
                            </div>
                            <p className="fine">
                              {composing
                                ? "保存后仍是停用。检查通过，才能启用。"
                                : "改名称、额度或缓存会生成新修订，并先停用。检查通过后再启用。"}
                            </p>
                          </>
                        ) : null}
                      </form>
                      {!composing && selected ? (
                        <section className="probe-history">
                          <h3>最近检查</h3>
                          {selectedProbes.length ? (
                            <ol className="probe-rail">
                              {selectedProbes.map((p) => {
                                const timing = probeTiming(p, now)
                                return (
                                <li key={p.id} data-status={p.status}>
                                  <i aria-hidden="true" />
                                  <div>
                                    <strong>
                                      {labels[p.status] || p.status}
                                      {isOpenProbe(p) && timing ? ` · ${timing}` : ""}
                                    </strong>
                                    <span>
                                      {p.mode === "cache" ? "缓存样本" : "图片与工具"} · {p.submissions} 次物理提交
                                    </span>
                                    {!isOpenProbe(p) && timing ? <small>用时 {timing}</small> : null}
                                    {p.failure_code ? <code>{p.failure_code}</code> : null}
                                    {p.mode === "cache" && p.status === "succeeded" ? (
                                      <small>
                                        {p.result?.usage_reported ? "缓存字段已报告" : "缓存字段未报告"} ·{" "}
                                        {p.result?.cache_hit_observed ? "观察到命中" : "未取得命中证据"} · 账单折扣未知
                                      </small>
                                    ) : null}
                                  </div>
                                  <time dateTime={p.created_at}>{new Date(p.created_at).toLocaleString("zh-Hans")}</time>
                                </li>
                                )
                              })}
                            </ol>
                          ) : (
                            <p className="fine">尚无检查记录。</p>
                          )}
                        </section>
                      ) : null}
                    </section>
                  ) : (
                    <p className="desk-wait" role="status">
                      <i />
                      正在读取目录…
                    </p>
                  )}
                </div>
              )
            ) : (
              <Diagnostics
                data={diagnostics}
                offset={usageOffset}
                onPage={loadDiagnostics}
                busy={busy === "diagnostics"}
                spinning={busy === "diagnostics"}
              />
            )}
            </div>
          </>
        )}
        {auth === true ? null : error ? (
          <p className="management-message error" role="alert">
            {error}
          </p>
        ) : null}
        {auth === true ? null : notice ? (
          <p className="management-message" role="status">
            {notice}
          </p>
        ) : null}
      </main>
      <SiteFooter />
    </>
  )
}
interface Metric {
  value: number | null
  status: string
}
function metric(m?: Metric) {
  if (!m || m.status === "unknown") return "未知"
  if (m.status !== "reported") return m.status === "null" ? "空值" : "异常"
  return (m.value ?? 0).toLocaleString("zh-Hans")
}
function metricKnown(m?: Metric) {
  return m?.status === "reported"
}
function duration(ms: number | null | undefined) {
  if (ms === null || ms === undefined) return "未知"
  if (ms < 1000) return `${ms}ms`
  const seconds = ms / 1000
  return `${seconds >= 10 ? seconds.toFixed(0) : seconds.toFixed(1)}s`
}
function stamp(iso: string) {
  const date = new Date(iso)
  if (Number.isNaN(date.getTime())) return iso
  return date.toLocaleString("sv-SE", { timeZone: "Asia/Shanghai", hour12: false })
}
function pageWindow(current: number, total: number) {
  const wanted = [1, total, current - 1, current, current + 1].filter((n, i, all) => n >= 1 && n <= total && all.indexOf(n) === i).sort((a, b) => a - b)
  const items: (number | "gap")[] = []
  let previous = 0
  for (const n of wanted) {
    if (previous && n - previous > 1) items.push("gap")
    items.push(n)
    previous = n
  }
  return items
}
function RisingFigure({ value, label }: { value?: number; label: string }) {
  const shown = useRise(value, value !== undefined)
  return (
    <div>
      <span>{label}</span>
      <strong>{shown === undefined ? "—" : shown.toLocaleString("zh-Hans")}</strong>
    </div>
  )
}
function Diagnostics({
  data,
  offset,
  onPage,
  busy,
  spinning,
}: {
  data: Record<string, unknown> | null
  offset: number
  onPage: (offset: number) => void
  busy: boolean
  spinning: boolean
}) {
  const summary = data?.summary as Record<string, number> | undefined
  const recent = (data?.recent || []) as {
    submission?: number
    created_at: string
    display_name: string
    provider_name?: string | null
    reporting: {
      input: Metric
      output: Metric
      cache_read: Metric
      cache_write: Metric
      stream_status: string
      first_response_ms: number | null
      first_meaningful_delta_ms?: number | null
      response_finished_at?: string | null
      total_ms: number
      hints_sent: boolean
      cache_policy: string
    }
  }[]
  const serverPaged = typeof data?.offset === "number"
  const total = serverPaged ? (summary?.physical_submissions ?? recent.length) : recent.length
  const rows = serverPaged ? recent : recent.slice(offset, offset + usagePageSize)
  const pages = Math.max(1, Math.ceil(total / usagePageSize))
  const page = Math.min(pages, Math.floor(offset / usagePageSize) + 1)
  const figures = [
    ["physical_submissions", "物理提交"],
    ["reported", "缓存字段已报告"],
    ["unknown", "未报告或异常"],
    ["hits", "观察到命中"],
    ["reported_zero", "明确报告为零"],
    ["incomplete", "未完整结束"],
  ] as const
  return (
    <section className="diagnostics">
      <header>
        <div>
          <h2>缓存与实际用量</h2>
          <p className="fine">未知样本不进入命中比例的分母；字段报告、实际命中与账单折扣分别展示。时间为提交开始；响应用时不含工具执行和文章校验，供应商后台可能按完成时刻记账。</p>
        </div>
        <button type="button" className="btn quiet desk-pill" disabled={busy} onClick={() => onPage(offset)}>
          <span className={spinning ? "desk-spin" : "desk-reload"} aria-hidden="true">
            ↻
          </span>
          重新读取
        </button>
      </header>
      {!data ? (
        <p className={busy ? "desk-wait" : "empty-state"} role="status">
          {busy ? <i /> : null}
          {busy ? "正在读取用量…" : "还没有读到用量。"}
        </p>
      ) : (
        <>
          <div className="diagnostic-metrics">
            {figures.map(([id, label]) => (
              <RisingFigure key={id} label={label} value={summary?.[id]} />
            ))}
          </div>
          <p className="doc-note">
            账单折扣：未知。历史仅规范化记录：
            {String(data.legacy_normalized_only ?? "—")} 条，不能按其中的 0 推断真实未命中。
          </p>
          <div className="usage-sheet">
            {rows.length ? (
              <table className="usage-table">
                <caption className="sr-only">模型调用记录</caption>
                <thead>
                  <tr>
                    <th>提交开始（北京时间）</th>
                    <th>模型</th>
                    <th className="num">响应用时 / 首个内容</th>
                    <th className="num">输入</th>
                    <th className="num">输出</th>
                    <th className="num">缓存读 / 写</th>
                    <th>结束</th>
                  </tr>
                </thead>
                <tbody>
                  {rows.map((row, i) => {
                    const report = row.reporting
                    const complete = report?.stream_status === "complete"
                    return (
                      <tr key={`${row.created_at}-${row.submission ?? i}`}>
                        <td>
                          <span className="usage-span"><time dateTime={row.created_at}>{stamp(row.created_at)}</time>{report?.response_finished_at ? <small>完成 {stamp(report.response_finished_at)}</small> : null}</span>
                        </td>
                        <td className="usage-model" title={[row.display_name, row.provider_name].filter(Boolean).join(" · ")}>
                          <span className="usage-span">
                            <span>{row.display_name}</span>
                            <small>{row.provider_name || "历史线路未知"}</small>
                          </span>
                        </td>
                        <td className="num">
                          <span className="usage-span">
                            <span>{duration(report?.total_ms)}</span>
                            <small title="首个非空正文或工具调用增量；历史首字节记录不混作首个内容">{duration(report?.first_meaningful_delta_ms)}</small>
                          </span>
                        </td>
                        <td className={metricKnown(report?.input) ? "num" : "num muted"}>{metric(report?.input)}</td>
                        <td className={metricKnown(report?.output) ? "num" : "num muted"}>{metric(report?.output)}</td>
                        <td className="num">
                          <span className={metricKnown(report?.cache_read) ? undefined : "muted"}>{metric(report?.cache_read)}</span>
                          <span className="usage-split"> / </span>
                          <span className={metricKnown(report?.cache_write) ? undefined : "muted"}>{metric(report?.cache_write)}</span>
                        </td>
                        <td>
                          <span className={`usage-end ${complete ? "on" : ""}`}>{complete ? "完整" : "未完整"}</span>
                          {report?.hints_sent ? <span className="usage-hint">显式</span> : null}
                        </td>
                      </tr>
                    )
                  })}
                </tbody>
              </table>
            ) : (
              <p className="empty-state">
                {offset > 0 ? "这一页没有记录。" : "还没有本版本的物理提交用量。已有历史记录保持原事实。"}
              </p>
            )}
            {total > 0 ? (
              <div className="usage-pager">
                <p>
                  共 {total.toLocaleString("zh-Hans")} 条
                  {pages > 1 ? ` · 每页 ${usagePageSize}` : ""}
                </p>
                {pages > 1 ? (
                <nav aria-label="用量分页">
                  <button type="button" className="usage-page" disabled={busy || page <= 1} onClick={() => onPage(offset - usagePageSize)}>
                    上一页
                  </button>
                  {pageWindow(page, pages).map((item, i) =>
                    item === "gap" ? (
                      <span key={`gap-${i}`} className="usage-gap">
                        …
                      </span>
                    ) : (
                      <button
                        key={item}
                        type="button"
                        className="usage-page"
                        aria-current={item === page ? "page" : undefined}
                        disabled={busy}
                        onClick={() => onPage((item - 1) * usagePageSize)}
                      >
                        {item}
                      </button>
                    ),
                  )}
                  <button type="button" className="usage-page" disabled={busy || page >= pages} onClick={() => onPage(offset + usagePageSize)}>
                    下一页
                  </button>
                </nav>
                ) : null}
              </div>
            ) : null}
          </div>
        </>
      )}
    </section>
  )
}
