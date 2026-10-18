import { App } from "../app/App"
import { Docs } from "./Docs"
import { Example } from "./Example"
import { Manage } from "./Manage"
import { SharedRecord } from "./SharedRecord"
export function SiteApp() {
  const path = location.pathname
  if (path === "/s" || path.startsWith("/s/")) return <SharedRecord />
  if (path === "/docs" || path.startsWith("/docs/")) return <Docs />
  if (path === "/example" || path === "/example/") return <Example />
  if (path === "/manage" || path.startsWith("/manage/")) return <Manage />
  return <App />
}
