const manageSectionIDs = ["models", "providers", "diagnostics", "execution", "network", "videos", "examples", "announcement"] as const

export type ManageSection = (typeof manageSectionIDs)[number]

export interface ManageRoute {
  section: ManageSection
  offset: number
  query: string
}

function sectionID(value: string | undefined): ManageSection {
  return manageSectionIDs.find((id) => id === value) || "models"
}

export function pageLocation() {
  const loc = globalThis.location
  return { pathname: loc?.pathname || "/", search: loc?.search || "" }
}

export function readManagePath(path: string): ManageRoute {
  const query = path.indexOf("?")
  return readManageRoute(query < 0 ? path : path.slice(0, query), query < 0 ? "" : path.slice(query))
}

export function readManageRoute(pathname: string, search: string): ManageRoute {
  const parts = pathname.split("/").filter(Boolean)
  const section = parts[0] === "manage" ? sectionID(parts[1]) : "models"
  const params = new URLSearchParams(search.startsWith("?") ? search.slice(1) : search)
  const raw = Number(params.get("offset"))
  const offset = (section === "videos" || section === "diagnostics") && Number.isFinite(raw) && raw > 0 ? Math.floor(raw) : 0
  const query = section === "videos" ? (params.get("q") || "").trim().slice(0, 200) : ""
  return { section, offset, query }
}

export function managePath(section: string, extra?: { offset?: number; query?: string }) {
  const id = sectionID(section)
  const params = new URLSearchParams()
  if (id === "videos" && extra?.query) params.set("q", extra.query)
  if ((id === "videos" || id === "diagnostics") && extra?.offset && extra.offset > 0) params.set("offset", String(Math.floor(extra.offset)))
  const query = params.toString()
  const path = id === "models" ? "/manage" : `/manage/${id}`
  return query ? `${path}?${query}` : path
}
