import { createRoot } from "react-dom/client"
import { App } from "./app/App"
import "./styles/app.css"
import "./styles/product.css"

const root = document.getElementById("root")
if (root) createRoot(root).render(<App />)
