import { expect, it } from "vitest"
import { managePath, readManageRoute } from "./manageRoute"

it("keeps the admin section, video search and usage page in the address", () => {
  expect(readManageRoute("/manage", "")).toEqual({ section: "models", offset: 0, query: "", cleanup: "" })
  expect(readManageRoute("/manage/videos", "?offset=40&q=solid")).toEqual({ section: "videos", offset: 40, query: "solid", cleanup: "" })
  expect(readManageRoute("/manage/videos", "?cleanup=ready")).toEqual({ section: "videos", offset: 0, query: "", cleanup: "ready" })
  expect(readManageRoute("/manage/videos", "?cleanup=later")).toEqual({ section: "videos", offset: 0, query: "", cleanup: "" })
  expect(readManageRoute("/manage/diagnostics/", "?offset=20&cleanup=ready")).toEqual({ section: "diagnostics", offset: 20, query: "", cleanup: "" })
  expect(readManageRoute("/manage/unknown", "?offset=20&q=solid")).toEqual({ section: "models", offset: 0, query: "", cleanup: "" })
  expect(managePath("models")).toBe("/manage")
  expect(managePath("videos", { offset: 40, query: "solid" })).toBe("/manage/videos?q=solid&offset=40")
  expect(managePath("videos", { query: "solid", cleanup: "held", offset: 20 })).toBe("/manage/videos?q=solid&cleanup=held&offset=20")
  expect(managePath("diagnostics", { offset: 0 })).toBe("/manage/diagnostics")
  expect(managePath("announcement")).toBe("/manage/announcement")
})
