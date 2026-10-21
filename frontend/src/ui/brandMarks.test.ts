import { describe, expect, it } from "vitest"
import { resolveBrand } from "./brandMarks"

describe("resolveBrand", () => {
  it("keeps an explicit brand", () => {
    expect(resolveBrand("claude", "qwen3.6-plus")).toBe("claude")
    expect(resolveBrand("grok", "custom model")).toBe("grok")
  })

  it("reads the family from a generic display name", () => {
    expect(resolveBrand("generic", "qwen3.6-plus")).toBe("qwen")
    expect(resolveBrand("generic", "deepseek-v3")).toBe("deepseek")
    expect(resolveBrand("generic", "grok-4.7")).toBe("grok")
    expect(resolveBrand("generic", "xAI Grok 4")).toBe("grok")
    expect(resolveBrand("generic", "gpt-4o")).toBe("openai")
    expect(resolveBrand("generic", "kimi-k2")).toBe("kimi")
    expect(resolveBrand("generic", "glm-4.5")).toBe("glm")
    expect(resolveBrand("generic", "MiMo-7B")).toBe("mimo")
    expect(resolveBrand("generic", "豆包")).toBe("doubao")
    expect(resolveBrand("generic", "hunyuan-turbo")).toBe("hunyuan")
    expect(resolveBrand("", "hy3")).toBe("hunyuan")
  })

  it("recognises other common model families", () => {
    expect(resolveBrand("generic", "MiniMax-M3.1-Flash")).toBe("minimax")
    expect(resolveBrand("generic", "Llama 3.3 70B")).toBe("meta")
    expect(resolveBrand("generic", "mistral-large-latest")).toBe("mistral")
    expect(resolveBrand("generic", "gemma-3-27b")).toBe("gemma")
    expect(resolveBrand("generic", "ERNIE-4.5-turbo")).toBe("wenxin")
    expect(resolveBrand("generic", "讯飞星火 4.0")).toBe("spark")
    expect(resolveBrand("generic", "step-2-16k")).toBe("stepfun")
    expect(resolveBrand("generic", "yi-large")).toBe("yi")
    expect(resolveBrand("generic", "Baichuan4")).toBe("baichuan")
    expect(resolveBrand("generic", "command-r-plus")).toBe("cohere")
    expect(resolveBrand("minimax", "custom model")).toBe("minimax")
  })

  it("does not treat hy inside another word as 混元", () => {
    expect(resolveBrand("generic", "rhythm")).toBe("generic")
    expect(resolveBrand("generic", "yield")).toBe("generic")
    expect(resolveBrand("generic", "sparkling")).toBe("generic")
    expect(resolveBrand("generic", "command center")).toBe("generic")
    expect(resolveBrand("generic", "")).toBe("generic")
  })
})
