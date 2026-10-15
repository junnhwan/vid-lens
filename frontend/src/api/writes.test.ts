import { describe, expect, it } from "vitest"
import { canReplay, forgetWrite, memoryStore, pendingWrite, rememberWrite } from "./writes"

describe("idempotent writes", () => {
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
