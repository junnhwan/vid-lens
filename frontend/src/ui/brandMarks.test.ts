import { describe, expect, it } from "vitest"
import { resolveBrand } from "./brandMarks"

describe("resolveBrand", () => {
  it("keeps an explicit brand", () => {
    expect(resolveBrand("claude", "qwen3.6-plus")).toBe("claude")
  })

  it("reads the family from a generic display name", () => {
    expect(resolveBrand("generic", "qwen3.6-plus")).toBe("qwen")
    expect(resolveBrand("generic", "deepseek-v3")).toBe("deepseek")
    expect(resolveBrand("generic", "gpt-4o")).toBe("openai")
    expect(resolveBrand("generic", "kimi-k2")).toBe("kimi")
    expect(resolveBrand("generic", "glm-4.5")).toBe("glm")
    expect(resolveBrand("generic", "MiMo-7B")).toBe("mimo")
    expect(resolveBrand("generic", "豆包")).toBe("doubao")
    expect(resolveBrand("generic", "hunyuan-turbo")).toBe("hunyuan")
    expect(resolveBrand("", "hy3")).toBe("hunyuan")
  })

  it("does not treat hy inside another word as 混元", () => {
    expect(resolveBrand("generic", "rhythm")).toBe("generic")
    expect(resolveBrand("generic", "")).toBe("generic")
  })
})
