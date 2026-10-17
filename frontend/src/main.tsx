import { createRoot } from "react-dom/client"
import { SiteApp } from "./site/SiteApp"
import "./styles/app.css"
import "./styles/product.css"
import "./styles/site.css"
import "./styles/reading.css"

const root = document.getElementById("root")
if (root) createRoot(root).render(<SiteApp />)
