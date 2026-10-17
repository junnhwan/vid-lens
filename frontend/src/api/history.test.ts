import { describe, expect, it } from "vitest"
import { HttpApi } from "./http"

describe("request history API", () => {
  it("uses the current session cookie and requests a bounded page", async () => {
    let requested = ""
    let credentials = ""
    const api = new HttpApi({
      fetchImpl: async (input, init) => {
        requested = String(input)
        credentials = String(init?.credentials)
        return new Response(JSON.stringify({ schema_version: 1, items: [], limit: 20, offset: 20, has_more: false }), {
          status: 200,
          headers: { "content-type": "application/json" },
        })
      },
    })

    const page = await api.requestHistory(20)
    expect(requested).toBe("/api/v1/requests?offset=20&limit=20")
    expect(credentials).toBe("include")
    expect(page.offset).toBe(20)
  })
})
