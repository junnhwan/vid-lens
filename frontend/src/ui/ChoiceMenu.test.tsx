import { renderToStaticMarkup } from "react-dom/server"
import { describe, expect, it } from "vitest"
import { ChoiceMenu } from "./ChoiceMenu"

const options = [
  { id: "glm", label: "GLM 5.3 Flash", brand: "glm" },
  { id: "ds", label: "deepseek-v4.1-flash", brand: "generic", mark: { label: "推荐", note: "当前更稳定" } },
]

describe("choice menu recommendation", () => {
  it("shows the recommendation on the closed trigger", () => {
    const html = renderToStaticMarkup(
      <ChoiceMenu label="本次使用的模型" value="ds" placeholder="正在读取模型" options={options} onChange={() => {}} />,
    )
    expect(html).toContain("data-recommend=\"true\"")
    expect(html).toContain("推荐")
    expect(html).toContain("aria-label=\"本次使用的模型，deepseek-v4.1-flash，推荐，当前更稳定\"")
    expect(html).not.toContain("choice-menu")
  })

  it("keeps an ordinary model unmarked", () => {
    const html = renderToStaticMarkup(
      <ChoiceMenu label="本次使用的模型" value="glm" placeholder="正在读取模型" options={options} onChange={() => {}} />,
    )
    expect(html).not.toContain("data-recommend")
    expect(html).not.toContain("推荐")
    expect(html).toContain("aria-label=\"本次使用的模型，GLM 5.3 Flash\"")
  })
})
