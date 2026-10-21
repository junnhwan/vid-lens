import { expect, it } from "vitest"
import { catalogLine, modelState, modelStateCounts, modelStateGroup, type Model } from "./manageCatalog"

const base: Model = {
  id: "m",
  revision: 1,
  revision_id: "r",
  provider_id: "p",
  provider_revision_id: "pr",
  display_name: "Model",
  brand_id: "generic",
  description: "",
  upstream_id: "vendor/model",
  enabled: false,
  provider_enabled: true,
  verified: false,
  is_default: false,
  recommended: false,
  max_output_tokens: 10000,
  max_input_chars: 180000,
  input_chars_limit: 140000,
  context_tokens: 0,
  disable_thinking: false,
  cache_policy: "provider_default",
  route_priority: 0,
  route_family: "family",
}

it("maps each catalog state to one clear label", () => {
  expect(modelState(base, undefined, 0)).toMatchObject({ text: "待检查", tone: "wait" })
  expect(modelState({ ...base, verified: true }, undefined, 0)).toMatchObject({ text: "已检查 · 未启用", tone: "held" })
  expect(modelState({ ...base, enabled: true }, undefined, 0)).toMatchObject({ text: "已启用 · 待复核", tone: "wait" })
  expect(modelState({ ...base, enabled: true, verified: true }, undefined, 0)).toMatchObject({ text: "可选用", tone: "ready" })
  expect(modelState({ ...base, provider_enabled: false }, undefined, 0)).toMatchObject({ text: "供应商已停用", tone: "dim" })
})

it("keeps the default mark in the detail seal but not in the list label", () => {
  const model = { ...base, enabled: true, verified: true, is_default: true }
  expect(catalogLine(model, undefined, 0).text).toBe("可选用 · 默认")
  expect(modelState(model, undefined, 0).text).toBe("可选用")
})

it("shows an open probe before the stored state", () => {
  const running = { id: "probe", status: "running", mode: "capabilities", submissions: 1, created_at: "2026-10-21T07:00:00Z" }
  expect(modelState({ ...base, enabled: true, verified: true }, running, Date.parse("2026-10-21T07:00:12Z"))).toMatchObject({
    text: "正在检查 · 12 秒",
    tone: "live",
  })
})

it("groups models into enabled, pending-check and off buckets", () => {
  const ready = { ...base, enabled: true, verified: true }
  const pending = { ...base, id: "pending" }
  const checkedOff = { ...base, id: "checked", verified: true }
  const providerOff = { ...base, id: "off", enabled: true, verified: true, provider_enabled: false }
  expect(modelStateGroup(ready)).toBe("enabled")
  expect(modelStateGroup({ ...base, enabled: true, is_default: true })).toBe("enabled")
  expect(modelStateGroup(pending)).toBe("pending")
  expect(modelStateGroup(checkedOff)).toBe("off")
  expect(modelStateGroup(providerOff)).toBe("off")
  expect(modelStateCounts([ready, pending, checkedOff, providerOff])).toEqual({ "": 4, enabled: 1, pending: 1, off: 2 })
})
