import { describe, expect, it } from "vitest"
import type { GenerationModel } from "../contract/types"
import { homeModelOption } from "./homeModel"

function model(partial: Pick<GenerationModel, "id" | "display_name" | "brand_id"> & { description?: string; recommended?: boolean }): GenerationModel {
  return { revision_id: "rev", ...partial }
}

describe("home model recommendation", () => {
  it("shows the mark only for a model the catalog recommends", () => {
    const option = homeModelOption(model({ id: "ds", display_name: "deepseek-v4.1-flash", brand_id: "generic", description: "图像与工具", recommended: true }))
    expect(option.mark).toEqual({ label: "推荐", note: "当前更稳定" })
    expect(option.hint).toBe("图像与工具")
    expect(homeModelOption(model({ id: "ds", display_name: "deepseek-v4.1-flash", brand_id: "deepseek" })).mark).toBeUndefined()
    expect(homeModelOption(model({ id: "glm", display_name: "GLM 5.3 Flash", brand_id: "glm", recommended: true })).mark).toEqual({ label: "推荐", note: "当前更稳定" })
  })
})
