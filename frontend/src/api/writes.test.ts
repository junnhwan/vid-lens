import { describe, expect, it } from "vitest"
import { canReplay, forgetWrite, memoryStore, pendingWrite, rememberWrite, restoreGenerationInput } from "./writes"

describe("idempotent writes", () => {
  it("restores the accepted detail and model when the form changes before retry", () => {
    const store = memoryStore()
    const frozen = { video_url: "https://youtube.com/watch?v=abcdefghijk", detail_level: "detailed" as const, purpose: "保留条件", model_id: "original", model_revision_id: "r1" }
    rememberWrite(store, { slot: "request:create", key: "original", path: "/api/v1/requests", body: JSON.stringify(frozen), expiresAt: 200, startedAt: 1 })
    const saved = JSON.parse(pendingWrite(store, "request:create")!.body)
    expect(restoreGenerationInput(saved)).toEqual({ detail_level: "detailed", purpose: "保留条件", model_id: "original", model_revision_id: "r1" })
  })

  it("preserves omission on historical pending writes", () => {
    expect(restoreGenerationInput({})).toEqual({})
    expect(restoreGenerationInput({ detail_level: "standard" })).toEqual({ detail_level: "standard" })
  })
  it("keeps one pending body per action and forgets it after acceptance", () => {
    const store = memoryStore()
    rememberWrite(store, { slot: "request:create", key: "k1", path: "/api/v1/requests", body: "{\"video_url\":\"https://example\"}", expiresAt: 100, startedAt: 1 })
    rememberWrite(store, { slot: "request:create", key: "k2", path: "/api/v1/requests", body: "{\"video_url\":\"https://example/2\"}", expiresAt: 200, startedAt: 2 })
    expect(pendingWrite(store, "request:create")?.key).toBe("k2")
    expect(canReplay(pendingWrite(store, "request:create")!, 200)).toBe(true)
    expect(canReplay(pendingWrite(store, "request:create")!, 201)).toBe(false)
    forgetWrite(store, "request:create")
    expect(pendingWrite(store, "request:create")).toBeNull()
  })
})
