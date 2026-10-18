import { describe, expect, it } from "vitest"
import { HttpApi } from "./http"
import { loadSharedRecord } from "./shares"

describe("single-record sharing", () => {
  it("keeps share changes scoped to the owner cookie and CSRF token", async () => {
    const calls: { path: string; init?: RequestInit }[] = []
    const api = new HttpApi({ fetchImpl: async (input, init) => {
      calls.push({ path: String(input), init })
      return new Response(JSON.stringify({ schema_version: 1, enabled: init?.method === "PUT", url: "/s/token", created_at: null }))
    } })
    api.csrfToken = "owner-csrf"
    await api.requestShare("one/record")
    await api.requestShare("one/record", true)
    await api.requestShare("one/record", false)
    expect(calls.map((call) => call.path)).toEqual(Array(3).fill("/api/v1/requests/one%2Frecord/share"))
    expect(calls.map((call) => call.init?.method)).toEqual(["GET", "PUT", "DELETE"])
    for (const call of calls.slice(1)) {
      expect(call.init?.credentials).toBe("include")
      expect(new Headers(call.init?.headers).get("x-csrf-token")).toBe("owner-csrf")
    }
  })

  it("opens a shared record without sending any browser's private session", async () => {
    await loadSharedRecord("public/token", undefined, async (input, init) => {
      expect(input).toBe("/api/v1/shares/public%2Ftoken")
      expect(init?.credentials).toBe("omit")
      expect(init?.cache).toBe("no-store")
      expect(init?.referrerPolicy).toBe("no-referrer")
      expect(new Headers(init?.headers).get("x-csrf-token")).toBeNull()
      return new Response(JSON.stringify({ schema_version: 1, view: {}, article: null, steps: [] }))
    })
  })

  it("distinguishes a revoked link from a temporary server outage", async () => {
    await expect(loadSharedRecord("revoked", undefined, async () => new Response("", { status: 404 }))).rejects.toMatchObject({ status: 404, code: "RESOURCE_NOT_FOUND" })
    await expect(loadSharedRecord("offline", undefined, async () => new Response("", { status: 503 }))).rejects.toMatchObject({ status: 503, code: "SERVICE_UNAVAILABLE" })
  })
})
