import { useEffect, useRef, useState } from "react"
import { ApiError } from "../api/error"
import { canReplay, forgetWrite, memoryStore, newIdempotencyKey, pendingWrite, rememberWrite } from "../api/writes"
import { useRise } from "../ui/countUp"
import { SiteFooter, SiteHeader } from "../ui/SiteHeader"
import { AnnouncementSettings } from "./AnnouncementSettings"
import { ExampleShowcase } from "./ExampleShowcase"
import { ExecutionSettings } from "./ExecutionSettings"
import { checkedFirst, emptyModel, emptyProvider, message, Model, ModelDraft, modelDraft, Probe, Provider, ProviderDraft, providerDraft, sameRevision, sections, usagePageSize } from "./manageCatalog"
import { Diagnostics } from "./ManageDiagnostics"
import { ManageModels } from "./ManageModels"
import { ManageProviders } from "./ManageProviders"
import { NetworkSettings } from "./NetworkSettings"
import { openProbe } from "./probeStatus"
import { useOwnerSession } from "./useOwnerSession"
import { useProbeMonitor } from "./useProbeMonitor"
import { managePath, pageLocation, readManagePath, readManageRoute } from "./manageRoute"
import { VideoStorage } from "./VideoStorage"

export function Manage() {
  const { api, auth, setAuth, password, setPassword, reveal, setReveal, login } = useOwnerSession(action)
  const [route, setRoute] = useState(() => readManageRoute(pageLocation().pathname, pageLocation().search)),
    [busy, setBusy] = useState(""),
    [error, setError] = useState(""),
    [notice, setNotice] = useState("")
  const [providers, setProviders] = useState<Provider[]>([]),
    [models, setModels] = useState<Model[]>([]),
    [revision, setRevision] = useState(0)
  const [pd, setPd] = useState<ProviderDraft>(emptyProvider),
    [md, setMd] = useState<ModelDraft>(emptyModel),
    [dirty, setDirty] = useState(false)
  function markDirty(next: boolean) {
    setDirty(next)
    if (!next) setError((current) => (current.includes("请先保存或撤销") ? "" : current))
  }
  const [discovery, setDiscovery] = useState<string[]>([]),
    [picked, setPicked] = useState<string[]>([]),
    [filter, setFilter] = useState(""),
    [composing, setComposing] = useState(false),
    [advanced, setAdvanced] = useState(false),
    [ready, setReady] = useState(false)
  const [diagnostics, setDiagnostics] = useState<Record<string, unknown> | null>(null)
  const section = route.section
  const { probeByModel, acceptProbes, now } = useProbeMonitor({ api, auth, busy, selectedId: composing ? undefined : md.id, refresh: load, setError, onAuthLost: () => setAuth(false) })
  const [routeOrderDraft, setRouteOrderDraft] = useState<{ family: string; ids: string[] } | null>(null)
  const usageToken = useRef(0)
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
  function visit(path: string, replace = false) {
    const here = pageLocation()
    if (`${here.pathname}${here.search}` !== path) {
      const history = globalThis.history
      if (replace) history?.replaceState(null, "", path)
      else history?.pushState(null, "", path)
    }
    setRoute(readManagePath(path))
  }
  useEffect(() => {
    const sync = () => {
      const here = pageLocation()
      const next = readManageRoute(here.pathname, here.search)
      if (dirty && (next.section !== route.section || next.offset !== route.offset || next.query !== route.query)) {
        globalThis.history?.pushState(null, "", managePath(route.section, route))
        setError("请先保存或撤销当前表单。")
        return
      }
      setRoute(next)
    }
    addEventListener("popstate", sync)
    return () => removeEventListener("popstate", sync)
  }, [dirty, route])
  useEffect(() => {
    const here = pageLocation()
    if (!here.pathname.startsWith("/manage")) return
    const current = readManageRoute(here.pathname, here.search)
    const canonical = managePath(current.section, current)
    if (`${here.pathname}${here.search}` !== canonical) globalThis.history?.replaceState(null, "", canonical)
  }, [])
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
    setRouteOrderDraft(null)

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
    visit(managePath(id))
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
  function loadDiagnostics(offset = route.offset) {
    const token = ++usageToken.current
    setBusy((current) => (current && current !== "diagnostics" ? current : "diagnostics"))
    setError("")
    setNotice("")
    void api.current
      .request<Record<string, unknown>>("GET", `/api/v1/admin/diagnostics?limit=${usagePageSize}&offset=${offset}`)
      .then((r) => {
        if (usageToken.current === token) setDiagnostics(r.body)
      })
      .catch((e) => {
        if (usageToken.current !== token) return
        if (e instanceof ApiError && e.status === 401) setAuth(false)
        setError(message(e))
      })
      .finally(() => {
        if (usageToken.current === token) setBusy((current) => (current === "diagnostics" ? "" : current))
      })
  }
  function showUsage(offset: number) {
    const path = managePath("diagnostics", { offset })
    const here = pageLocation()
    if (`${here.pathname}${here.search}` !== path) {
      visit(path)
      return
    }
    loadDiagnostics(offset)
  }
  useEffect(() => {
    if (auth && section === "diagnostics") loadDiagnostics(route.offset)
  }, [auth, section, route.offset])
  async function saveProvider() {
    await action("provider", async () => {
      const { id, ...body } = pd
      const r = await api.current.request<Provider>(
        id ? "PUT" : "POST",
        id ? `/api/v1/admin/providers/${id}` : "/api/v1/admin/providers",
        body,
      )
      setPd(providerDraft(r.body))
      markDirty(false)
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
      markDirty(false)
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
  async function setRecommended(m: Model, recommended: boolean) {
    await action(m.id, async () => {
      await api.current.request("PUT", `/api/v1/admin/models/${m.id}/recommendation`, {
        recommended,
        expected_revision: m.revision,
      })
      await load()
      setNotice(recommended ? "首页会标出这个模型。" : "已取消首页推荐。")
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
      acceptProbes(m.id, listed.body.items)
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
  const selectedRouteGroup = selected ? models.filter((model) => model.route_family === selected.route_family) : []
  const baseRouteGroup = selectedRouteGroup.slice().sort((a, b) => {
    const priorityA = a.route_priority || Number.MAX_SAFE_INTEGER
    const priorityB = b.route_priority || Number.MAX_SAFE_INTEGER
    return priorityA - priorityB || providerName(a.provider_id).localeCompare(providerName(b.provider_id), "zh-CN") || a.id.localeCompare(b.id)
  })
  const baseRouteIDs = baseRouteGroup.map((model) => model.id)
  const currentRouteDraft = routeOrderDraft
  const routeOrderIDs =
    currentRouteDraft && currentRouteDraft.family === selected?.route_family
      ? [
        ...currentRouteDraft.ids.filter((id) => baseRouteIDs.includes(id)),
        ...baseRouteIDs.filter((id) => !currentRouteDraft.ids.includes(id)),
      ]
      : baseRouteIDs
  const routeOrderChanged = routeOrderIDs.some((id, index) => id !== baseRouteIDs[index])
  const routeModelsByID = new Map(selectedRouteGroup.map((model) => [model.id, model]))
  const orderedRouteModels = routeOrderIDs.map((id) => routeModelsByID.get(id)).filter((model): model is Model => Boolean(model))
  function modelMeta(model: Pick<Model, "display_name" | "upstream_id" | "provider_id" | "route_priority">) {
    const name = providerName(model.provider_id)
    const identity = model.upstream_id === model.display_name ? name : `${model.upstream_id} · ${name}`
    return `${identity} · ${model.route_priority > 0 ? `顺序 ${model.route_priority}` : "自动顺序"}`
  }
  function patchModel(partial: Partial<ModelDraft>) {
    setMd((draft) => ({ ...draft, ...partial }))
    setDirty(true)
  }
  function revertModel() {
    markDirty(false)
    setRouteOrderDraft(null)
    setMd(selected ? modelDraft(selected) : { ...emptyModel, provider_id: providers[0]?.id || "" })
  }
  function moveRoute(id: string, toIndex: number) {
    if (!selected || dirty || busy) return
    const fromIndex = routeOrderIDs.indexOf(id)
    if (fromIndex < 0 || fromIndex === toIndex || toIndex < 0 || toIndex >= routeOrderIDs.length) return
    const ids = routeOrderIDs.slice()
    ids.splice(fromIndex, 1)
    ids.splice(toIndex, 0, id)
    setRouteOrderDraft({ family: selected.route_family, ids })
  }
  async function saveRouteOrder() {
    if (!selected || !routeOrderChanged || orderedRouteModels.length < 2 || dirty) return
    await action("route-order", async () => {
      try {
        const result = await api.current.request<{ items: Model[] }>("POST", "/api/v1/admin/model-routes/order", {
          items: orderedRouteModels.map((model) => ({ id: model.id, expected_revision: model.revision })),
        })
        const refreshed = new Map(result.body.items.map((model) => [model.id, model]))
        setModels((current) => current.map((model) => refreshed.get(model.id) || model))
        const freshSelected = refreshed.get(selected.id)
        if (freshSelected) setMd(modelDraft(freshSelected))
        setRouteOrderDraft(null)

        setNotice("同型号线路顺序已保存。提交次数较少的线路会先尝试；次数相同时按这里的顺序选择。")
      } catch (error) {
        if (error instanceof ApiError && error.status === 409) {
          const refreshed = await load()
          const freshSelected = refreshed.find((model) => model.id === selected.id)
          if (freshSelected) setMd(modelDraft(freshSelected))
          setRouteOrderDraft(null)

        }
        throw error
      }
    })
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
            <p>{auth ? "管理模型与联网，挑选公开示例，查看视频存储和实际用量。" : "管理视频、公开示例、供应商、模型、联网搜索与运行诊断。"}</p>
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
              <nav className="manage-tabs" data-tab={tab} aria-label="管理分区" style={{ gridTemplateColumns: `repeat(${sections.length}, minmax(0, 1fr))` }}>
                <i className="tab-indicator" aria-hidden="true" style={{ width: `calc((100% - 8px) / ${sections.length})`, transform: `translateX(${Number(tab) * 100}%)` }} />
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
              {section === "announcement" ? (
                <AnnouncementSettings api={api.current} onDirty={markDirty} onAuthLost={() => setAuth(false)} />
              ) : section === "execution" ? (
                <ExecutionSettings api={api.current} onDirty={markDirty} onAuthLost={() => setAuth(false)} />
              ) : section === "videos" ? (
                <VideoStorage api={api.current} onDirty={markDirty} onAuthLost={() => setAuth(false)} query={route.query} offset={route.offset} onRoute={(next) => {
                  const path = managePath("videos", next)
                  visit(path)
                }} />
              ) : section === "examples" ? (
                <ExampleShowcase api={api.current} onDirty={markDirty} onAuthLost={() => setAuth(false)} />
              ) : section === "network" ? (
                <NetworkSettings api={api.current} onDirty={markDirty} onAuthLost={() => setAuth(false)} />
              ) : section === "providers" ? <ManageProviders {...{ openSection, editProvider, providers, pd, dirty, saveProvider, setPd, setDirty: markDirty, busy, discover, discovery, setPicked, picked, importModels }} /> : section === "models" ? <ManageModels {...{ ready, error, models, composing, providers, openProviders, beginAddProvider, busy, importCurrent, editModel, filter, filtered, setFilter, probeByModel, now, md, modelMeta, selected, cancelCompose, dirty, selectedProbes, checking, probe, capabilityProbe, toggleModel, makeDefault, setRecommended, setError, setNotice, saveModel, patchModel, advanced, setAdvanced, orderedRouteModels, providerName, moveRoute, routeOrderChanged, saveRouteOrder, cacheChecking, cacheProbe, revertModel }} /> : (
                <Diagnostics
                  data={diagnostics}
                  offset={route.offset}
                  onPage={showUsage}
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
