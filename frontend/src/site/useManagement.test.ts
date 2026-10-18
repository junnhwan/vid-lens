import { createElement } from "react"
import { act, create, type ReactTestRenderer } from "react-test-renderer"
import { afterEach, beforeEach, expect, it, vi } from "vitest"
import { ApiError } from "../api/error"
import { HttpApi } from "../api/http"
import { memoryStore } from "../api/writes"
import { Manage } from "./Manage"
import { providerDraft } from "./manageCatalog"
import { useOwnerSession } from "./useOwnerSession"
import { useProbeMonitor } from "./useProbeMonitor"

const mock = vi.hoisted(() => ({ request: vi.fn() }))
vi.mock("../ui/SiteHeader", () => ({ SiteHeader: () => null, SiteFooter: () => null }))
vi.mock("../ui/countUp", () => ({ useRise: (value: unknown) => value }))
vi.mock("../api/http", () => ({ HttpApi: class { request = mock.request; csrfToken = "" } }))

function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>(done => { resolve = done })
  return { promise, resolve }
}

let renderer: ReactTestRenderer | undefined
let owner: ReturnType<typeof useOwnerSession>
function Owner() {
  owner = useOwnerSession(async (_name, action) => { await action() })
  return null
}

beforeEach(() => {
  vi.resetAllMocks()
  vi.stubGlobal("sessionStorage", memoryStore())
  vi.stubGlobal("addEventListener", vi.fn())
  vi.stubGlobal("removeEventListener", vi.fn())
  vi.stubGlobal("document", { hidden: false, head: { appendChild() {} }, createElement: () => ({ remove() {} }) })
})
afterEach(async () => {
  await act(async () => renderer?.unmount())
  renderer = undefined
  vi.useRealTimers()
  vi.unstubAllGlobals()
})

it("keeps a newly authenticated session when the older bootstrap finishes", async () => {
  const bootstrap = deferred<{ body: { csrf_token: string } }>()
  mock.request.mockReturnValueOnce(bootstrap.promise).mockResolvedValue({ body: { csrf_token: "current" } })
  await act(async () => { renderer = create(createElement(Owner)) })
  await act(async () => owner.setPassword("entered-password"))
  await act(async () => owner.login())
  await act(async () => bootstrap.resolve({ body: { csrf_token: "obsolete" } }))
  expect(owner.auth).toBe(true)
  expect(owner.api.current.csrfToken).toBe("current")
  expect(owner.password).toBe("")
})

it("does not restore authentication after the owner session is lost", async () => {
  const bootstrap = deferred<{ body: { csrf_token: string } }>()
  mock.request.mockReturnValue(bootstrap.promise)
  await act(async () => { renderer = create(createElement(Owner)) })
  await act(async () => owner.setAuth(false))
  await act(async () => bootstrap.resolve({ body: { csrf_token: "obsolete" } }))
  expect(owner.auth).toBe(false)
  expect(owner.api.current.csrfToken).toBe("")
})

it("does not install a token from a login completed after unmount", async () => {
  const login = deferred<{ body: { csrf_token: string } }>()
  mock.request.mockRejectedValueOnce(new Error("expired")).mockReturnValue(login.promise)
  await act(async () => { renderer = create(createElement(Owner)) })
  await act(async () => { void owner.login() })
  const api = owner.api.current
  await act(async () => renderer?.unmount())
  renderer = undefined
  await act(async () => login.resolve({ body: { csrf_token: "late" } }))
  expect(api.csrfToken).toBe("")
})

it("stops probe polling when authentication is rejected", async () => {
  vi.useFakeTimers()
  const lost = vi.fn(), refresh = vi.fn(), error = vi.fn()
  const api = { current: new HttpApi() }
  mock.request.mockRejectedValue(new ApiError({ status: 401, code: "AUTH_REQUIRED", detail: "expired" }))
  function Monitor() { useProbeMonitor({ api, auth: true, busy: "", selectedId: "model", refresh, setError: error, onAuthLost: lost }); return null }
  await act(async () => { renderer = create(createElement(Monitor)) })
  expect(lost).toHaveBeenCalledOnce()
  expect(refresh).not.toHaveBeenCalled()
  expect(vi.getTimerCount()).toBe(0)
})

it("ignores a probe result completed after its monitor unmounts", async () => {
  vi.useFakeTimers()
  const pending = deferred<{ body: { items: never[] } }>()
  const refresh = vi.fn(), error = vi.fn(), lost = vi.fn()
  const api = { current: new HttpApi() }
  mock.request.mockReturnValue(pending.promise)
  function Monitor() { useProbeMonitor({ api, auth: true, busy: "", selectedId: "model", refresh, setError: error, onAuthLost: lost }); return null }
  await act(async () => { renderer = create(createElement(Monitor)) })
  await act(async () => renderer?.unmount())
  renderer = undefined
  await act(async () => pending.resolve({ body: { items: [] } }))
  expect(refresh).not.toHaveBeenCalled()
  expect(error).not.toHaveBeenCalled()
  expect(vi.getTimerCount()).toBe(0)
})

it("starts provider edits with an empty write-only credential", () => {
  const provider = { id: "provider", revision: 2, revision_id: "rev", name: "Provider", base_url: "https://example.test", enabled: true, key_configured: true, proxy_policy: "server", api_key: "must-not-copy" }
  expect(providerDraft(provider)).toMatchObject({ expected_revision: 2, api_key: "" })
})

it("keeps unsaved provider edits through navigation and revision conflicts", async () => {
  const provider = { id:"fixture",revision:1,revision_id:"rev",name:"Fixture",base_url:"https://fixture.test",enabled:true,key_configured:true,proxy_policy:"server" }
  mock.request.mockImplementation(async (method: string, path: string) => {
    if (path.endsWith("/session")) return {body:{csrf_token:"test"}}
    if (method === "GET" && path.endsWith("/providers")) return {body:{items:[provider]}}
    if (method === "GET" && path.endsWith("/models")) return {body:{items:[],settings:{revision:1}}}
    throw new ApiError({status:409,code:"REVISION_CONFLICT",detail:"configuration changed"})
  })
  await act(async () => {renderer=create(createElement(Manage))})
  const root = renderer!.root
  await act(async () => root.findAllByType("button").find(b => b.children.includes("供应商"))!.props.onClick())
  await act(async () => root.findAllByType("button").find(b => b.findAllByType("strong").some(s => s.children.includes("Fixture")))!.props.onClick())
  await act(async () => root.findByProps({value:"Fixture"}).props.onChange({target:{value:"Edited"}}))
  await act(async () => root.findAllByType("button").find(b => b.children.includes("模型"))!.props.onClick())
  expect(root.findByProps({value:"Edited"})).toBeTruthy()
  expect(root.findAllByProps({role:"alert"}).map(n => n.children.join("")).join("")).toContain("保存或撤销")
  await act(async () => root.findByType("form").props.onSubmit({preventDefault(){}}))
  expect(root.findByProps({value:"Edited"})).toBeTruthy()
  expect(root.findAllByProps({role:"alert"}).map(n => n.children.join("")).join("")).toContain("configuration changed")
  expect(root.findAllByType("input").find(n => n.props.type === "password")!.props.value).toBe("")
})
