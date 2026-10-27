import { expect, it } from "vitest"
import { HttpApi } from "./http"

it("sends detail in the same immutable request as model and purpose", async () => {
  let captured: RequestInit | undefined
  const api = new HttpApi({ fetchImpl: (async (_url, init) => {
    captured = init
    return new Response(JSON.stringify({ request: { id: "accepted" } }), { status: 201, headers: { "content-type": "application/json" } })
  }) as typeof fetch })
  await api.createRequest("https://youtube.com/watch?v=abcdefghijk", "fixed-key", { detail_level: "concise", model_id: "model", model_revision_id: "revision", purpose: "步骤", regenerate: true })
  expect(JSON.parse(String(captured?.body))).toEqual({ video_url: "https://youtube.com/watch?v=abcdefghijk", detail_level: "concise", model_id: "model", model_revision_id: "revision", purpose: "步骤", regenerate: true })
  expect(new Headers(captured?.headers).get("idempotency-key")).toBe("fixed-key")
})
