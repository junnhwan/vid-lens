import { useEffect, useRef, useState } from "react"
import { HttpApi } from "../api/http"
import { ApiError } from "../api/error"
import { canReplay, newIdempotencyKey, pendingWrite, rememberWrite, forgetWrite, memoryStore } from "../api/writes"
import { SiteHeader, SiteFooter } from "../ui/SiteHeader"
import { ModelIcon } from "../ui/ModelIdentity"
import { ChoiceMenu, type ChoiceOption } from "../ui/ChoiceMenu"
import { useRise } from "../ui/countUp"

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
  disable_thinking: boolean
  cache_policy: string
}
interface Probe {
  id: string
  status: string
  mode: string
  submissions: number
  created_at: string
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
  ["models", "模型目录"],
  ["providers", "供应商"],
  ["diagnostics", "缓存与用量"],
] as const
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
    [filter, setFilter] = useState("")
  const [probes, setProbes] = useState<Probe[]>([]),
    [probeModel, setProbeModel] = useState(""),
    [diagnostics, setDiagnostics] = useState<Record<string, unknown> | null>(null)
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
  }
  useEffect(() => {
    if (auth) void load().catch((e) => setError(message(e)))
  }, [auth])
  useEffect(() => {
    if (!auth || !probeModel) return
    let stop = false,
      timer: ReturnType<typeof setTimeout> | undefined
    async function poll() {
      if (document.hidden) {
        timer = setTimeout(poll, 2000)
        return
      }
      try {
        const r = await api.current.request<{ items: Probe[] }>("GET", `/api/v1/admin/models/${probeModel}/probes`)
        if (stop) return
        setProbes(r.body.items)
        if (r.body.items.some((p) => ["queued", "running"].includes(p.status))) timer = setTimeout(poll, 2000)
        else void load().catch(() => {})
      } catch (e) {
        if (!stop) setError(message(e))
      }
    }
    void poll()
    return () => {
      stop = true
      if (timer) clearTimeout(timer)
    }
  }, [auth, probeModel, busy])
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
      setError("当前表单有未保存内容，请先保存或撤销。")
      return
    }
    setMd(m ? modelDraft(m) : { ...emptyModel, provider_id: providers[0]?.id || "" })
    setProbeModel(m?.id || "")
    setProbes([])
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
      const r = await api.current.request<Model>(id ? "PUT" : "POST", id ? `/api/v1/admin/models/${id}` : "/api/v1/admin/models", body)
      setMd(modelDraft(r.body))
      setDirty(false)
      setProbeModel(r.body.id)
      setNotice("模型已保存。新修订需先通过能力检查，再启用。")
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
      await load()
      setNotice(m.enabled ? "模型已停用；旧文章仍可阅读，后续新调用将被阻止。" : "模型已启用。")
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
      setProbeModel(m.id)
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
  const filtered = models.filter((m) =>
    [m.display_name, m.upstream_id, providers.find((p) => p.id === m.provider_id)?.name].join(" ").toLowerCase().includes(filter.toLowerCase()),
  )
  const sectionIndex = Math.max(
    0,
    sections.findIndex(([id]) => id === section),
  )
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
            <h1>{auth ? "模型与供应商" : "站长登录"}</h1>
            <p>{auth ? "只让确认可用的模型，出现在访客面前。" : "管理供应商、模型与运行诊断。"}</p>
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
              <nav className="manage-tabs" data-tab={sectionIndex} aria-label="管理分区">
                <i className="tab-indicator" aria-hidden="true" />
                {sections.map(([id, label]) => (
                  <button
                    key={id}
                    type="button"
                    aria-current={section === id ? "page" : undefined}
                    onClick={() => {
                      if (dirty) {
                        setError("请先保存或撤销当前表单。")
                        return
                      }
                      setSection(id)
                      if (id === "diagnostics")
                        void action("diagnostics", async () => {
                          const r = await api.current.request<Record<string, unknown>>("GET", "/api/v1/admin/diagnostics")
                          setDiagnostics(r.body)
                        })
                    }}
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
            <div className="desk-stage" key={section}>
            {section === "providers" ? (
              <div className="manage-columns">
                <section className="provider-rail">
                  <div className="manage-list-head">
                    <h2>供应商</h2>
                    <button type="button" className="btn quiet desk-pill" onClick={() => editProvider()}>
                      新增
                    </button>
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
              <>
                <div className="model-toolbar">
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
                  <button type="button" className="btn primary" onClick={() => editModel()}>
                    添加模型
                  </button>
                </div>
                <div className="model-ledger">
                  <div className="ledger-head">
                    <span>目录</span>
                    <span>{models.length ? `${filtered.length} / ${models.length}` : "空"}</span>
                  </div>
                  {!models.length ? (
                    <div className="empty-state">
                      <p>还没有目录模型。先添加供应商，再手动添加或发现模型。现有单模型入口仍按原配置工作。</p>
                      <button
                        type="button"
                        className="btn quiet desk-pill"
                        disabled={!!busy}
                        onClick={() =>
                          void action("import-current", async () => {
                            await api.current.request("POST", "/api/v1/admin/models/import-current", {})
                            await load()
                            setNotice("现有模型已导入为停用候选；先检查再启用，未改变现有入口。")
                          })
                        }
                      >
                        导入现有配置为候选
                      </button>
                    </div>
                  ) : filtered.length ? (
                    <ul className="ledger-list">
                      {filtered.map((m) => (
                        <li key={m.id} data-current={md.id === m.id ? "true" : "false"}>
                          <button
                            type="button"
                            className="ledger-open"
                            aria-current={md.id === m.id ? "true" : undefined}
                            onClick={() => editModel(m)}
                          >
                            <span className="ledger-mark">
                              <ModelIcon brand={m.brand_id} name={m.display_name} />
                            </span>
                            <span className="ledger-copy">
                              <strong>{m.display_name}</strong>
                              <small>
                                {m.upstream_id} · {providers.find((p) => p.id === m.provider_id)?.name || "未关联供应商"}
                              </small>
                              <em className={m.verified ? "on" : undefined}>
                                {m.verified ? "图片与工具检查通过" : "尚未通过当前修订检查"}
                              </em>
                            </span>
                          </button>
                          <div className="ledger-ops">
                            <div className="ledger-flags">
                              <span className={`desk-seal ${m.enabled ? "on" : ""}`}>
                                <i className={`status-dot ${m.enabled ? "on" : ""}`} />
                                {m.enabled ? "启用" : "停用"}
                              </span>
                              {m.is_default ? <span className="desk-seal mark">默认</span> : null}
                            </div>
                            <div className="ledger-actions">
                              <button type="button" className="text-action" disabled={!!busy} onClick={() => void toggleModel(m)}>
                                {m.enabled ? "停用" : "启用"}
                              </button>
                              <button
                                type="button"
                                className="text-action"
                                disabled={!!busy || !m.enabled}
                                onClick={() =>
                                  void action("default", async () => {
                                    await api.current.request("PUT", "/api/v1/admin/model-settings", {
                                      default_model_id: m.id,
                                      expected_revision: revision,
                                    })
                                    await load()
                                    setNotice("默认模型已更新，只影响新请求。")
                                  })
                                }
                              >
                                设为默认
                              </button>
                              <button type="button" className="text-action" disabled={!!busy} onClick={() => void probe(m, "capabilities")}>
                                检查
                              </button>
                            </div>
                          </div>
                        </li>
                      ))}
                    </ul>
                  ) : (
                    <p className="empty-state">没有符合「{filter}」的模型。</p>
                  )}
                </div>
                {!models.some((m) => m.enabled && m.provider_enabled) && models.length ? (
                  <p className="doc-note">当前目录没有启用模型。若已切换到目录选模，新解读将不可提交；旧文章仍可阅读与导出。</p>
                ) : null}
                <section className="manage-form model-detail">
                  <div>
                    <h2>
                      {md.id ? <ModelIcon brand={md.brand_id} name={md.display_name} decorative /> : null}
                      {md.id ? "模型详情" : "添加模型"}
                      {dirty ? <span className="desk-dirty">未保存</span> : null}
                    </h2>
                    <p className="fine">新修订先保存为停用。一次小样本通过不代表长期健康或长视频质量保证。</p>
                  </div>
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
                        onChange={(id) => {
                          setMd({ ...md, provider_id: id })
                          setDirty(true)
                        }}
                      />
                      <label className="desk-field">
                        <span>显示名</span>
                        <input
                          value={md.display_name}
                          onChange={(e) => {
                            setMd({ ...md, display_name: e.target.value })
                            setDirty(true)
                          }}
                          required
                        />
                      </label>
                      <label className="desk-field">
                        <span>上游 model ID</span>
                        <input
                          value={md.upstream_id}
                          spellCheck={false}
                          onChange={(e) => {
                            setMd({ ...md, upstream_id: e.target.value })
                            setDirty(true)
                          }}
                          required
                        />
                      </label>
                      <DeskSelect
                        label="品牌标识"
                        value={md.brand_id}
                        options={brandOptions}
                        placeholder="选择品牌"
                        onChange={(id) => {
                          setMd({ ...md, brand_id: id })
                          setDirty(true)
                        }}
                      />
                      <label className="desk-field">
                        <span>输出 tokens 上限</span>
                        <input
                          type="number"
                          min={1024}
                          max={32768}
                          value={md.max_output_tokens}
                          onChange={(e) => {
                            setMd({
                              ...md,
                              max_output_tokens: Number(e.target.value),
                            })
                            setDirty(true)
                          }}
                        />
                      </label>
                      <label className="desk-field">
                        <span>输入字符保护上限</span>
                        <input
                          type="number"
                          min={10000}
                          max={1000000}
                          value={md.max_input_chars}
                          onChange={(e) => {
                            setMd({
                              ...md,
                              max_input_chars: Number(e.target.value),
                            })
                            setDirty(true)
                          }}
                        />
                      </label>
                      <DeskSelect
                        label="缓存策略"
                        value={md.cache_policy}
                        options={cacheOptions}
                        placeholder="选择缓存策略"
                        onChange={(id) => {
                          setMd({ ...md, cache_policy: id })
                          setDirty(true)
                        }}
                      />
                    </div>
                    <label className="desk-field">
                      <span>给访客的一句说明</span>
                      <input
                        maxLength={240}
                        value={md.description}
                        onChange={(e) => {
                          setMd({ ...md, description: e.target.value })
                          setDirty(true)
                        }}
                      />
                    </label>
                    <div className="desk-switches">
                      <InkSwitch
                        checked={md.disable_thinking}
                        label="发送关闭思考的兼容参数"
                        onChange={(checked) => {
                          setMd({ ...md, disable_thinking: checked })
                          setDirty(true)
                        }}
                      />
                      <InkSwitch
                        checked={md.enabled}
                        label="启用此修订（能力检查通过后）"
                        onChange={(checked) => {
                          setMd({ ...md, enabled: checked })
                          setDirty(true)
                        }}
                      />
                    </div>
                    <div className="form-actions">
                      <button className="btn primary" disabled={!!busy}>
                        {busy === "model" ? "正在保存" : "保存模型"}
                      </button>
                      <button
                        type="button"
                        className="btn quiet desk-pill"
                        onClick={() => {
                          setDirty(false)
                          setMd(selected ? modelDraft(selected) : emptyModel)
                        }}
                      >
                        撤销修改
                      </button>
                      {selected ? (
                        <>
                          <button
                            type="button"
                            className="btn quiet desk-pill"
                            disabled={!!busy || dirty}
                            onClick={() => void probe(selected, "capabilities")}
                          >
                            检查图片与工具
                          </button>
                          <button
                            type="button"
                            className="btn quiet desk-pill"
                            disabled={!!busy || dirty}
                            onClick={() => void probe(selected, "cache")}
                          >
                            串行检查缓存
                          </button>
                        </>
                      ) : null}
                    </div>
                    <p className="fine">
                      检查会调用真实模型。每任务最多 3 次提交、180 秒；缓存检查单独串行
                      warm/read，不批量探测。显式缓存需当前端点报告实际创建或命中后才启用提示。
                    </p>
                  </form>
                </section>
                {probeModel ? (
                  <section className="probe-history">
                    <h2>最近检查</h2>
                    {probes.length ? (
                      <ol className="probe-rail">
                        {probes.map((p) => (
                          <li key={p.id} data-status={p.status}>
                            <i aria-hidden="true" />
                            <div>
                              <strong>{labels[p.status] || p.status}</strong>
                              <span>
                                {p.mode === "cache" ? "缓存样本" : "图片与工具"} · {p.submissions} 次物理提交
                              </span>
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
                        ))}
                      </ol>
                    ) : (
                      <p className="fine">尚无检查记录。</p>
                    )}
                  </section>
                ) : null}
              </>
            ) : (
              <Diagnostics
                data={diagnostics}
                onReload={() =>
                  void action("diagnostics", async () => {
                    const r = await api.current.request<Record<string, unknown>>("GET", "/api/v1/admin/diagnostics")
                    setDiagnostics(r.body)
                  })
                }
                busy={busy === "diagnostics"}
                spinning={busy === "diagnostics"}
              />
            )}
            </div>
          </>
        )}
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
  if (!m || m.status === "unknown") return "未报告"
  if (m.status !== "reported") return m.status === "null" ? "空值" : "字段异常"
  return (m.value ?? 0).toLocaleString("zh-Hans")
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
  onReload,
  busy,
  spinning,
}: {
  data: Record<string, unknown> | null
  onReload: () => void
  busy: boolean
  spinning: boolean
}) {
  const summary = data?.summary as Record<string, number> | undefined
  const recent = (data?.recent || []) as {
    created_at: string
    display_name: string
    reporting: {
      input: Metric
      output: Metric
      cache_read: Metric
      cache_write: Metric
      stream_status: string
      first_response_ms: number | null
      total_ms: number
      hints_sent: boolean
      cache_policy: string
    }
  }[]
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
          <p className="fine">未知样本不进入命中比例的分母；字段报告、实际命中与账单折扣分别展示。</p>
        </div>
        <button type="button" className="btn quiet desk-pill" disabled={busy} onClick={onReload}>
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
          {recent.length ? (
            <ul className="usage-list">
              {recent.map((r, i) => (
                <li key={i}>
                  <header>
                    <strong>{r.display_name}</strong>
                    <time dateTime={r.created_at}>{new Date(r.created_at).toLocaleString("zh-Hans")}</time>
                  </header>
                  <dl>
                    <div>
                      <dt>输入 / 输出</dt>
                      <dd>
                        {metric(r.reporting.input)} / {metric(r.reporting.output)}
                      </dd>
                    </div>
                    <div>
                      <dt>缓存读取 / 创建</dt>
                      <dd>
                        {metric(r.reporting.cache_read)} / {metric(r.reporting.cache_write)}
                      </dd>
                    </div>
                    <div>
                      <dt>首响应 / 总耗时</dt>
                      <dd>
                        {r.reporting.first_response_ms === null ? "未知" : `${r.reporting.first_response_ms}ms`} / {r.reporting.total_ms}ms
                      </dd>
                    </div>
                    <div>
                      <dt>实际提示 / 结束</dt>
                      <dd>
                        {r.reporting.hints_sent ? "已发送显式提示" : "未发显式提示"}
                        <small>{r.reporting.stream_status === "complete" ? "完整" : "未完整"}</small>
                      </dd>
                    </div>
                  </dl>
                </li>
              ))}
            </ul>
          ) : (
            <p className="empty-state">还没有本版本的物理提交用量。已有历史记录保持原事实。</p>
          )}
        </>
      )}
    </section>
  )
}
