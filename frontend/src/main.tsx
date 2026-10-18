import { createRoot } from "react-dom/client"
import { SiteApp } from "./site/SiteApp"
import "./styles/app.css"
import "./styles/product.css"
import "./styles/site.css"
import "./styles/reading.css"
import "./styles/sharing.css"

// Apply this before React renders shared content or requests its images.
if (location.pathname.startsWith("/s/")) {
  for (const [name, content] of [["referrer", "no-referrer"], ["robots", "noindex, nofollow, noarchive"]]) {
    const meta = document.createElement("meta")
    meta.name = name
    meta.content = content
    document.head.append(meta)
  }
}

const root = document.getElementById("root")
if (root) createRoot(root).render(<SiteApp />)
